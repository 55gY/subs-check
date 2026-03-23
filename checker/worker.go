package checker

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/55gY/subs-check/config"
	proxyutils "github.com/55gY/subs-check/provider"
	"github.com/juju/ratelimit"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
)

// 全局测速统计（所有 speedWorker 共享）
var (
	speedStatsUnder10   atomic.Int32
	speedStatsUnder50   atomic.Int32
	speedStatsUnder100  atomic.Int32
	speedStatsUnder500  atomic.Int32
	speedStatsUnder1000 atomic.Int32
	speedStatsOverEnd   atomic.Int32
	receivedCounter     atomic.Int32
)

func hasMediaResult(result *Result) bool {
	if result == nil {
		return false
	}

	return result.Openai || result.OpenaiWeb || result.Youtube != "" || result.Netflix || result.Disney || result.Gemini || result.TikTok != "" || result.IPRisk != ""
}

// aliveWorkerCollect 存活检测worker（收集模式）
func aliveWorkerCollect(ctx context.Context, in <-chan PipelineItem, results *[]PipelineItem, mutex *sync.Mutex, tracker *ProgressTracker, speedON, mediaON bool) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-in:
			if !ok {
				return
			}
			if os.Getenv("SUB_CHECK_SKIP") != "" {
				tracker.CountAlive(false)
				continue
			}

			client := CreateClient(item.ProxyMap)
			if client == nil {
				slog.Debug("CreateClient失败", "proxy", item.ProxyMap["name"])
				tracker.CountAlive(false)
				continue
			}
			item.Client = client
			item.Result = &Result{Proxy: item.ProxyMap}

			google, latency, err := CheckAliveWithWarmup(ctx, client.Client)
			if err != nil || !google {
				slog.Debug("存活检测失败（收集模式）", "proxy", item.ProxyMap["name"], "latency", latency, "error", err)
				client.Close()
				tracker.CountAlive(false)
				continue
			}

			tracker.CountAlive(true)

			// 收集存活节点，根据配置决定是否需要后续检测
			if speedON || mediaON {
				mutex.Lock()
				*results = append(*results, item)
				mutex.Unlock()
			} else {
				// 不需要后续检测，直接输出结果
				// 这种情况不会走到这里，因为在主流程中已经判断
				client.Close()
			}
		}
	}
}

func aliveWorker(ctx context.Context, in <-chan PipelineItem, speedOut, mediaOut chan<- PipelineItem, resOut chan<- Result, tracker *ProgressTracker, speedON, mediaON bool) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-in:
			if !ok {
				return
			}
			if os.Getenv("SUB_CHECK_SKIP") != "" {
				tracker.CountAlive(false)
				continue
			}

			client := CreateClient(item.ProxyMap)
			if client == nil {
				slog.Debug("CreateClient失败", "proxy", item.ProxyMap["name"])
				tracker.CountAlive(false)
				continue
			}
			item.Client = client
			item.Result = &Result{Proxy: item.ProxyMap}

			google, latency, err := CheckAliveWithWarmup(ctx, client.Client)
			if err != nil || !google {
				slog.Debug("存活检测失败", "proxy", item.ProxyMap["name"], "latency", latency, "error", err)
				client.Close()
				tracker.CountAlive(false)
				continue
			}

			tracker.CountAlive(true)

			if speedON {
				select {
				case <-ctx.Done():
					client.Close()
					return
				case speedOut <- item:
				}
			} else if mediaON {
				select {
				case <-ctx.Done():
					client.Close()
					return
				case mediaOut <- item:
				}
			} else {
				if updateProxyName(ctx, item.Result, client, 0) {
					// 节点被过滤，关闭连接并跳过
					client.Close()
					continue
				}
				Available.Add(1)
				select {
				case <-ctx.Done():
					client.Close()
					return
				case resOut <- *item.Result:
				}
				client.Close()
			}
		}
	}
}

func speedWorker(ctx context.Context, in <-chan PipelineItem, mediaOut chan<- PipelineItem, resOut chan<- Result, tracker *ProgressTracker, mediaON bool, _ *atomic.Int32) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("speedWorker发生panic", "错误", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-in:
			if !ok {
				return
			}

			metrics, err := CheckSpeed(ctx, item.Client.Client, item.Client.BytesRead)
			if err != nil {
				item.Client.Close()
				tracker.CountSpeedWithDuration(false, metrics.TotalDuration, metrics.ContextCanceled)
				continue
			}
			speed := metrics.SpeedKBps
			// 统计互斥速度区间 - 使用全局 atomic 计数器
			switch {
			case speed < 10:
				speedStatsUnder10.Add(1)
			case speed < 50:
				speedStatsUnder50.Add(1)
			case speed < 100:
				speedStatsUnder100.Add(1)
			case speed < 500:
				speedStatsUnder500.Add(1)
			case speed < 1000:
				speedStatsUnder1000.Add(1)
			default:
				speedStatsOverEnd.Add(1)
			}

			item.Speed = speed
			tracker.CountSpeedWithDuration(true, metrics.TotalDuration, false)

			if mediaON {
				receivedCounter.Add(1)
				select {
				case <-ctx.Done():
					item.Client.Close()
					return
				case mediaOut <- item:
				}
			} else {
				if updateProxyName(ctx, item.Result, item.Client, speed) {
					// 节点被过滤，关闭连接并跳过
					item.Client.Close()
					continue
				}
				Available.Add(1)
				select {
				case <-ctx.Done():
					item.Client.Close()
					return
				case resOut <- *item.Result:
				}
				item.Client.Close()
			}
		}
	}
}

func mediaWorker(ctx context.Context, in <-chan PipelineItem, resOut chan<- Result, tracker *ProgressTracker, _ *atomic.Int32) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-in:
			if !ok {
				return
			}
			start := time.Now()
			// Media checks
			if config.GlobalConfig.MediaCheck {
				seenPlatforms := make(map[string]struct{}, len(config.GlobalConfig.Platforms))
				for _, plat := range config.GlobalConfig.Platforms {
					if _, exists := seenPlatforms[plat]; exists {
						continue
					}
					seenPlatforms[plat] = struct{}{}
					if ctx.Err() != nil {
						break
					}
					switch plat {
					case "openai":
						if item.Result.Openai || item.Result.OpenaiWeb {
							continue
						}
						cookiesOK, clientOK := CheckOpenAI(ctx, item.Client.Client)
						if clientOK && cookiesOK {
							item.Result.Openai = true
						} else if cookiesOK || clientOK {
							item.Result.OpenaiWeb = true
						}
					case "youtube":
						if item.Result.Youtube != "" {
							continue
						}
						if region, _ := CheckYoutube(ctx, item.Client.Client); region != "" {
							item.Result.Youtube = region
						}
					case "netflix":
						if item.Result.Netflix {
							continue
						}
						if ok, _ := CheckNetflix(ctx, item.Client.Client); ok {
							item.Result.Netflix = true
						}
					case "disney":
						if item.Result.Disney {
							continue
						}
						if ok, _ := CheckDisney(ctx, item.Client.Client); ok {
							item.Result.Disney = true
						}
					case "gemini":
						if item.Result.Gemini {
							continue
						}
						if ok, _ := CheckGemini(ctx, item.Client.Client); ok {
							item.Result.Gemini = true
						}
					case "iprisk":
						if item.Result.IP != "" && item.Result.IPRisk != "" {
							continue
						}
						country, ip, fraudScore := proxyutils.GetProxyCountry(ctx, item.Client.Client)
						if ip != "" {
							item.Result.IP = ip
							item.Result.Country = country
							item.Result.IPRisk = proxyutils.GetFraudScoreLabel(fraudScore)
						}
					case "tiktok":
						if item.Result.TikTok != "" {
							continue
						}
						if region, _ := CheckTikTok(ctx, item.Client.Client); region != "" {
							item.Result.TikTok = region
						}
					}
				}
			}

			tracker.CountMediaWithResult(hasMediaResult(item.Result), time.Since(start), false)
			if updateProxyName(ctx, item.Result, item.Client, item.Speed) {
				// 节点被过滤，关闭连接并跳过
				item.Client.Close()
				continue
			}

			Available.Add(1)
			select {
			case <-ctx.Done():
				item.Client.Close()
				return
			case resOut <- *item.Result:
			}
			item.Client.Close()
		}
	}
}

// statsConn wraps net.Conn to count bytes read and apply rate limiting
type statsConn struct {
	net.Conn
	bytesRead *uint64
	bucket    *ratelimit.Bucket
}

func (c *statsConn) Read(b []byte) (n int, err error) {
	// 速度限制（全局）
	if c.bucket != nil {
		c.bucket.Wait(int64(len(b)))
	}

	n, err = c.Conn.Read(b)
	atomic.AddUint64(c.bytesRead, uint64(n))

	return n, err
}

// CreateClient creates and returns an http.Client with a Close function
type ProxyClient struct {
	*http.Client
	proxy     constant.Proxy
	BytesRead *uint64
}

func CreateClient(mapping map[string]any) *ProxyClient {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug(fmt.Sprintf("CreateClient发生panic: %v, proxy: %v", r, mapping["name"]))
		}
	}()

	proxy, err := adapter.ParseProxy(mapping)
	if err != nil {
		slog.Debug(fmt.Sprintf("底层mihomo创建代理Client失败: %v", err))
		return nil
	}

	var bytesRead uint64
	baseTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			var u16Port uint16
			if port, err := strconv.ParseUint(port, 10, 16); err == nil {
				u16Port = uint16(port)
			}
			conn, err := proxy.DialContext(ctx, &constant.Metadata{
				Host:    host,
				DstPort: u16Port,
			})
			if err != nil {
				return nil, err
			}
			return &statsConn{
				Conn:      conn,
				bytesRead: &bytesRead,
				bucket:    Bucket,
			}, nil
		},
		DisableKeepAlives: true,
	}

	return &ProxyClient{
		Client: &http.Client{
			Timeout:   time.Duration(config.GlobalConfig.Timeout) * time.Millisecond,
			Transport: baseTransport,
		},
		proxy:     proxy,
		BytesRead: &bytesRead,
	}
}

// Close closes the proxy client and cleans up resources
// 防止底层库有一些泄露，所以这里手动关闭
func (pc *ProxyClient) Close() {
	defer func() {
		if r := recover(); r != nil {
			slog.Debug(fmt.Sprintf("Close发生panic: %v", r))
		}
	}()

	if pc.Client != nil {
		pc.Client.CloseIdleConnections()
	}

	// 即使这里不关闭，底层GC的时候也会自动关闭
	// 这里及时的关闭，方便内存回收
	if pc.proxy != nil {
		pc.proxy.Close()
	}
	pc.Client = nil

	if pc.BytesRead != nil {
		TotalBytes.Add(atomic.LoadUint64(pc.BytesRead))
	}
}

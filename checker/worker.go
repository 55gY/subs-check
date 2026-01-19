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

			google, err := CheckAlive(client.Client)
			if err != nil || !google {
				slog.Debug("存活检测失败", "proxy", item.ProxyMap["name"], "error", err)
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

			google, err := CheckAlive(client.Client)
			if err != nil || !google {
				slog.Debug("存活检测失败", "proxy", item.ProxyMap["name"], "error", err)
				client.Close()
				tracker.CountAlive(false)
				continue
			}

			tracker.CountAlive(true)

			if speedON {
				// 预先增加测速计数，避免节点在通道中时的计数盲区
				tracker.speedDone.Add(1)
				speedOut <- item
			} else if mediaON {
				// 预先增加媒体计数，避免节点在通道中时的计数盲区
				tracker.mediaDone.Add(1)
				mediaOut <- item
			} else {
				if updateProxyName(item.Result, client, 0) {
					// 节点被过滤，关闭连接并跳过
					client.Close()
					continue
				}
				Available.Add(1)
				resOut <- *item.Result
				client.Close()
			}
		}
	}
}

func speedWorker(ctx context.Context, in <-chan PipelineItem, mediaOut chan<- PipelineItem, resOut chan<- Result, tracker *ProgressTracker, mediaON bool) {
	processedCount := 0
	for {
		select {
		case <-ctx.Done():
			// slog.Info("speedWorker退出", "已处理", processedCount)
			return
		case item, ok := <-in:
			if !ok {
				// slog.Info("speedWorker接收到关闭信号", "已处理", processedCount)
				return
			}
			processedCount++

			speed, _, err := CheckSpeed(item.Client.Client, Bucket, item.Client.BytesRead)
			if err != nil {
				// slog.Info("测速失败", "proxy", item.ProxyMap["name"], "error", err)
				item.Client.Close()
				tracker.CountSpeed(false)
				continue
			}
			if speed < config.GlobalConfig.MinSpeed {
				// slog.Info("测速未达标", "proxy", item.ProxyMap["name"], "speed_kb", speed, "min_required", config.GlobalConfig.MinSpeed)
				item.Client.Close()
				tracker.CountSpeed(false)
				continue
			}

			item.Speed = speed
			tracker.CountSpeed(true)
			// slog.Info("测速通过", "proxy", item.ProxyMap["name"], "speed_kb", speed)

			if mediaON {
				// 预先增加媒体计数，避免节点在通道中时的计数盲区
				tracker.mediaDone.Add(1)
				mediaOut <- item
			} else {
				if updateProxyName(item.Result, item.Client, speed) {
					// 节点被过滤，关闭连接并跳过
					item.Client.Close()
					continue
				}
				Available.Add(1)
				resOut <- *item.Result
				item.Client.Close()
			}
		}
	}
}

func mediaWorker(ctx context.Context, in <-chan PipelineItem, resOut chan<- Result, tracker *ProgressTracker) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-in:
			if !ok {
				return
			}
			// Media checks
			if config.GlobalConfig.MediaCheck {
				for _, plat := range config.GlobalConfig.Platforms {
					switch plat {
					case "openai":
						cookiesOK, clientOK := CheckOpenAI(item.Client.Client)
						if clientOK && cookiesOK {
							item.Result.Openai = true
						} else if cookiesOK || clientOK {
							item.Result.OpenaiWeb = true
						}
					case "youtube":
						if region, _ := CheckYoutube(item.Client.Client); region != "" {
							item.Result.Youtube = region
						}
					case "netflix":
						if ok, _ := CheckNetflix(item.Client.Client); ok {
							item.Result.Netflix = true
						}
					case "disney":
						if ok, _ := CheckDisney(item.Client.Client); ok {
							item.Result.Disney = true
						}
					case "gemini":
						if ok, _ := CheckGemini(item.Client.Client); ok {
							item.Result.Gemini = true
						}
					case "iprisk":
						country, ip, fraudScore := proxyutils.GetProxyCountry(item.Client.Client)
						if ip != "" {
							item.Result.IP = ip
							item.Result.Country = country
							item.Result.IPRisk = proxyutils.GetFraudScoreLabel(fraudScore)
						}
					case "tiktok":
						if region, _ := CheckTikTok(item.Client.Client); region != "" {
							item.Result.TikTok = region
						}
					}
				}
			}

			tracker.CountMedia()
			if updateProxyName(item.Result, item.Client, item.Speed) {
				// 节点被过滤，关闭连接并跳过
				item.Client.Close()
				continue
			}

			Available.Add(1)
			resOut <- *item.Result
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

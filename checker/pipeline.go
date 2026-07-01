package checker

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/55gY/subs-check/config"
	proxyutils "github.com/55gY/subs-check/provider"
	"github.com/juju/ratelimit"
)

type Result struct {
	RecordID  uint64
	TestStage int
	SpeedKBps int
	Batch     int
	Proxy     map[string]any
	Openai    bool
	OpenaiWeb bool
	Youtube   string
	Netflix   bool
	Google    bool
	Disney    bool
	Gemini    bool
	TikTok    string
	IP        string
	IPRisk    string
	Country   string
}

type PipelineItem struct {
	ProxyMap map[string]any
	Result   *Result
	RecordID uint64
	Speed    int
}

var Progress atomic.Uint32
var Available atomic.Uint32
var ProxyCount atomic.Uint32
var TotalBytes atomic.Uint64
var ForceClose atomic.Bool
var CurrentTracker atomic.Pointer[ProgressTracker]
var Bucket *ratelimit.Bucket
var progressWeight ProgressWeight

// aliveItem 存活检测阶段传递到下一阶段的数据
type aliveItem struct {
	proxy  map[string]any
	client *ProxyClient
}

// speedItem 测速阶段传递到下一阶段的数据
type speedItem struct {
	proxy    map[string]any
	client   *ProxyClient
	speedKbs int
}

// Check 执行三阶段流水线代理检测
// 阶段1: 存活检测 → 阶段2: 测速检测 → 阶段3: 媒体检测
// 流水线模式：存活成功一个后立即进入测速，测速成功后立即进入媒体检测
// 三个阶段通过 channel 连接，独立并发运行
func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()
	ForceClose.Store(false)
	CurrentTracker.Store(nil)

	ProxyCount.Store(0)
	Available.Store(0)
	Progress.Store(0)
	TotalBytes.Store(0)

	tmp, failedSubs, successSubs, localUrls, err := proxyutils.GetProxies()
	if err != nil {
		return nil, fmt.Errorf("获取节点失败: %w", err)
	}
	proxies := append([]map[string]any{}, tmp...)
	slog.Info(fmt.Sprintf("获取节点数量: %d", len(proxies)))

	for _, successUrl := range successSubs {
		if err := config.ResetFailureCount(successUrl); err != nil {
			slog.Error("重置失败计数失败", "error", err, "url", successUrl)
		}
	}
	for _, failedUrl := range failedSubs {
		if !localUrls[failedUrl] {
			continue
		}
		failureCount, countErr := config.IncrementFailureCount(failedUrl)
		if countErr != nil {
			slog.Error("记录失败次数失败", "error", countErr, "url", failedUrl)
			continue
		}
		if config.ShouldRemoveFailedSub(failureCount) {
			slog.Warn("已删除失败订阅", "url", failedUrl, "失败次数", failureCount)
			if err := config.RemoveSubUrlFromConfig(failedUrl); err != nil {
				slog.Error("删除订阅失败", "error", err)
			}
		}
	}

	proxies = proxyutils.DeduplicateProxies(proxies)
	proxyutils.SmartShuffleByServer(proxies, proxyutils.ShuffleConfig{})
	slog.Info(fmt.Sprintf("去重并乱序后节点数量: %d", len(proxies)))

	speedON := config.GlobalConfig.SpeedTestUrl != "" && strings.TrimSpace(config.GlobalConfig.SpeedTestUrl) != ""
	mediaON := config.GlobalConfig.MediaCheck
	progressWeight = getCheckWeight(speedON, mediaON)
	tracker := NewProgressTracker(len(proxies))
	CurrentTracker.Store(tracker)

	slog.Info("检测模式配置",
		"存活检测", true,
		"测速检测", speedON,
		"媒体检测", mediaON,
		"测速URL", config.GlobalConfig.SpeedTestUrl)

	if !speedON && !mediaON {
		slog.Info("⚡ 快速模式：仅进行存活检测（未启用测速和媒体检测）")
	}

	if config.GlobalConfig.TotalSpeedLimit != 0 {
		Bucket = ratelimit.NewBucketWithRate(float64(config.GlobalConfig.TotalSpeedLimit*1024*1024), int64(config.GlobalConfig.TotalSpeedLimit*1024*1024/10))
	} else {
		Bucket = ratelimit.NewBucketWithRate(float64(math.MaxInt64), int64(math.MaxInt64))
	}

	if config.GlobalConfig == nil {
		return nil, fmt.Errorf("配置未正确加载")
	}

	// 各阶段使用独立 context，互不影响
	aliveCtx, aliveCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer aliveCancel()

	// ============ 流水线 channel ============
	// 阶段1 → 阶段2：存活成功的节点立即流入测速
	aliveCh := make(chan aliveItem, len(proxies))
	// 阶段2 → 阶段3：测速完成的节点立即流入媒体检测
	speedCh := make(chan speedItem, len(proxies))
	// 最终结果
	resultCh := make(chan Result, len(proxies))

	// ============ 阶段3: 媒体检测消费者 (先启动，从 speedCh 读取) ============
	var mediaWg sync.WaitGroup
	if mediaON {
		slog.Info("阶段3: 媒体检测启动")
		tracker.SetStage(2, "媒体检测")

		mediaCtx, mediaCancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer mediaCancel()

		mediaConcurrent := config.GlobalConfig.GetMediaConcurrent()
		if mediaConcurrent <= 0 {
			mediaConcurrent = 5
		}
		mediaSem := make(chan struct{}, mediaConcurrent)

		// 先启动阶段3消费者 goroutine，从 speedCh 读取
		go func() {
			for si := range speedCh {
				select {
				case <-mediaCtx.Done():
					// context 超时，关闭剩余节点的 client
					si.client.Close()
					tracker.CountMediaWithResult(false, 0, false)
					continue
				default:
				}
				mediaWg.Add(1)
				mediaSem <- struct{}{}
				go func(si speedItem) {
					defer mediaWg.Done()
					defer func() { <-mediaSem }()

					item := Result{Proxy: si.proxy, SpeedKBps: si.speedKbs}

					if ForceClose.Load() {
						tracker.CountMediaWithResult(false, 0, false)
						resultCh <- item
						si.client.Close()
						return
					}

					// 媒体检测：检测所有启用的平台
					for _, plat := range config.GlobalConfig.Platforms {
						switch plat {
						case "openai":
							item.Openai, item.OpenaiWeb = CheckOpenAI(mediaCtx, si.client.Client)
						case "youtube":
							if yt, err := CheckYoutube(mediaCtx, si.client.Client); err == nil {
								item.Youtube = yt
							}
						case "netflix":
							if nf, err := CheckNetflix(mediaCtx, si.client.Client); err == nil {
								item.Netflix = nf
							}
						case "disney":
							if ds, err := CheckDisney(mediaCtx, si.client.Client); err == nil {
								item.Disney = ds
							}
						case "gemini":
							if gm, err := CheckGemini(mediaCtx, si.client.Client); err == nil {
								item.Gemini = gm
							}
						case "tiktok":
							if tk, err := CheckTikTok(mediaCtx, si.client.Client); err == nil {
								item.TikTok = tk
							}
						}
					}

					tracker.CountMediaWithResult(true, 0, false)
					resultCh <- item
					si.client.Close()
				}(si)
			}
		}()
	} else {
		// 未启用媒体检测，直接构建结果
		go func() {
			for si := range speedCh {
				mediaWg.Add(1)
				resultCh <- Result{Proxy: si.proxy, SpeedKBps: si.speedKbs}
				si.client.Close()
			}
		}()
	}

	// ============ 阶段2: 测速检测消费者 (先启动，从 aliveCh 读取) ============
	var speedWg sync.WaitGroup
	if speedON {
		slog.Info("阶段2: 测速检测启动")
		tracker.SetStage(1, "测速检测")

		speedCtx, speedCancel := context.WithTimeout(context.Background(), time.Duration(config.GlobalConfig.DownloadTimeout+30)*time.Second)
		defer speedCancel()

		speedConcurrent := config.GlobalConfig.GetSpeedConcurrent()
		if speedConcurrent <= 0 {
			speedConcurrent = 5
		}
		speedSem := make(chan struct{}, speedConcurrent)

		// 先启动阶段2消费者 goroutine，从 aliveCh 读取
		go func() {
			for ai := range aliveCh {
				select {
				case <-speedCtx.Done():
					// context 超时，关闭剩余存活节点的 client
					ai.client.Close()
					tracker.CountSpeed(false)
					continue
				default:
				}
				speedWg.Add(1)
				speedSem <- struct{}{}
				go func(ai aliveItem) {
					defer speedWg.Done()
					defer func() { <-speedSem }()

					if ForceClose.Load() {
						tracker.CountSpeed(false)
						ai.client.Close()
						return
					}

					metrics, err := CheckSpeed(speedCtx, ai.client.Client, ai.client.BytesRead)
					if err == nil {
						// 测速成功，立即发送到媒体检测阶段
						speedCh <- speedItem{proxy: ai.proxy, client: ai.client, speedKbs: metrics.SpeedKBps}
						tracker.CountSpeed(true)
					} else {
						tracker.CountSpeed(false)
						// 测速失败的节点也传递到下一阶段（如果启用媒体检测）
						if mediaON {
							speedCh <- speedItem{proxy: ai.proxy, client: ai.client, speedKbs: 0}
						} else {
							// 未启用媒体检测时，测速失败的节点client需要立即关闭，避免资源泄漏
							ai.client.Close()
						}
					}
				}(ai)
			}
		}()
	} else {
		// 未启用测速，存活结果直接传递到下一阶段
		go func() {
			for ai := range aliveCh {
				speedWg.Add(1)
				speedCh <- speedItem{proxy: ai.proxy, client: ai.client, speedKbs: 0}
			}
		}()
	}

	// ============ 阶段1: 存活检测 (生产者) ============
	slog.Info("======== 流水线启动 ========")
	slog.Info("阶段1: 存活检测开始")
	tracker.SetStage(0, "存活检测")
	tracker.SetTimeout(3 * time.Minute)

	aliveConcurrent := config.GlobalConfig.GetAliveConcurrent()
	if aliveConcurrent <= 0 {
		aliveConcurrent = 5
	}

	var aliveWg sync.WaitGroup
	aliveSem := make(chan struct{}, aliveConcurrent)

	for _, proxy := range proxies {
		select {
		case <-aliveCtx.Done():
			goto aliveSubmitDone
		default:
		}
		aliveWg.Add(1)
		aliveSem <- struct{}{}
		go func(proxyMap map[string]any) {
			defer aliveWg.Done()
			defer func() { <-aliveSem }()

			if ForceClose.Load() {
				tracker.CountAlive(false)
				return
			}

			client := CreateClient(proxyMap)
			if client == nil {
				tracker.CountAlive(false)
				return
			}

			alive, err := CheckAlive(aliveCtx, client.Client)
			if err != nil || !alive {
				tracker.CountAlive(false)
				client.Close()
				return
			}
			tracker.CountAlive(true)
			// 存活成功，立即发送到测速阶段
			aliveCh <- aliveItem{proxy: proxyMap, client: client}
		}(proxy)
	}

aliveSubmitDone:
	aliveWg.Wait()
	close(aliveCh)
	tracker.ClearTimeout()

	totalNodes, aliveSuccessTotal, aliveDoneTotal, _, _, _ := tracker.GetStats()
	slog.Info("阶段1完成", "阶段", "存活检测", "总数", totalNodes, "成功数", aliveSuccessTotal, "失败数", aliveDoneTotal-aliveSuccessTotal)

	// 等待阶段2完成
	speedWg.Wait()
	close(speedCh)

	_, _, _, speedSuccessTotal, speedDoneTotal, _ := tracker.GetStats()
	if speedON {
		slog.Info("阶段2完成", "阶段", "测速检测", "总数", aliveSuccessTotal, "成功数", speedSuccessTotal, "失败数", speedDoneTotal-speedSuccessTotal)
	}

	// 等待阶段3完成
	mediaWg.Wait()
	close(resultCh)

	// 收集最终结果
	var results []Result
	for item := range resultCh {
		results = append(results, item)
	}

	_, _, _, _, _, mediaDoneTotal := tracker.GetStats()
	if mediaON {
		slog.Info("阶段3完成", "阶段", "媒体检测", "总数", speedDoneTotal, "完成数", mediaDoneTotal)
	}

	runtime.GC()

	// ============ 统计输出 ============
	totalNodes, aliveSuccessTotal, aliveDoneTotal, speedSuccessTotal, speedDoneTotal, mediaDoneTotal = tracker.GetStats()
	slog.Info("流水线完成统计",
		"阶段1-存活", fmt.Sprintf("总数=%d 成功=%d 失败=%d", totalNodes, aliveSuccessTotal, aliveDoneTotal-aliveSuccessTotal),
		"阶段2-测速", fmt.Sprintf("总数=%d 成功=%d 失败=%d", aliveSuccessTotal, speedSuccessTotal, speedDoneTotal-speedSuccessTotal),
		"阶段3-媒体", fmt.Sprintf("总数=%d 完成=%d", speedDoneTotal, mediaDoneTotal))
	slog.Info("检测完成统计",
		"总节点数", totalNodes,
		"最终可用数", len(results),
		"存活完成", aliveDoneTotal,
		"存活成功", aliveSuccessTotal,
		"存活失败", aliveDoneTotal-aliveSuccessTotal,
		"测速完成", speedDoneTotal,
		"测速成功", speedSuccessTotal,
		"测速失败", speedDoneTotal-speedSuccessTotal,
		"媒体完成", mediaDoneTotal,
		"媒体失败", 0)
	slog.Info(fmt.Sprintf("可用节点数量: %d", len(results)))
	slog.Info(fmt.Sprintf("测试总消耗流量: %.3fGB", float64(TotalBytes.Load())/1024/1024/1024))

	return results, nil
}

func getConcurrency(total int, base int, ratio float64) int {
	target := float64(base) * ratio
	if target < 1 {
		target = 1
	}
	const maxConcurrency = 300
	if target > maxConcurrency {
		target = maxConcurrency
	}
	if total < 100 {
		scale := float64(total) / 100.0
		result := int(target * scale)
		if result < 1 {
			return 1
		}
		return result
	}
	return int(target)
}

func updateProxyName(ctx context.Context, res *Result, httpClient *http.Client, speed int) bool {
	if config.GlobalConfig.RenameNode {
		var fraudScore int
		var country string
		if res.Country != "" && res.IPRisk != "" {
			fraudScore = parseFraudScoreFromLabel(res.IPRisk)
			country = res.Country
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(country, fraudScore)
		} else {
			country, _, fs := proxyutils.GetProxyCountry(ctx, httpClient)
			fraudScore = fs
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(country, fraudScore)
		}
		if shouldSkipByCountryCode(country) {
			return true
		}
	}

	var name string
	switch v := res.Proxy["name"].(type) {
	case string:
		name = v
	default:
		name = fmt.Sprintf("%v", v)
	}
	name = strings.TrimSpace(name)

	var tags []string
	if config.GlobalConfig.SpeedTestUrl != "" {
		name = regexp.MustCompile(`\s*\|(?:\s*[\d.]+[KM]B/s)`).ReplaceAllString(name, "")
		if speed > 0 {
			if speed < 1024 {
				tags = append(tags, fmt.Sprintf("%dKB/s", speed))
			} else {
				tags = append(tags, fmt.Sprintf("%.1fMB/s", float64(speed)/1024))
			}
		}
	}
	if config.GlobalConfig.MediaCheck {
		name = regexp.MustCompile(`\s*\|(?:NF|D\+|GPT⁺|GPT|GM|YT-[^|]+|TK-[^|]+|\d+%)`).ReplaceAllString(name, "")
	}
	for _, plat := range config.GlobalConfig.Platforms {
		switch plat {
		case "openai":
			if res.Openai {
				tags = append(tags, "GPT⁺")
			} else if res.OpenaiWeb {
				tags = append(tags, "GPT")
			}
		case "netflix":
			if res.Netflix {
				tags = append(tags, "NF")
			}
		case "disney":
			if res.Disney {
				tags = append(tags, "D+")
			}
		case "gemini":
			if res.Gemini {
				tags = append(tags, "GM")
			}
		case "iprisk":
			if res.IPRisk != "" {
				tags = append(tags, res.IPRisk)
			}
		case "youtube":
			if res.Youtube != "" {
				tags = append(tags, fmt.Sprintf("YT-%s", res.Youtube))
			}
		case "tiktok":
			if res.TikTok != "" {
				tags = append(tags, fmt.Sprintf("TK-%s", res.TikTok))
			}
		}
	}
	if tag, ok := res.Proxy["sub_tag"].(string); ok && tag != "" {
		tags = append(tags, tag)
	}
	if len(tags) > 0 {
		name += "|" + strings.Join(tags, "|")
	}
	res.Proxy["name"] = name
	return false
}

func showProgress(done chan bool, total int) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			fmt.Println()
			return
		case <-ticker.C:
			p := Progress.Load()
			pct := float64(p) / 100
			if pct > 100 {
				pct = 100
			}
			available := Available.Load()
			virtualProcessed := int(float64(total) * pct / 100)
			fmt.Printf("\r进度: [%-45s] %.1f%% (%d/%d) 可用: %d",
				strings.Repeat("=", int(pct/2))+">",
				pct,
				virtualProcessed,
				total,
				available)
		}
	}
}

func parseFraudScoreFromLabel(label string) int {
	switch label {
	case "极佳":
		return 5
	case "优秀":
		return 20
	case "良好":
		return 40
	case "中等":
		return 60
	case "差":
		return 80
	case "极差":
		return 95
	default:
		return 0
	}
}

func shouldSkipByCountryCode(countryCode string) bool {
	if len(config.GlobalConfig.Filters) == 0 {
		return false
	}
	for _, filter := range config.GlobalConfig.Filters {
		if strings.HasPrefix(countryCode, filter) {
			slog.Debug("节点被过滤", "country", countryCode, "filter", filter)
			return true
		}
	}
	return false
}
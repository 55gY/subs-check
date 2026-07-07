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
var CurrentTracker *ProgressTracker
var Bucket *ratelimit.Bucket
var progressWeight ProgressWeight

// calcCheckTimeout 根据节点数量和并发数自动计算检测超时时间
// 公式: 批次数 × 每批平均耗时(2秒)，最小120秒，最大1800秒(30分钟)
func calcCheckTimeout(nodeCount, concurrent int) time.Duration {
	if concurrent <= 0 {
		concurrent = 100
	}
	if nodeCount <= 0 {
		return 120 * time.Second
	}
	batches := math.Ceil(float64(nodeCount) / float64(concurrent))
	timeout := time.Duration(batches) * 2 * time.Second
	if timeout < 120*time.Second {
		timeout = 120 * time.Second
	}
	if timeout > 1800*time.Second {
		timeout = 1800 * time.Second
	}
	return timeout
}

func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()
	ForceClose.Store(false)
	CurrentTracker = nil

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
		if config.ShouldRemoveFailedSub(failedUrl, failureCount) {
			slog.Warn("已删除失败订阅", "url", failedUrl, "失败次数", failureCount)
			if err := config.RemoveSubUrlFromConfig(failedUrl); err != nil {
				slog.Error("删除订阅失败", "error", err)
			}
		}
	}

	// 清洗节点中的无效字符（U+FFFD等）
	proxyutils.CleanProxies(proxies)

	proxies = proxyutils.DeduplicateProxies(proxies)
	proxyutils.SmartShuffleByServer(proxies, proxyutils.ShuffleConfig{})
	slog.Info(fmt.Sprintf("去重并乱序后节点数量: %d", len(proxies)))

	speedON := config.GlobalConfig.SpeedTestUrl != "" && strings.TrimSpace(config.GlobalConfig.SpeedTestUrl) != ""
	mediaON := config.GlobalConfig.MediaCheck
	progressWeight = getCheckWeight(speedON, mediaON)
	tracker := NewProgressTracker(len(proxies))
	CurrentTracker = tracker

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

	concurrent := config.GlobalConfig.GetAliveConcurrent()
	if concurrent <= 0 {
		concurrent = 5
	}

	checkTimeout := calcCheckTimeout(len(proxies), concurrent)
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	var results []Result
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrent)

	slog.Info("======== 阶段1: 存活检测 ========")
	tracker.SetStage(0, "存活检测")
	tracker.SetTimeout(checkTimeout)
	slog.Info("存活检测超时配置", "节点数", len(proxies), "并发数", concurrent, "超时(秒)", int(checkTimeout.Seconds()))

	for _, proxy := range proxies {
		select {
		case <-ctx.Done():
			wg.Wait()
			goto finished
		default:
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(proxyMap map[string]any) {
			defer wg.Done()
			defer func() { <-sem }()

			if ForceClose.Load() {
				tracker.CountAlive(false)
				return
			}

			client := CreateClient(proxyMap)
			if client == nil {
				tracker.CountAlive(false)
				return
			}
			defer client.Close()

			var alive bool
			if config.GlobalConfig.UnifiedDelay {
				alive, _, err = CheckAliveWithWarmup(ctx, client.Client)
			} else {
				alive, err = CheckAlive(ctx, client.Client)
			}
			if err != nil || !alive {
				tracker.CountAlive(false)
				return
			}
			tracker.CountAlive(true)

			item := Result{Proxy: proxyMap}
			if speedON {
				tracker.SetStage(1, "测速检测")
				metrics, err := CheckSpeed(ctx, client.Client, client.BytesRead)
				if err == nil {
					item.SpeedKBps = metrics.SpeedKBps
					tracker.CountSpeed(true)
				} else {
					tracker.CountSpeed(false)
				}
			}

			if mediaON {
			tracker.SetStage(2, "媒体检测")
			// 遍历 platforms 配置，调用对应的媒体检测函数
			for _, plat := range config.GlobalConfig.Platforms {
				switch plat {
				case "openai":
					item.Openai, item.OpenaiWeb = CheckOpenAI(ctx, client.Client)
				case "youtube":
					if yt, err := CheckYoutube(ctx, client.Client); err == nil {
						item.Youtube = yt
					}
				case "netflix":
					if nf, err := CheckNetflix(ctx, client.Client); err == nil {
						item.Netflix = nf
					}
				case "disney":
					if ds, err := CheckDisney(ctx, client.Client); err == nil {
						item.Disney = ds
					}
				case "gemini":
					if gm, err := CheckGemini(ctx, client.Client); err == nil {
						item.Gemini = gm
					}
				case "tiktok":
					if tk, err := CheckTikTok(ctx, client.Client); err == nil {
						item.TikTok = tk
					}
				}
			}
			tracker.CountMediaWithResult(true, 0, false)
		}
	
			// 所有检测完成后，更新节点名称（重命名+标签）
			skip := updateProxyName(ctx, &item, client.Client, item.SpeedKBps)
			if skip {
				return
			}

			mu.Lock()
			results = append(results, item)
			mu.Unlock()
			Available.Store(uint32(len(results)))
		}(proxy)
	}

	wg.Wait()

finished:
	tracker.ClearTimeout()
	runtime.GC()

	totalNodes, aliveSuccessTotal, aliveDoneTotal, speedSuccessTotal, speedDoneTotal, mediaDoneTotal := tracker.GetStats()
	aliveFailedTotal := aliveDoneTotal - aliveSuccessTotal
	untestedTotal := totalNodes - aliveDoneTotal
	slog.Info("阶段1完成", "阶段", "存活检测", "总数", totalNodes, "已测试", aliveDoneTotal, "成功", aliveSuccessTotal, "失败", aliveFailedTotal, "未测试", untestedTotal)
	if speedON {
		slog.Info("======== 阶段2: 测速检测 ========")
		speedFailedTotal := speedDoneTotal - speedSuccessTotal
		slog.Info("阶段2完成", "阶段", "测速检测", "总数", aliveSuccessTotal, "成功", speedSuccessTotal, "失败", speedFailedTotal)
	}
	if mediaON {
		slog.Info("======== 阶段3: 媒体检测 ========")
		slog.Info("阶段3完成", "阶段", "媒体检测", "总数", speedSuccessTotal, "成功", mediaDoneTotal, "失败", 0)
	}
	slog.Info("阶段完成统计",
		"阶段1-存活", fmt.Sprintf("总数=%d 已测试=%d 成功=%d 失败=%d 未测试=%d", totalNodes, aliveDoneTotal, aliveSuccessTotal, aliveFailedTotal, untestedTotal),
		"阶段2-测速", fmt.Sprintf("总数=%d 成功=%d 失败=%d", aliveSuccessTotal, speedSuccessTotal, speedDoneTotal-speedSuccessTotal),
		"阶段3-媒体", fmt.Sprintf("总数=%d 成功=%d 失败=%d", speedSuccessTotal, mediaDoneTotal, 0))
	slog.Info("检测完成统计",
		"总节点数", totalNodes,
		"已测试", aliveDoneTotal,
		"存活成功", aliveSuccessTotal,
		"存活失败", aliveFailedTotal,
		"未测试", untestedTotal,
		"测速成功", speedSuccessTotal,
		"测速失败", speedDoneTotal-speedSuccessTotal,
		"媒体完成", mediaDoneTotal,
		"最终可用数", len(results))
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

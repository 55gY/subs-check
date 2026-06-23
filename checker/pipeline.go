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

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	var results []Result
	var mu sync.Mutex
	var wg sync.WaitGroup
	concurrent := config.GlobalConfig.GetAliveConcurrent()
	if concurrent <= 0 {
		concurrent = 5
	}
	sem := make(chan struct{}, concurrent)

	slog.Info("======== 阶段1: 存活检测 ========")
	tracker.SetStage(0, "存活检测")
	tracker.SetTimeout(2 * time.Minute)

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

			alive, err := CheckAlive(ctx, client.Client)
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
				youtube, err := CheckYoutube(ctx, client.Client)
				if err == nil {
					item.Youtube = youtube
					tracker.CountMediaWithResult(true, 0, false)
				} else {
					tracker.CountMediaWithResult(false, 0, false)
				}
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
	slog.Info("阶段1完成", "阶段", "存活检测", "总数", totalNodes, "成功数", aliveSuccessTotal, "失败数", aliveDoneTotal-aliveSuccessTotal)
	if speedON {
		slog.Info("======== 阶段2: 测速检测 ========")
		slog.Info("阶段2开始", "阶段", "测速检测", "总数", aliveSuccessTotal)
		slog.Info("阶段2完成", "阶段", "测速检测", "总数", aliveSuccessTotal, "成功数", speedSuccessTotal, "失败数", speedDoneTotal-speedSuccessTotal)
	}
	if mediaON {
		slog.Info("======== 阶段3: 媒体检测 ========")
		slog.Info("阶段3开始", "阶段", "媒体检测", "总数", speedSuccessTotal)
		slog.Info("阶段3完成", "阶段", "媒体检测", "总数", speedSuccessTotal, "成功数", mediaDoneTotal, "失败数", 0)
	}
	slog.Info("阶段完成统计",
		"阶段1-存活", fmt.Sprintf("总数=%d 成功=%d 失败=%d", totalNodes, aliveSuccessTotal, aliveDoneTotal-aliveSuccessTotal),
		"阶段2-测速", fmt.Sprintf("总数=%d 成功=%d 失败=%d", aliveSuccessTotal, speedSuccessTotal, speedDoneTotal-speedSuccessTotal),
		"阶段3-媒体", fmt.Sprintf("总数=%d 成功=%d 失败=%d", speedSuccessTotal, mediaDoneTotal, 0))
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

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

type pipelineNode struct {
	Proxy  map[string]any
	Client *ProxyClient
	Result Result
}

var Progress atomic.Uint32
var Available atomic.Uint32
var ProxyCount atomic.Uint32
var TotalBytes atomic.Uint64
var ForceClose atomic.Bool

// currentTracker 保存当前检测流水线的进度追踪器，供 API 层并发读取。
// 使用 atomic.Pointer 避免检测 goroutine 写入与 /api/status 读取之间的数据竞争。
var currentTracker atomic.Pointer[ProgressTracker]
var Bucket *ratelimit.Bucket
var progressWeight ProgressWeight

var (
	speedTagRe = regexp.MustCompile(`\s*\|(?:\s*[\d.]+[KM]B/s)`)
	mediaTagRe = regexp.MustCompile(`\s*\|(?:NF|D\+|GPT⁺|GPT|GM|YT-[^|]+|TK-[^|]+|\d+%)`)
)

// GetCurrentTracker 原子读取当前进度追踪器；无检测进行时返回 nil。
// 供 API 层（/api/status）在检测 goroutine 并发写入时安全读取。
func GetCurrentTracker() *ProgressTracker {
	return currentTracker.Load()
}

func clampBatchDuration(d time.Duration) time.Duration {
	if d < 2*time.Second {
		return 2 * time.Second
	}
	if d > 60*time.Second {
		return 60 * time.Second
	}
	return d
}

func stageTimeout(nodeCount, concurrent int, perBatch time.Duration) time.Duration {
	if concurrent <= 0 {
		concurrent = 1
	}
	if nodeCount <= 0 {
		return 0
	}
	batches := math.Ceil(float64(nodeCount) / float64(concurrent))
	return time.Duration(int64(batches)) * clampBatchDuration(perBatch)
}

// calcCheckTimeout 按三阶段串行耗时估算总超时。
// 存活阶段按真实单节点超时估算；测速/媒体按对应超时叠加。
// 结果限制在 [120s, 3600s]。
func calcCheckTimeout(nodeCount, aliveConc, speedConc, mediaConc int, speedON, mediaON bool) time.Duration {
	if aliveConc <= 0 {
		aliveConc = 100
	}
	if nodeCount <= 0 {
		return 120 * time.Second
	}

	cfg := config.GlobalConfig
	var aliveBatch time.Duration
	if cfg.UnifiedDelay {
		w := time.Duration(cfg.WarmupTimeout) * time.Second
		if w <= 0 {
			w = 15 * time.Second
		}
		t := time.Duration(cfg.TestTimeout) * time.Second
		if t <= 0 {
			t = 10 * time.Second
		}
		aliveBatch = w + t
	} else if cfg.Timeout > 0 {
		aliveBatch = time.Duration(cfg.Timeout) * time.Millisecond
	}
	timeout := stageTimeout(nodeCount, aliveConc, aliveBatch)

	if speedON {
		speedBatch := time.Duration(cfg.DownloadTimeout) * time.Second
		if speedBatch <= 0 {
			speedBatch = 10 * time.Second
		}
		timeout += stageTimeout(nodeCount, speedConc, speedBatch)
	}
	if mediaON {
		platformN := len(cfg.Platforms)
		if platformN <= 0 {
			platformN = 1
		}
		mediaBatch := time.Duration(platformN*5) * time.Second
		timeout += stageTimeout(nodeCount, mediaConc, mediaBatch)
	}

	if timeout < 120*time.Second {
		timeout = 120 * time.Second
	}
	if timeout > 3600*time.Second {
		timeout = 3600 * time.Second
	}
	return timeout
}

func sendOrDrop[T any](ctx context.Context, ch chan<- T, v T) bool {
	if ForceClose.Load() {
		return false
	}
	select {
	case <-ctx.Done():
		return false
	default:
	}
	select {
	case <-ctx.Done():
		return false
	case ch <- v:
		return true
	}
}

func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()
	ForceClose.Store(false)
	currentTracker.Store(nil)

	ProxyCount.Store(0)
	Available.Store(0)
	Progress.Store(0)
	TotalBytes.Store(0)

	tmp, failedSubs, successSubs, localUrls, err := proxyutils.GetProxies()
	if err != nil {
		return nil, fmt.Errorf("获取节点失败: %w", err)
	}
	proxies := append([]map[string]any{}, tmp...)
	slog.Info("获取节点数量", "count", len(proxies))

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

	proxyutils.CleanProxies(proxies)

	proxies = proxyutils.DeduplicateProxies(proxies)
	proxyutils.SmartShuffleByServer(proxies, proxyutils.ShuffleConfig{})
	slog.Info("去重并乱序后节点数量", "count", len(proxies))

	speedON := strings.TrimSpace(config.GlobalConfig.SpeedTestUrl) != ""
	mediaON := config.GlobalConfig.MediaCheck
	progressWeight = getCheckWeight(speedON, mediaON)
	tracker := NewProgressTracker(len(proxies))
	currentTracker.Store(tracker)

	slog.Info("检测模式配置",
		"存活检测", true,
		"测速检测", speedON,
		"媒体检测", mediaON,
		"测速URL", config.GlobalConfig.SpeedTestUrl)

	if !speedON && !mediaON {
		slog.Info("快速模式：仅进行存活检测（未启用测速和媒体检测）")
	}

	if config.GlobalConfig.TotalSpeedLimit != 0 {
		Bucket = ratelimit.NewBucketWithRate(float64(config.GlobalConfig.TotalSpeedLimit*1024*1024), int64(config.GlobalConfig.TotalSpeedLimit*1024*1024/10))
	} else {
		Bucket = ratelimit.NewBucketWithRate(float64(math.MaxInt64), int64(math.MaxInt64))
	}

	aliveConc := config.GlobalConfig.GetAliveConcurrent()
	if aliveConc <= 0 {
		aliveConc = 5
	}
	speedConc := config.GlobalConfig.GetSpeedConcurrent()
	if speedConc <= 0 {
		speedConc = 1
	}
	mediaConc := config.GlobalConfig.GetMediaConcurrent()
	if mediaConc <= 0 {
		mediaConc = 1
	}
	if !speedON {
		speedConc = 0
	}
	if !mediaON {
		mediaConc = 0
	}

	checkTimeout := calcCheckTimeout(len(proxies), aliveConc, speedConc, mediaConc, speedON, mediaON)
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()

	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ForceClose.Load() {
					cancel()
					return
				}
			}
		}
	}()

	var results []Result
	var mu sync.Mutex
	appendResult := func(item Result) {
		mu.Lock()
		results = append(results, item)
		Available.Store(uint32(len(results)))
		mu.Unlock()
	}
	closeNode := func(node *pipelineNode) {
		if node != nil && node.Client != nil {
			node.Client.Close()
			node.Client = nil
		}
	}
	finalizeNode := func(node *pipelineNode) {
		if node == nil {
			return
		}
		skip := false
		if node.Client != nil && node.Client.Client != nil {
			skip = updateProxyName(ctx, &node.Result, node.Client.Client, node.Result.SpeedKBps)
		}
		closeNode(node)
		if skip {
			return
		}
		appendResult(node.Result)
	}

	var speedChan chan *pipelineNode
	var mediaChan chan *pipelineNode
	if speedON {
		speedChan = make(chan *pipelineNode, speedConc)
	}
	if mediaON {
		mediaChan = make(chan *pipelineNode, mediaConc)
	}

	var mediaWG sync.WaitGroup
	if mediaON {
		for i := 0; i < mediaConc; i++ {
			mediaWG.Add(1)
			go func() {
				defer mediaWG.Done()
				for node := range mediaChan {
					if ForceClose.Load() || ctx.Err() != nil {
						tracker.CountMediaWithResult(false, 0, ctx.Err() != nil)
						finalizeNode(node)
						continue
					}
					fillMedia(ctx, &node.Result, node.Client.Client)
					hit := node.Result.Openai || node.Result.OpenaiWeb || node.Result.Netflix || node.Result.Disney || node.Result.Gemini || node.Result.Youtube != "" || node.Result.TikTok != ""
					tracker.CountMediaWithResult(hit, 0, false)
					tracker.AddMediaResult(node.Result.Openai || node.Result.OpenaiWeb, node.Result.Netflix, node.Result.Disney, node.Result.Gemini, node.Result.Youtube, node.Result.TikTok)
					finalizeNode(node)
				}
			}()
		}
	}

	handoffMedia := func(node *pipelineNode) {
		if mediaON {
			if sendOrDrop(ctx, mediaChan, node) {
				return
			}
		}
		finalizeNode(node)
	}

	var speedWG sync.WaitGroup
	if speedON {
		for i := 0; i < speedConc; i++ {
			speedWG.Add(1)
			go func() {
				defer speedWG.Done()
				for node := range speedChan {
					if ForceClose.Load() || ctx.Err() != nil {
						tracker.CountSpeed(false)
						finalizeNode(node)
						continue
					}
					metrics, err := CheckSpeed(ctx, node.Client.Client, node.Client.BytesRead)
					if err == nil {
						node.Result.SpeedKBps = metrics.SpeedKBps
						tracker.CountSpeed(true)
						tracker.AddSpeedSample(metrics.SpeedKBps)
					} else {
						tracker.CountSpeed(false)
					}
					handoffMedia(node)
				}
			}()
		}
	}

	aliveChan := make(chan map[string]any, aliveConc)
	var aliveWG sync.WaitGroup
	for i := 0; i < aliveConc; i++ {
		aliveWG.Add(1)
		go func() {
			defer aliveWG.Done()
			for proxyMap := range aliveChan {
				if ForceClose.Load() || ctx.Err() != nil {
					tracker.CountAlive(false)
					continue
				}

				client := CreateClient(proxyMap)
				if client == nil {
					tracker.CountAlive(false)
					continue
				}

				var alive bool
				var aliveErr error
				if config.GlobalConfig.UnifiedDelay {
					alive, _, aliveErr = CheckAliveWithWarmup(ctx, client.Client)
				} else {
					alive, aliveErr = CheckAlive(ctx, client.Client)
				}
				if aliveErr != nil || !alive {
					tracker.CountAlive(false)
					client.Close()
					continue
				}
				tracker.CountAlive(true)

				node := &pipelineNode{
					Proxy:  proxyMap,
					Client: client,
					Result: Result{Proxy: proxyMap},
				}
				if speedON {
					if sendOrDrop(ctx, speedChan, node) {
						continue
					}
					finalizeNode(node)
					continue
				}
				handoffMedia(node)
			}
		}()
	}

	slog.Info("======== 阶段1: 存活检测 ========")
	tracker.SetStage(0, "存活检测")
	tracker.SetTimeout(checkTimeout)
	slog.Info("分阶段并发",
		"alive", aliveConc,
		"speed", speedConc,
		"media", mediaConc,
		"节点数", len(proxies),
		"超时(秒)", int(checkTimeout.Seconds()))

	for _, proxy := range proxies {
		if ForceClose.Load() || ctx.Err() != nil {
			break
		}
		if !sendOrDrop(ctx, aliveChan, proxy) {
			break
		}
	}
	close(aliveChan)
	aliveWG.Wait()

	if speedON {
		slog.Info("======== 阶段2: 测速检测 ========")
		tracker.SetStage(1, "测速检测")
		close(speedChan)
		speedWG.Wait()
	}
	if mediaON {
		slog.Info("======== 阶段3: 媒体检测 ========")
		tracker.SetStage(2, "媒体检测")
		close(mediaChan)
		mediaWG.Wait()
	}

	tracker.ClearTimeout()
	runtime.GC()

	totalNodes, aliveSuccessTotal, aliveDoneTotal, speedSuccessTotal, speedDoneTotal, mediaDoneTotal := tracker.GetStats()
	aliveFailedTotal := aliveDoneTotal - aliveSuccessTotal
	untestedTotal := totalNodes - aliveDoneTotal
	slog.Info("阶段1完成", "阶段", "存活检测", "总数", totalNodes, "已测试", aliveDoneTotal, "成功", aliveSuccessTotal, "失败", aliveFailedTotal, "未测试", untestedTotal)
	if speedON {
		speedFailedTotal := speedDoneTotal - speedSuccessTotal
		slog.Info("阶段2完成", "阶段", "测速检测", "总数", aliveSuccessTotal, "成功", speedSuccessTotal, "失败", speedFailedTotal)
	}
	if mediaON {
		slog.Info("阶段3完成", "阶段", "媒体检测", "总数", aliveSuccessTotal, "成功", mediaDoneTotal, "失败", 0)
	}
	slog.Info("阶段完成统计",
		"阶段1-存活", fmt.Sprintf("总数=%d 已测试=%d 成功=%d 失败=%d 未测试=%d", totalNodes, aliveDoneTotal, aliveSuccessTotal, aliveFailedTotal, untestedTotal),
		"阶段2-测速", fmt.Sprintf("总数=%d 成功=%d 失败=%d", aliveSuccessTotal, speedSuccessTotal, speedDoneTotal-speedSuccessTotal),
		"阶段3-媒体", fmt.Sprintf("总数=%d 成功=%d 失败=%d", aliveSuccessTotal, mediaDoneTotal, 0))
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
	slog.Info("可用节点数量", "count", len(results))
	slog.Info("测试总消耗流量", "GB", fmt.Sprintf("%.3f", float64(TotalBytes.Load())/1024/1024/1024))

	return results, nil
}

func fillMedia(ctx context.Context, item *Result, httpClient *http.Client) {
	if httpClient == nil {
		return
	}
	for _, plat := range config.GlobalConfig.Platforms {
		switch plat {
		case "openai":
			item.Openai, item.OpenaiWeb = CheckOpenAI(ctx, httpClient)
		case "youtube":
			if yt, err := CheckYoutube(ctx, httpClient); err == nil {
				item.Youtube = yt
			}
		case "netflix":
			if nf, err := CheckNetflix(ctx, httpClient); err == nil {
				item.Netflix = nf
			}
		case "disney":
			if ds, err := CheckDisney(ctx, httpClient); err == nil {
				item.Disney = ds
			}
		case "gemini":
			if gm, err := CheckGemini(ctx, httpClient); err == nil {
				item.Gemini = gm
			}
		case "tiktok":
			if tk, err := CheckTikTok(ctx, httpClient); err == nil {
				item.TikTok = tk
			}
		}
	}
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
		name = speedTagRe.ReplaceAllString(name, "")
		if speed > 0 {
			if speed < 1024 {
				tags = append(tags, fmt.Sprintf("%dKB/s", speed))
			} else {
				tags = append(tags, fmt.Sprintf("%.1fMB/s", float64(speed)/1024))
			}
		}
	}
	if config.GlobalConfig.MediaCheck {
		name = mediaTagRe.ReplaceAllString(name, "")
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

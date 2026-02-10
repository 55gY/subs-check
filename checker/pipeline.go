package checker

import (
	"context"
	"fmt"
	"log/slog"
	"math"
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

// Result 存储节点检测结果
type Result struct {
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
	Client   *ProxyClient
	Result   *Result
	Speed    int
}

var Progress atomic.Uint32
var Available atomic.Uint32
var ProxyCount atomic.Uint32
var TotalBytes atomic.Uint64

var ForceClose atomic.Bool

// CurrentTracker 当前的进度追踪器，用于Web API访问
var CurrentTracker *ProgressTracker

var Bucket *ratelimit.Bucket

var progressWeight ProgressWeight

// Check 执行代理检测的主函数 (Adaptive Pipeline)
func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()
	ForceClose.Store(false)

	ProxyCount.Store(0)
	Available.Store(0)
	Progress.Store(0)
	TotalBytes.Store(0)

	// 1. 获取节点
	var proxies []map[string]any

	// 启动订阅获取进度同步到全局Progress
	subsFetchDone := make(chan struct{})
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-subsFetchDone:
				return
			case <-ticker.C:
				total := proxyutils.SubsFetchTotal.Load()
				if total > 0 {
					progress := proxyutils.SubsFetchProgress.Load()
					// 将订阅获取进度映射到0-100的范围
					Progress.Store(uint32(progress))
					ProxyCount.Store(uint32(total))
				}
			}
		}
	}()

	tmp, failedSubs, successSubs, localUrls, err := proxyutils.GetProxies()
	close(subsFetchDone) // 停止进度同步

	// 订阅获取完成,重置订阅进度变量(标记订阅阶段结束)
	proxyutils.SubsFetchTotal.Store(0)

	if err != nil {
		return nil, fmt.Errorf("获取节点失败: %w", err)
	}
	proxies = append(proxies, tmp...)
	slog.Info(fmt.Sprintf("获取节点数量: %d", len(proxies)))

	// 2. 处理失败计数
	for _, successUrl := range successSubs {
		if err := config.ResetFailureCount(successUrl); err != nil {
			slog.Error("重置失败计数失败", "error", err, "url", successUrl)
		}
	}
	for _, failedUrl := range failedSubs {
		// 检查是否为本地订阅
		if !localUrls[failedUrl] {
			// 远程订阅失败时记录但不删除
			// 直接跳过判断,不输出日志
			// failureCount, countErr := config.IncrementFailureCount(failedUrl)
			// if countErr != nil {
			// 	slog.Error("记录失败次数失败", "error", countErr, "url", failedUrl)
			// } else if config.ShouldRemoveFailedSub(failedUrl, failureCount) {
			// 	slog.Warn("远程订阅失败次数已达阈值", "url", failedUrl, "失败次数", failureCount, "说明", "该订阅来自远程清单，不会从本地配置中删除，请检查远程清单质量")
			// }
			continue
		}

		// 只处理本地订阅的删除
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

	// 3. 智能乱序 (Smart Shuffle)
	proxyutils.SmartShuffleByServer(proxies, proxyutils.ShuffleConfig{})
	slog.Info(fmt.Sprintf("去重并乱序后节点数量: %d", len(proxies)))

	// 4. 初始化进度追踪
	speedON := config.GlobalConfig.SpeedTestUrl != "" && strings.TrimSpace(config.GlobalConfig.SpeedTestUrl) != ""
	mediaON := config.GlobalConfig.MediaCheck
	progressWeight = getCheckWeight(speedON, mediaON)
	tracker := NewProgressTracker(len(proxies))
	CurrentTracker = tracker // 保存到全局变量供Web API使用

	slog.Info("检测模式配置",
		"存活检测", true,
		"测速检测", speedON,
		"媒体检测", mediaON,
		"最低速度(KB/s)", config.GlobalConfig.MinSpeed,
		"测速URL", config.GlobalConfig.SpeedTestUrl)

	if !speedON && !mediaON {
		slog.Info("⚡ 快速模式：仅进行存活检测（未启用测速和媒体检测）")
	}

	// 5. 初始化限速桶
	if config.GlobalConfig.TotalSpeedLimit != 0 {
		Bucket = ratelimit.NewBucketWithRate(float64(config.GlobalConfig.TotalSpeedLimit*1024*1024), int64(config.GlobalConfig.TotalSpeedLimit*1024*1024/10))
	} else {
		Bucket = ratelimit.NewBucketWithRate(float64(math.MaxInt64), int64(math.MaxInt64))
	}

	// 6. 启动 Pipeline
	aliveChan := make(chan PipelineItem, config.GlobalConfig.Concurrent)
	speedChan := make(chan PipelineItem, config.GlobalConfig.Concurrent)
	mediaChan := make(chan PipelineItem, config.GlobalConfig.Concurrent)
	resultChan := make(chan Result, len(proxies))

	// 存活检测结果收集
	var aliveResults []PipelineItem
	var aliveResultsMutex sync.Mutex

	var aliveWG, speedWG, mediaWG sync.WaitGroup

	// 计算并发数 (Adaptive Concurrency)
	baseConcurrent := config.GlobalConfig.Concurrent
	aliveConc := getConcurrency(len(proxies), baseConcurrent, 0.6)
	speedConc := getConcurrency(len(proxies), baseConcurrent, 0.4)
	mediaConc := getConcurrency(len(proxies), baseConcurrent, 0.2)

	slog.Info("启动检测流水线", "alive_workers", aliveConc, "speed_workers", speedConc, "media_workers", mediaConc)
	slog.Info("检测参数", "speed_enabled", speedON, "media_enabled", mediaON, "min_speed_kb", config.GlobalConfig.MinSpeed)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 设置初始阶段
	tracker.SetStage(0, "存活检测")

	// ===== 第1阶段：批量存活检测 =====
	// 启动 Alive Workers (收集模式)
	for i := 0; i < aliveConc; i++ {
		aliveWG.Add(1)
		go func() {
			defer aliveWG.Done()
			aliveWorkerCollect(ctx, aliveChan, &aliveResults, &aliveResultsMutex, tracker, speedON, mediaON)
		}()
	}

	// 进度显示
	done := make(chan bool)
	if config.GlobalConfig.PrintProgress {
		go showProgress(done, len(proxies))
	}

	// 定期 GC
	gcDone := make(chan bool)
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-gcDone:
				return
			case <-ticker.C:
				runtime.GC()
			}
		}
	}()

	// 发送存活检测任务
	go func() {
		sentCount := 0
		for _, p := range proxies {
			aliveChan <- PipelineItem{ProxyMap: p}
			sentCount++
		}
		close(aliveChan)
		slog.Info("所有存活检测任务已发送", "发送数量", sentCount)
	}()

	// 定期报告存活检测进度
	progressTicker := time.NewTicker(30 * time.Second)
	progressDone := make(chan bool)
	go func() {
		defer progressTicker.Stop()
		for {
			select {
			case <-progressDone:
				return
			case <-progressTicker.C:
				processed := tracker.aliveDone.Load()
				success := tracker.aliveSuccess.Load()
				percent := float64(processed) / float64(len(proxies)) * 100
				remaining := tracker.GetTimeoutRemaining()
				if remaining > 0 {
					slog.Info("存活检测进度报告",
						"已处理", processed,
						"总数", len(proxies),
						"通过", success,
						"进度", fmt.Sprintf("%.1f%%", percent),
						"超时倒计时", fmt.Sprintf("%ds", remaining))
				} else {
					slog.Info("存活检测进度报告",
						"已处理", processed,
						"总数", len(proxies),
						"通过", success,
						"进度", fmt.Sprintf("%.1f%%", percent))
				}
			}
		}
	}()

	// 等待存活检测完成
	slog.Info("等待存活检测完成...", "启动的workers", aliveConc)

	// 计算合理的超时时间并设置到tracker
	// 基础时间 = (节点数 / 并发数 * 单节点超时) + 缓冲时间
	baseTime := time.Duration(len(proxies)/aliveConc*config.GlobalConfig.Timeout)*time.Millisecond + 60*time.Second
	// 乘以2倍安全系数，考虑网络波动和worker调度延迟
	expectedTime := baseTime * 2
	if expectedTime < 2*time.Minute {
		expectedTime = 2 * time.Minute // 最少2分钟
	}
	if expectedTime > 10*time.Minute {
		expectedTime = 10 * time.Minute // 最多10分钟
	}
	tracker.SetTimeout(expectedTime)
	slog.Info("设置存活检测超时", "预计时间", expectedTime.String(), "基础时间", baseTime.String())

	// 添加超时保护，防止永久卡住
	aliveWaitDone := make(chan bool)
	go func() {
		aliveWG.Wait()
		aliveWaitDone <- true
	}()

	timeout := time.After(expectedTime)
	select {
	case <-aliveWaitDone:
		tracker.ClearTimeout() // 清除超时信息
		progressDone <- true   // 停止进度报告
		slog.Info("存活检测完成", "通过数量", tracker.aliveSuccess.Load(), "总节点数", len(proxies), "收集节点数", len(aliveResults))
	case <-timeout:
		tracker.ClearTimeout() // 清除超时信息
		progressDone <- true   // 停止进度报告
		未完成节点数 := len(proxies) - int(tracker.aliveDone.Load())
		slog.Warn("存活检测超时！强制继续",
			"已处理", tracker.aliveDone.Load(),
			"未完成", 未完成节点数,
			"总数", len(proxies),
			"通过数量", tracker.aliveSuccess.Load(),
			"收集节点数", len(aliveResults),
			"说明", "未完成的节点可能正在处理中，将在后台继续运行直到超时")
		// 注意：卡住的worker会在各自的HTTP超时后自动退出
		// 不强制取消context，因为后续的测速和媒体检测还需要使用
	}

	// ===== 第2-3阶段：测速+媒体流水线 =====
	// 如果没有存活节点或者不需要测速和媒体，直接返回
	if len(aliveResults) == 0 || (!speedON && !mediaON) {
		close(resultChan)
		goto collectResults
	}

	// 启动 Speed Workers
	if speedON {
		slog.Info("切换到测速检测阶段")
		tracker.SetStage(1, "测速检测")
	}
	slog.Info("启动测速workers", "数量", speedConc)
	for i := 0; i < speedConc; i++ {
		speedWG.Add(1)
		go func() {
			defer speedWG.Done()
			speedWorker(ctx, speedChan, mediaChan, resultChan, tracker, mediaON)
		}()
	}

	// 启动 Media Workers
	if mediaON {
		slog.Info("切换到媒体检测阶段")
		tracker.SetStage(2, "媒体检测")
	}
	slog.Info("启动媒体workers", "数量", mediaConc)
	for i := 0; i < mediaConc; i++ {
		mediaWG.Add(1)
		go func() {
			defer mediaWG.Done()
			mediaWorker(ctx, mediaChan, resultChan, tracker)
		}()
	}

	// 将存活节点送入测速流水线
	go func() {
		slog.Info("开始批量发送存活节点到测速流水线", "节点数", len(aliveResults))
		for _, item := range aliveResults {
			speedChan <- item
		}
		close(speedChan)
	}()

	// 级联关闭通道
	go func() {
		speedWG.Wait()
		slog.Info("测速检测完成")
		close(mediaChan)
	}()
	go func() {
		mediaWG.Wait()
		close(resultChan)
	}()

collectResults:

	// 收集结果
	var results []Result
	for res := range resultChan {
		results = append(results, res)
	}

	// 清理
	if config.GlobalConfig.PrintProgress {
		done <- true
	}
	gcDone <- true
	runtime.GC()

	slog.Info("检测完成统计",
		"总节点数", len(proxies),
		"存活节点数", tracker.aliveSuccess.Load(),
		"测速通过数", tracker.speedSuccess.Load(),
		"媒体检测数", tracker.mediaDone.Load(),
		"最终可用数", len(results))
	slog.Info(fmt.Sprintf("可用节点数量: %d", len(results)))
	slog.Info(fmt.Sprintf("测试总消耗流量: %.3fGB", float64(TotalBytes.Load())/1024/1024/1024))

	return results, nil
}

func getConcurrency(total int, base int, ratio float64) int {
	// 根据 ratio 调整基础并发数
	target := float64(base) * ratio
	if target < 1 {
		target = 1
	}

	// 添加硬性上限保护，避免过高并发导致资源耗尽
	const maxConcurrency = 300 // 最大并发数
	if target > maxConcurrency {
		target = maxConcurrency
	}

	// 根据节点总数自适应调整并发数
	// 节点少于100时使用较低并发避免资源浪费
	// 节点较多时使用完整的 target 值提高处理速度
	if total < 100 {
		// 节点较少时，并发数按节点数比例缩减
		scale := float64(total) / 100.0
		result := int(target * scale)
		if result < 1 {
			return 1
		}
		return result
	}

	// 节点数 ≥100 时，使用完整的 target 并发数
	return int(target)
}

// updateProxyName 更新代理名称
// 返回 true 表示节点应被过滤（跳过）
func updateProxyName(res *Result, httpClient *ProxyClient, speed int) bool {
	// 以节点IP查询位置重命名节点
	if config.GlobalConfig.RenameNode {
		var fraudScore int
		var country string
		if res.Country != "" && res.IPRisk != "" {
			// 如果已经有 Country 和 IPRisk，从 IPRisk 推导 fraudScore
			fraudScore = parseFraudScoreFromLabel(res.IPRisk)
			country = res.Country
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(country, fraudScore)
		} else {
			country, _, fs := proxyutils.GetProxyCountry(httpClient.Client)
			fraudScore = fs
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(country, fraudScore)
		}
		
		// 检查国家代码是否应被过滤
		if shouldSkipByCountryCode(country) {
			return true
		}
	}

	// 安全地获取 name 字段
	var name string
	switch v := res.Proxy["name"].(type) {
	case string:
		name = v
	default:
		name = fmt.Sprintf("%v", v)
	}
	name = strings.TrimSpace(name)

	var tags []string
	// 获取速度
	if config.GlobalConfig.SpeedTestUrl != "" {
		name = regexp.MustCompile(`\s*\|(?:\s*[\d.]+[KM]B/s)`).ReplaceAllString(name, "")
		var speedStr string
		if speed > 0 {
			if speed < 1024 {
				speedStr = fmt.Sprintf("%dKB/s", speed)
			} else {
				speedStr = fmt.Sprintf("%.1fMB/s", float64(speed)/1024)
			}
			tags = append(tags, speedStr)
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
			pct := float64(p) / float64(total) * 100
			if pct > 100 {
				pct = 100
			}
			available := Available.Load()

			fmt.Printf("\r进度: [%-45s] %.1f%% (%d/%d) 可用: %d",
				strings.Repeat("=", int(pct/2))+">",
				pct,
				p,
				total,
				available)
		}
	}
}

// parseFraudScoreFromLabel 从纯净度标签反推 fraudScore 数值
// 用于当已经有 IPRisk 标签但需要 fraudScore 数值的情况
func parseFraudScoreFromLabel(label string) int {
	switch label {
	case "极佳":
		return 5 // 0-10 范围，取中间值
	case "优秀":
		return 20 // 11-30 范围，取中间值
	case "良好":
		return 40 // 31-50 范围，取中间值
	case "中等":
		return 60 // 51-70 范围，取中间值
	case "差":
		return 80 // 71-90 范围，取中间值
	case "极差":
		return 95 // >90，取一个较高值
	default:
		return 0
	}
}
// shouldSkipByCountryCode 根据国家代码判断是否应该跳过节点
// 如果 Filters 为空，不过滤任何节点
// 使用前缀匹配方式检查国家代码
func shouldSkipByCountryCode(countryCode string) bool {
	// 如果 Filters 为空，不过滤任何节点
	if len(config.GlobalConfig.Filters) == 0 {
		return false
	}
	
	// 检查国家代码是否匹配任何过滤规则（前缀匹配）
	for _, filter := range config.GlobalConfig.Filters {
		if strings.HasPrefix(countryCode, filter) {
			slog.Debug("节点被过滤", "country", countryCode, "filter", filter)
			return true
		}
	}
	return false
}
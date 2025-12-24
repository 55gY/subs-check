package check

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/55gY/subs-check/check/platform"
	"github.com/55gY/subs-check/config"
	proxyutils "github.com/55gY/subs-check/proxy"
	"github.com/juju/ratelimit"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
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
	if config.GlobalConfig.KeepSuccessProxies {
		slog.Info(fmt.Sprintf("添加之前测试成功的节点，数量: %d", len(config.GlobalProxies)))
		proxies = append(proxies, config.GlobalProxies...)
	}

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

	tmp, failedSubs, successSubs, err := proxyutils.GetProxies()
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

	config.GlobalProxies = make([]map[string]any, 0)
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
		for _, p := range proxies {
			aliveChan <- PipelineItem{ProxyMap: p}
		}
		close(aliveChan)
	}()

	// 等待存活检测完成
	aliveWG.Wait()
	slog.Info("存活检测完成", "通过数量", tracker.aliveSuccess.Load(), "总节点数", len(proxies))

	// ===== 第2-3阶段：测速+媒体流水线 =====
	// 如果没有存活节点或者不需要测速和媒体，直接返回
	if len(aliveResults) == 0 || (!speedON && !mediaON) {
		close(resultChan)
		goto collectResults
	}

	// 启动 Speed Workers
	if speedON {
		tracker.SetStage(1, "测速检测")
	}
	for i := 0; i < speedConc; i++ {
		speedWG.Add(1)
		go func() {
			defer speedWG.Done()
			speedWorker(ctx, speedChan, mediaChan, resultChan, tracker, mediaON)
		}()
	}

	// 启动 Media Workers
	if mediaON {
		tracker.SetStage(2, "媒体检测")
	}
	for i := 0; i < mediaConc; i++ {
		mediaWG.Add(1)
		go func() {
			defer mediaWG.Done()
			mediaWorker(ctx, mediaChan, resultChan, tracker)
		}()
	}

	// 将存活节点送入测速流水线
	go func() {
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

	checkSubscriptionSuccessRate(proxies, results)

	return results, nil
}

func getConcurrency(total int, base int, ratio float64) int {
	target := float64(base) * ratio
	if target < 1 {
		target = 1
	}
	// 简化为线性计算：根据总数调整并发数
	if total < 100 {
		return int(target)
	}
	return int(target * ratio)
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

			google, err := platform.CheckAlive(client.Client)
			if err != nil || !google {
				slog.Debug("存活检测失败", "proxy", item.ProxyMap["name"], "error", err)
				client.Close()
				tracker.CountAlive(false)
				continue
			}

			tracker.CountAlive(true)

			// 收集存活节点，根据配置决定是否需要后续检测
			if speedON || mediaON {
				// 预先增加后续阶段计数，避免节点在内存列表中等待时的计数盲区
				// 这样在批量发送到下一阶段前，进度条就能显示这些节点已经在处理流程中
				if speedON {
					tracker.speedDone.Add(1)
				} else if mediaON {
					tracker.mediaDone.Add(1)
				}
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

			google, err := platform.CheckAlive(client.Client)
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
				updateProxyName(item.Result, client, 0)
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

			speed, _, err := platform.CheckSpeed(item.Client.Client, Bucket, item.Client.BytesRead)
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
				updateProxyName(item.Result, item.Client, speed)
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
						cookiesOK, clientOK := platform.CheckOpenAI(item.Client.Client)
						if clientOK && cookiesOK {
							item.Result.Openai = true
						} else if cookiesOK || clientOK {
							item.Result.OpenaiWeb = true
						}
					case "youtube":
						if region, _ := platform.CheckYoutube(item.Client.Client); region != "" {
							item.Result.Youtube = region
						}
					case "netflix":
						if ok, _ := platform.CheckNetflix(item.Client.Client); ok {
							item.Result.Netflix = true
						}
					case "disney":
						if ok, _ := platform.CheckDisney(item.Client.Client); ok {
							item.Result.Disney = true
						}
					case "gemini":
						if ok, _ := platform.CheckGemini(item.Client.Client); ok {
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
						if region, _ := platform.CheckTikTok(item.Client.Client); region != "" {
							item.Result.TikTok = region
						}
					}
				}
			}

			tracker.CountMedia()
			updateProxyName(item.Result, item.Client, item.Speed)

			Available.Add(1)
			resOut <- *item.Result
			item.Client.Close()
		}
	}
}

// updateProxyName 更新代理名称
func updateProxyName(res *Result, httpClient *ProxyClient, speed int) {
	// 以节点IP查询位置重命名节点
	if config.GlobalConfig.RenameNode {
		var fraudScore int
		if res.Country != "" && res.IPRisk != "" {
			// 如果已经有 Country 和 IPRisk，从 IPRisk 推导 fraudScore
			fraudScore = parseFraudScoreFromLabel(res.IPRisk)
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(res.Country, fraudScore)
		} else {
			country, _, fs := proxyutils.GetProxyCountry(httpClient.Client)
			fraudScore = fs
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(country, fraudScore)
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

func checkSubscriptionSuccessRate(allProxies []map[string]any, results []Result) {
	subStats := make(map[string]struct {
		total   int
		success int
	})

	for _, proxy := range allProxies {
		if subUrl, ok := proxy["sub_url"].(string); ok {
			stats := subStats[subUrl]
			stats.total++
			subStats[subUrl] = stats
		}
	}

	for _, result := range results {
		if result.Proxy != nil {
			if subUrl, ok := result.Proxy["sub_url"].(string); ok {
				stats := subStats[subUrl]
				stats.success++
				subStats[subUrl] = stats
			}
			delete(result.Proxy, "sub_url")
		}
	}

	for subUrl, stats := range subStats {
		if stats.total > 0 {
			successRate := float32(stats.success) / float32(stats.total)
			if successRate < config.GlobalConfig.SuccessRate {
				slog.Warn(fmt.Sprintf("订阅成功率过低: %s", subUrl),
					"总节点数", stats.total,
					"成功节点数", stats.success,
					"成功占比", fmt.Sprintf("%.2f%%", successRate*100))
			} else {
				slog.Debug(fmt.Sprintf("订阅节点统计: %s", subUrl),
					"总节点数", stats.total,
					"成功节点数", stats.success,
					"成功占比", fmt.Sprintf("%.2f%%", successRate*100))
			}
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

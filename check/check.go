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
	Proxy      map[string]any
	Openai     bool
	OpenaiWeb  bool
	Youtube    string
	Netflix    bool
	Google     bool
	Cloudflare bool
	Disney     bool
	Gemini     bool
	TikTok     string
	IP         string
	IPRisk     string
	Country    string
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

var Bucket *ratelimit.Bucket

var progressWeight ProgressWeight
var globalTracker *ProgressTracker // 添加全局tracker变量

// Check 执行代理检测的主函数 (Adaptive Pipeline)
func Check(proxies []adapter.Proxy) {
	// 1. 初始化全局变量
	ProxyCount.Store(uint32(len(proxies)))
	Progress.Store(0)
	Available.Store(0)
	TotalBytes.Store(0)
	ForceClose.Store(false)

	// 2. 初始化速率限制器
	if config.GlobalConfig.RateLimit > 0 {
		Bucket = ratelimit.NewBucketWithRate(float64(config.GlobalConfig.RateLimit), int64(config.GlobalConfig.RateLimit))
	}

	// 3. 初始化管道
	pipeline := make(chan PipelineItem, config.GlobalConfig.MaxConcurrency)
	var wg sync.WaitGroup

	// 4. 初始化进度追踪
	speedON := config.GlobalConfig.SpeedTestUrl != "" && strings.TrimSpace(config.GlobalConfig.SpeedTestUrl) != ""
	mediaON := config.GlobalConfig.MediaCheck
	progressWeight = getCheckWeight(speedON, mediaON)
	tracker := NewProgressTracker(len(proxies))
	globalTracker = tracker // 保存tracker到全局变量
	
	slog.Info("检测模式配置", 
		"存活检测", true,
		"测速检测", speedON, 
		"媒体检测", mediaON,
		"最低速度(KB/s)", config.GlobalConfig.MinSpeed,
		"测速URL", config.GlobalConfig.SpeedTestUrl)
	
	if !speedON && !mediaON {
		slog.Info("⚡ 快速模式：仅进行存活检测（未启用测速和媒体检测）")
	}

	// 5. 启动工作线程
	for i := 0; i < config.GlobalConfig.MaxConcurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range pipeline {
				checkProxy(item)
			}
		}()
	}

	// 6. 启动进度显示
	go func() {
		displayProgress()
	}()

	// 7. 启动代理检测
	for _, proxy := range proxies {
		if ForceClose.Load() {
			break
		}
		pipeline <- PipelineItem{
			ProxyMap: proxy.Export(),
			Client:   NewProxyClient(proxy),
			Result:   &Result{Proxy: proxy.Export()},
		}
	}

	// 8. 关闭管道
	close(pipeline)

	// 9. 等待所有工作线程完成
	wg.Wait()

	// 10. 输出结果
	outputResults()
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
			
			// 如果有全局tracker，则显示详细阶段信息
			if globalTracker != nil {
				stage, name, doneCount, successCount := globalTracker.GetStageInfo()
				if stage == 1 { // 测速阶段
					fmt.Printf("\r进度: [%-45s] %.1f%% (%d/%d) 可用: %d | 阶段: %s (%d/%d)",
						strings.Repeat("=", int(pct/2))+">",
						pct,
						p,
						total,
						available,
						name,
						successCount, // 测速达标数
						doneCount)    // 测速完成数
				} else {
					fmt.Printf("\r进度: [%-45s] %.1f%% (%d/%d) 可用: %d | 阶段: %s (%d/%d)",
						strings.Repeat("=", int(pct/2))+">",
						pct,
						p,
						total,
						available,
						name,
						successCount, // 对于其他阶段，这是成功数
						doneCount)    // 对于其他阶段，这是完成数
				}
			} else {
				// 显示基本进度信息
				fmt.Printf("\r进度: [%-45s] %.1f%% (%d/%d) 可用: %d",
					strings.Repeat("=", int(pct/2))+">",
					pct,
					p,
					total,
					available)
			}
		}
	}
}

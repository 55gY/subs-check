package checker

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/55gY/subs-check/config"
)

// CheckAlive 原有的单次存活检测
func CheckAlive(httpClient *http.Client) (bool, error) {
	resp, err := httpClient.Get(config.GlobalConfig.AliveTestUrl)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	// 2xx
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	return false, nil
}

// CheckAliveWithWarmup 带预热的存活检测（统一延迟模式）
// 通过两次测试消除握手时间差异，返回更准确的延迟测量
func CheckAliveWithWarmup(httpClient *http.Client) (bool, int, error) {
	cfg := config.GlobalConfig
	
	// 如果未启用统一延迟，使用原有逻辑
	if !cfg.UnifiedDelay {
		start := time.Now()
		alive, err := CheckAlive(httpClient)
		latency := int(time.Since(start).Milliseconds())
		return alive, latency, err
	}
	
	// 第一次测试：预热连接（完成握手）
	warmupTimeout := time.Duration(cfg.WarmupTimeout) * time.Second
	if warmupTimeout == 0 {
		warmupTimeout = 15 * time.Second // 默认15秒
	}
	
	ctx1, cancel1 := context.WithTimeout(context.Background(), warmupTimeout)
	defer cancel1()
	
	req1, err := http.NewRequestWithContext(ctx1, "GET", cfg.AliveTestUrl, nil)
	if err != nil {
		return false, 0, err
	}
	
	resp1, err := httpClient.Do(req1)
	if err != nil {
		slog.Debug("预热连接失败", "error", err)
		return false, 0, err
	}
	resp1.Body.Close()
	
	// 检查预热请求状态
	if resp1.StatusCode < 200 || resp1.StatusCode >= 300 {
		return false, 0, nil
	}
	
	// 短暂间隔，让连接稳定
	time.Sleep(50 * time.Millisecond)
	
	// 第二次测试：实际延迟测量
	testTimeout := time.Duration(cfg.TestTimeout) * time.Second
	if testTimeout == 0 {
		testTimeout = 10 * time.Second // 默认10秒
	}
	
	ctx2, cancel2 := context.WithTimeout(context.Background(), testTimeout)
	defer cancel2()
	
	req2, err := http.NewRequestWithContext(ctx2, "GET", cfg.AliveTestUrl, nil)
	if err != nil {
		return false, 0, err
	}
	
	start := time.Now()
	resp2, err := httpClient.Do(req2)
	latency := int(time.Since(start).Milliseconds())
	
	if err != nil {
		return false, latency, err
	}
	defer resp2.Body.Close()
	
	// 检查状态码
	if resp2.StatusCode >= 200 && resp2.StatusCode < 300 {
		return true, latency, nil
	}
	
	return false, latency, nil
}

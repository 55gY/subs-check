package checker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/55gY/subs-check/config"
)

// DelayTestResult 延迟测试结果
type DelayTestResult struct {
	RTT     int    // RTT 时间（毫秒）
	Success bool   // 是否成功
	Err     string // 错误信息
}

// DelayTest 执行延迟测试（支持 HTTP/HTTPS，可选代理）
func DelayTest(ctx context.Context, targetURL string, httpClient *http.Client, useProxy bool) DelayTestResult {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 0}
	}
	start := time.Now()

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, "HEAD", targetURL, nil)
	if err != nil {
		return DelayTestResult{RTT: 10000, Success: false, Err: "创建请求失败: " + err.Error()}
	}

	// 如果使用代理，设置代理
	if useProxy {
		// 创建一个新的 http.Client，使用代理
		proxyURL := &url.URL{
			Scheme: "http",
			Host:   "127.0.0.1:8199",
		}
		httpClient = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyURL(proxyURL),
			},
		}
	}

	// 发送 HEAD 请求
	resp, err := httpClient.Do(req)
	if err != nil {
		return DelayTestResult{RTT: int(time.Since(start).Milliseconds()), Success: false, Err: "请求失败: " + err.Error()}
	}
	defer resp.Body.Close()

	// 读取响应体（可选，用于确保连接正常）
	_, err = io.ReadAll(resp.Body)
	if err != nil {
		// 读取失败不影响延迟测试结果
	}

	return DelayTestResult{RTT: int(time.Since(start).Milliseconds()), Success: true, Err: ""}
}

// DelayTestWithWarmup 带预热的延迟测试（统一延迟模式）
// 返回：是否成功，RTT（毫秒），错误
func DelayTestWithWarmup(ctx context.Context, targetURL string, httpClient *http.Client, useProxy bool) (bool, int, error) {
	cfg := config.GlobalConfig

	if !cfg.UnifiedDelay {
		// 不使用统一延迟模式，直接单次测试
		result := DelayTest(ctx, cfg.AliveTestUrl, httpClient, false)
		return result.Success, result.RTT, nil
	}

	// 使用统一延迟模式：先预热，再测试
	warmupTimeout := time.Duration(cfg.WarmupTimeout) * time.Second
	if warmupTimeout == 0 {
		warmupTimeout = 15 * time.Second
	}

	ctx1, cancel1 := context.WithTimeout(ctx, warmupTimeout)
	defer cancel1()

	// 预热
	_ = DelayTest(ctx1, targetURL, httpClient, useProxy)

	// 等待 50ms
	select {
	case <-ctx.Done():
		return false, 0, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}

	// 实际测试
	testTimeout := time.Duration(cfg.TestTimeout) * time.Second
	if testTimeout == 0 {
		testTimeout = 10 * time.Second
	}

	ctx2, cancel2 := context.WithTimeout(ctx, testTimeout)
	defer cancel2()

	result := DelayTest(ctx2, targetURL, httpClient, useProxy)
	if !result.Success {
		return false, result.RTT, errors.New(result.Err)
	}

	return true, result.RTT, nil
}

package checker

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/55gY/subs-check/config"
)

const maxDelayBodySize = 64 * 1024

// DelayTestResult 延迟测试结果
type DelayTestResult struct {
	RTT     int    // RTT 时间（毫秒）
	Success bool   // 是否成功
	Err     string // 错误信息
}

// DelayTest 执行延迟测试（支持 HTTP/HTTPS）
func DelayTest(ctx context.Context, targetURL string, httpClient *http.Client) DelayTestResult {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 0}
	}
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, "HEAD", targetURL, nil)
	if err != nil {
		return DelayTestResult{RTT: 10000, Success: false, Err: "创建请求失败: " + err.Error()}
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return DelayTestResult{RTT: int(time.Since(start).Milliseconds()), Success: false, Err: "请求失败: " + err.Error()}
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDelayBodySize))

	return DelayTestResult{RTT: int(time.Since(start).Milliseconds()), Success: true, Err: ""}
}

// DelayTestWithWarmup 带预热的延迟测试（统一延迟模式）
// 返回：是否成功，RTT（毫秒），错误
func DelayTestWithWarmup(ctx context.Context, targetURL string, httpClient *http.Client) (bool, int, error) {
	cfg := config.GlobalConfig

	if !cfg.UnifiedDelay {
		result := DelayTest(ctx, cfg.AliveTestUrl, httpClient)
		return result.Success, result.RTT, nil
	}

	warmupTimeout := time.Duration(cfg.WarmupTimeout) * time.Second
	if warmupTimeout == 0 {
		warmupTimeout = 15 * time.Second
	}

	ctx1, cancel1 := context.WithTimeout(ctx, warmupTimeout)
	defer cancel1()

	_ = DelayTest(ctx1, targetURL, httpClient)

	select {
	case <-ctx.Done():
		return false, 0, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}

	testTimeout := time.Duration(cfg.TestTimeout) * time.Second
	if testTimeout == 0 {
		testTimeout = 10 * time.Second
	}

	ctx2, cancel2 := context.WithTimeout(ctx, testTimeout)
	defer cancel2()

	result := DelayTest(ctx2, targetURL, httpClient)
	if !result.Success {
		return false, result.RTT, errors.New(result.Err)
	}

	return true, result.RTT, nil
}

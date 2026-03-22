package checker

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/55gY/subs-check/config"
)

func doAliveRequest(ctx context.Context, httpClient *http.Client, url string) (int, int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, 0, err
	}

	start := time.Now()
	resp, err := httpClient.Do(req)
	latency := int(time.Since(start).Milliseconds())
	if err != nil {
		return 0, latency, err
	}
	defer resp.Body.Close()

	return resp.StatusCode, latency, nil
}

func isSuccessfulStatusCode(statusCode int) bool {
	return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
}

// CheckAlive 原有的单次存活检测
func CheckAlive(ctx context.Context, httpClient *http.Client) (bool, error) {
	statusCode, _, err := doAliveRequest(ctx, httpClient, config.GlobalConfig.AliveTestUrl)
	if err != nil {
		return false, err
	}
	if isSuccessfulStatusCode(statusCode) {
		return true, nil
	}
	return false, nil
}

// CheckAliveWithWarmup 带预热的存活检测（统一延迟模式）
// 通过两次测试消除握手时间差异，返回更准确的延迟测量
func CheckAliveWithWarmup(ctx context.Context, httpClient *http.Client) (bool, int, error) {
	cfg := config.GlobalConfig

	// 如果未启用统一延迟，使用原有逻辑
	if !cfg.UnifiedDelay {
		statusCode, latency, err := doAliveRequest(ctx, httpClient, cfg.AliveTestUrl)
		if err != nil {
			return false, latency, err
		}
		return isSuccessfulStatusCode(statusCode), latency, nil
	}

	// 第一次测试：预热连接（完成握手）
	warmupTimeout := time.Duration(cfg.WarmupTimeout) * time.Second
	if warmupTimeout == 0 {
		warmupTimeout = 15 * time.Second // 默认15秒
	}

	ctx1, cancel1 := context.WithTimeout(ctx, warmupTimeout)
	defer cancel1()

	statusCode, _, err := doAliveRequest(ctx1, httpClient, cfg.AliveTestUrl)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return false, 0, err
		}
		slog.Debug("预热连接失败", "error", err)
		return false, 0, err
	}

	if !isSuccessfulStatusCode(statusCode) {
		return false, 0, nil
	}

	// 短暂间隔，让连接稳定
	select {
	case <-ctx.Done():
		return false, 0, ctx.Err()
	case <-time.After(50 * time.Millisecond):
	}

	// 第二次测试：实际延迟测量
	testTimeout := time.Duration(cfg.TestTimeout) * time.Second
	if testTimeout == 0 {
		testTimeout = 10 * time.Second // 默认10秒
	}

	ctx2, cancel2 := context.WithTimeout(ctx, testTimeout)
	defer cancel2()

	statusCode, latency, err := doAliveRequest(ctx2, httpClient, cfg.AliveTestUrl)
	if err != nil {
		return false, latency, err
	}

	if isSuccessfulStatusCode(statusCode) {
		return true, latency, nil
	}

	return false, latency, nil
}

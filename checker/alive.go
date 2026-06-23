package checker

import (
	"context"
	"net/http"

	"github.com/55gY/subs-check/config"
)

// CheckAlive 基于 DelayTest 的存活检测（clash-verge-rev 风格）
func CheckAlive(ctx context.Context, httpClient *http.Client) (bool, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	result := DelayTest(ctx, config.GlobalConfig.AliveTestUrl, httpClient, false)
	return result.Success, nil
}

// CheckAliveWithWarmup 带预热的存活检测（统一延迟模式）
func CheckAliveWithWarmup(ctx context.Context, httpClient *http.Client) (bool, int, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	return DelayTestWithWarmup(ctx, config.GlobalConfig.AliveTestUrl, httpClient, false)
}

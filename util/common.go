package util

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
)

func GetExecutablePath() string {
	ex, err := os.Executable()
	if err != nil {
		slog.Error(fmt.Sprintf("获取程序路径失败: %v", err))
		return "."
	}
	return filepath.Dir(ex)
}

// SetupSignalHandler 设置信号处理
// 同时支持两种信号处理模式：
// - HUB 信号(SIGHUP): 只设置 check.ForceClose 为 true，不退出程序
// - Ctrl+C 信号(SIGINT/SIGTERM): 第一次设置 ForceClose，第二次退出程序
func SetupSignalHandler(forceClose *atomic.Bool) {
	slog.Debug("设置信号处理器")

	// 监听 SIGHUP (类似 Nginx 的 HUB 信号)
	hubSigChan := make(chan os.Signal, 1)
	signal.Notify(hubSigChan, syscall.SIGHUP)

	// 处理 HUB 信号
	go func() {
		for sig := range hubSigChan {
			slog.Debug(fmt.Sprintf("收到 HUB 信号: %s", sig))

			// HUB 信号只设置 ForceClose，不退出程序
			forceClose.Store(true)
			slog.Debug("HUB 模式: 已设置强制关闭标志，任务将自动结束，程序继续运行")
		}
	}()
}

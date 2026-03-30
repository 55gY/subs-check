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

	// 监听 SIGHUP / SIGINT / SIGTERM
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM)

	var exitRequested atomic.Bool

	go func() {
		for sig := range sigChan {
			switch sig {
			case syscall.SIGHUP:
				slog.Debug(fmt.Sprintf("收到 HUB 信号: %s", sig))
				forceClose.Store(true)
				slog.Debug("HUB 模式: 已设置强制关闭标志，任务将自动结束，程序继续运行")
			case syscall.SIGINT, syscall.SIGTERM:
				if exitRequested.CompareAndSwap(false, true) {
					slog.Warn("收到退出信号，已设置强制关闭标志；再次发送将立即退出")
					forceClose.Store(true)
					continue
				}
				slog.Error("再次收到退出信号，程序立即退出")
				os.Exit(1)
			default:
				slog.Warn(fmt.Sprintf("收到未处理信号: %s", sig))
			}
		}
	}()
}

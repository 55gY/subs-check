package monitor

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	human "github.com/docker/go-units"
)

// StartMemoryMonitor 启动内存监控
func StartMemoryMonitor() {
	// 设置Go运行时软内存限制（Go 1.19+）
	// 这会让GC更积极地回收内存，而不是杀死进程
	if softLimit := os.Getenv("SUB_CHECK_MEM_SOFT_LIMIT"); softLimit != "" {
		memoryLimit, err := human.FromHumanSize(softLimit)
		if err != nil {
			slog.Error("软内存限制参数错误", "error", err)
		} else if memoryLimit > 0 {
			// 设置Go运行时内存限制
			debug.SetMemoryLimit(memoryLimit)
			slog.Info("已设置Go运行时软内存限制", "limit", softLimit)
			
			// 启动监控，当接近限制时主动触发GC
			go func() {
				ticker := time.NewTicker(10 * time.Second)
				defer ticker.Stop()
				
				var m runtime.MemStats
				for range ticker.C {
					runtime.ReadMemStats(&m)
					currentUsage := m.Sys
					usagePercent := float64(currentUsage) / float64(memoryLimit) * 100
					
					// 当使用超过80%时，主动触发GC
					if usagePercent > 80 {
						slog.Debug("内存使用接近限制，触发GC", 
							"usage", formatBytes(currentUsage),
							"limit", softLimit,
							"percent", fmt.Sprintf("%.1f%%", usagePercent))
						runtime.GC()
					}
				}
			}()
		}
	}
	
	// mihomo的内存问题解决不了，所以加个内存限制自动重启
	// 解决了，暂时保留逻辑
	if limit := os.Getenv("SUB_CHECK_MEM_LIMIT"); limit != "" {
		memoryLimit, err := human.FromHumanSize(limit)
		if err != nil {
			slog.Error("硬内存限制参数错误", "error", err)
			return
		}

		if memoryLimit == 0 {
			return
		}
		
		slog.Info("已设置硬内存限制（超限将重启进程）", "limit", limit)

		go func() {
			for {
				time.Sleep(30 * time.Second)
				checkMemory(uint64(memoryLimit))
			}
		}()
	}

	// 添加内存使用情况监控
	if strings.ToLower(os.Getenv("SUB_CHECK_MEM_MONITOR")) != "" {
		go func() {
			var m runtime.MemStats
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				runtime.ReadMemStats(&m)
				slog.Info("内存使用情况",
					"Alloc", formatBytes(m.Alloc),
					"TotalAlloc", formatBytes(m.TotalAlloc),
					"Sys", formatBytes(m.Sys),
					"HeapAlloc", formatBytes(m.HeapAlloc),
					"HeapSys", formatBytes(m.HeapSys),
					"HeapInuse", formatBytes(m.HeapInuse),
					"HeapIdle", formatBytes(m.HeapIdle),
					"HeapReleased", formatBytes(m.HeapReleased),
					"HeapObjects", m.HeapObjects,
					"StackInuse", formatBytes(m.StackInuse),
					"StackSys", formatBytes(m.StackSys),
					"MSpanInuse", formatBytes(m.MSpanInuse),
					"MSpanSys", formatBytes(m.MSpanSys),
					"MCacheInuse", formatBytes(m.MCacheInuse),
					"MCacheSys", formatBytes(m.MCacheSys),
					"BuckHashSys", formatBytes(m.BuckHashSys),
					"GCSys", formatBytes(m.GCSys),
					"OtherSys", formatBytes(m.OtherSys),
					"NextGC", formatBytes(m.NextGC),
					"LastGC", time.Unix(0, int64(m.LastGC)).Format("15:04:05"),
					"PauseTotalNs", m.PauseTotalNs,
					"NumGC", m.NumGC,
					"NumForcedGC", m.NumForcedGC,
					"GCCPUFraction", m.GCCPUFraction,
				)
			}
		}()
	}
}

// checkMemory 检查内存使用情况
func checkMemory(memoryLimit uint64) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	currentUsage := m.HeapAlloc + m.StackInuse
	if currentUsage > memoryLimit {
		metadata := m.Sys - m.HeapSys - m.StackSys
		heapFrag := m.HeapInuse - m.HeapAlloc
		approxRSS := m.HeapAlloc + m.StackInuse + metadata + heapFrag
		slog.Warn("内存超过使用限制",
			"rss", human.HumanSize(float64(approxRSS)),
			"metadata", human.HumanSize(float64(metadata)),
			"heapFrag", human.HumanSize(float64(heapFrag)),
			"limit", human.HumanSize(float64(memoryLimit)))

		// 重新启动自己
		cmd := getSelfCommand()
		if cmd != nil {
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			cmd.Start() // 让新进程启动
			slog.Warn("因为内存问题启动了新进程，二进制用户如果需要关闭请关闭此窗口/终端")
		}

		// 退出当前进程
		os.Exit(1)
	}
}

// getSelfCommand 获取当前程序路径和参数
func getSelfCommand() *exec.Cmd {
	exePath, err := os.Executable()
	if err != nil {
		slog.Error("获取可执行文件路径失败:", "error", err)
		return nil
	}
	args := os.Args[1:] // 获取参数（不包括程序名）
	slog.Warn("🔄 进程即将重启...", "path", exePath, "args", args)
	return exec.Command(exePath, args...)
}

// formatBytes 将字节数格式化为人类可读的形式
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

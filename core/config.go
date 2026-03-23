package core

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/55gY/subs-check/config"
	"github.com/55gY/subs-check/util"
	"github.com/fsnotify/fsnotify"
	"gopkg.in/yaml.v3"
)

// initConfigPath 初始化配置文件路径
func (app *App) initConfigPath() error {
	if app.configPath == "" {
		execPath := util.GetExecutablePath()
		configDir := filepath.Join(execPath, "config")

		if err := os.MkdirAll(configDir, 0755); err != nil {
			return fmt.Errorf("创建配置目录失败: %w", err)
		}

		app.configPath = filepath.Join(configDir, "config.yaml")
	}
	// 设置全局配置文件路径，供其他包使用
	config.GlobalConfigPath = app.configPath
	return nil
}

// loadConfig 加载配置文件
func (app *App) loadConfig() error {
	yamlFile, err := os.ReadFile(app.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return app.createDefaultConfig()
		}
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := yaml.Unmarshal(yamlFile, config.GlobalConfig); err != nil {
		return fmt.Errorf("解析配置文件失败: %w", err)
	}

	// 加载配置后，去除SubUrls中的重复项
	if len(config.GlobalConfig.SubUrls) > 0 {
		seenUrls := make(map[string]bool)
		var uniqueUrls []string
		duplicateCount := 0

		for _, url := range config.GlobalConfig.SubUrls {
			if !seenUrls[url] {
				seenUrls[url] = true
				uniqueUrls = append(uniqueUrls, url)
			} else {
				duplicateCount++
			}
		}

		// 如果发现重复项，更新配置并保存
		if duplicateCount > 0 {
			slog.Warn(fmt.Sprintf("配置中发现 %d 个重复订阅链接，已自动去除", duplicateCount))
			config.GlobalConfig.SubUrls = uniqueUrls

			// 调用去重函数清理配置文件
			if err := config.DeduplicateSubUrlsFromConfig(); err != nil {
				slog.Warn(fmt.Sprintf("自动去重配置文件失败: %v", err))
			}
		}
	}

	// 如果配置了 filters，自动确保 iprisk 在 platforms 中
	ensureIpriskInPlatforms()

	slog.Info("配置文件读取成功",
		"speed-test-url", config.GlobalConfig.SpeedTestUrl,
		"alive-test-url", config.GlobalConfig.AliveTestUrl)

	// 输出并发配置加载信息
	if config.GlobalConfig.ConcurrentStage != nil {
		slog.Info("并发配置加载",
			"concurrent-stage.alive", config.GlobalConfig.ConcurrentStage.Alive,
			"concurrent-stage.speed", config.GlobalConfig.ConcurrentStage.Speed,
			"concurrent-stage.media", config.GlobalConfig.ConcurrentStage.Media,
			"生效-alive", config.GlobalConfig.GetAliveConcurrent(),
			"生效-speed", config.GlobalConfig.GetSpeedConcurrent(),
			"生效-media", config.GlobalConfig.GetMediaConcurrent())
	} else {
		slog.Info("并发配置加载",
			"concurrent-stage", "未配置(使用默认值)",
			"生效-alive", config.GlobalConfig.GetAliveConcurrent(),
			"生效-speed", config.GlobalConfig.GetSpeedConcurrent(),
			"生效-media", config.GlobalConfig.GetMediaConcurrent())
	}

	return nil
}

// ensureIpriskInPlatforms 确保当配置了 filters 时，iprisk 平台自动添加到 platforms 列表
// 这样可以确保 Country 字段被正确设置，过滤功能才能正常工作
func ensureIpriskInPlatforms() {
	// 检查是否配置了 filters
	if len(config.GlobalConfig.Filters) == 0 {
		return
	}

	// 检查 platforms 中是否已有 iprisk
	hasIprisk := false
	for _, plat := range config.GlobalConfig.Platforms {
		if plat == "iprisk" {
			hasIprisk = true
			break
		}
	}

	// 如果没有 iprisk，则自动添加
	if !hasIprisk {
		config.GlobalConfig.Platforms = append(config.GlobalConfig.Platforms, "iprisk")
		slog.Info("检测到 filters 配置，已自动启用 iprisk 平台以支持节点过滤功能")
	}
}

// createDefaultConfig 创建默认配置文件
func (app *App) createDefaultConfig() error {
	slog.Info("配置文件不存在，创建默认配置文件")

	if err := os.WriteFile(app.configPath, []byte(config.DefaultConfigTemplate), 0644); err != nil {
		return fmt.Errorf("写入默认配置文件失败: %w", err)
	}

	slog.Info("默认配置文件创建成功")
	slog.Info(fmt.Sprintf("请编辑配置文件: %s", app.configPath))
	os.Exit(0)
	return nil
}

// initConfigWatcher 初始化配置文件监听
func (app *App) initConfigWatcher() error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("创建文件监听器失败: %w", err)
	}

	app.watcher = watcher

	// 防抖定时器，防止vscode等软件先临时创建文件在覆盖，会产生两次write事件
	var debounceTimer *time.Timer
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if absPath, _ := filepath.Abs(app.configPath); event.Name != absPath {
					continue
				}
				// 兼容容器外修改
				if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
					// 如果定时器存在，重置它
					if debounceTimer != nil {
						debounceTimer.Stop()
					}

					// 创建新的定时器，延迟100ms执行
					debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
						slog.Info("配置文件发生变化，正在重新加载")
						oldCronExpr := config.GlobalConfig.CronExpression
						oldInterval := app.interval

						if err := app.loadConfig(); err != nil {
							slog.Error(fmt.Sprintf("重新加载配置文件失败: %v", err))
							return
						}

						// 检查cron表达式或检测间隔是否变化
						if oldCronExpr != config.GlobalConfig.CronExpression ||
							oldInterval != config.GlobalConfig.CheckInterval {

							app.interval = func() int {
								if config.GlobalConfig.CheckInterval <= 0 {
									return 1
								}
								return config.GlobalConfig.CheckInterval
							}()
							slog.Warn("检测设置发生变化，重新配置定时器")

							// 使用setTimer方法重新设置定时器
							app.setTimer()
						}
					})
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				slog.Error(fmt.Sprintf("配置文件监听错误: %v", err))
			}
		}
	}()

	// 开始监听配置文件目录
	if err := watcher.Add(filepath.Dir(app.configPath)); err != nil {
		return fmt.Errorf("添加配置文件监听失败: %w", err)
	}

	slog.Info("配置文件监听已启动")
	return nil
}

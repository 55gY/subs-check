package core

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/55gY/subs-check/checker"
	"github.com/55gY/subs-check/config"
	"github.com/55gY/subs-check/output"
	proxyutils "github.com/55gY/subs-check/provider"
	"github.com/55gY/subs-check/util"
	"github.com/gin-gonic/gin"
	"github.com/juju/ratelimit"
	"github.com/metacubex/mihomo/common/convert"
	"gopkg.in/yaml.v3"
)

// getConfig 获取配置文件内容
func (app *App) getConfig(c *gin.Context) {
	configData, err := os.ReadFile(app.configPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取配置文件失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"content": string(configData),
	})
}

// updateConfig 更新配置文件内容
func (app *App) updateConfig(c *gin.Context) {
	var req struct {
		Content string `json:"content"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求格式"})
		return
	}
	// 验证YAML格式
	var yamlData map[string]any
	if err := yaml.Unmarshal([]byte(req.Content), &yamlData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("YAML格式错误: %v", err)})
		return
	}

	message := "配置已更新"
	if subToken, ok := yamlData["sub-token"].(string); !ok || strings.TrimSpace(subToken) == "" {
		// 提交数据中 sub-token 为空，始终生成新 token（不依赖内存中的旧值）
		token := GenerateSecureToken()
		config.GlobalConfig.SubToken = token
		req.Content = upsertTopLevelConfigValue(req.Content, "sub-token", token, "api-key")
		yamlData["sub-token"] = token
		message = "配置已更新，已自动生成订阅token"
	}

	// 去除 sub-urls 中的重复项（保留原有格式和注释）
	if existing, ok := yamlData["sub-urls"]; ok {
		if urls, ok := existing.([]any); ok {
			// 提取所有订阅URL
			seenUrls := make(map[string]bool)
			var uniqueUrlsInOrder []string
			for _, url := range urls {
				if strUrl, ok := url.(string); ok {
					strUrl = strings.TrimSpace(strUrl)
					if strUrl != "" && !seenUrls[strUrl] {
						seenUrls[strUrl] = true
						uniqueUrlsInOrder = append(uniqueUrlsInOrder, strUrl)
					}
				}
			}

			// 如果有重复，则逐行处理原始内容
			if len(uniqueUrlsInOrder) < len(urls) {
				lines := strings.Split(req.Content, "\n")
				var newLines []string
				inSubUrls := false
				processedUrls := make(map[string]bool)

				for _, line := range lines {
					trimmedLine := strings.TrimSpace(line)

					// 检测 sub-urls 部分
					if trimmedLine == "sub-urls:" || trimmedLine == "sub-urls: []" {
						inSubUrls = true
						newLines = append(newLines, line)
						continue
					}

					// 如果在 sub-urls 部分
					if inSubUrls {
						// 检测是否为 sub-urls 的项
						if strings.HasPrefix(trimmedLine, "- ") {
							// 提取URL
							url := strings.TrimSpace(strings.TrimPrefix(trimmedLine, "- "))
							if url != "" && !processedUrls[url] {
								processedUrls[url] = true
								newLines = append(newLines, line)
							}
							// 跳过重复的URL行
							continue
						} else if len(trimmedLine) > 0 && trimmedLine[0] != ' ' && trimmedLine[0] != '#' {
							// 遇到新的顶级配置项，sub-urls 部分结束
							inSubUrls = false
						}
					}

					// 保留其他所有行（包括注释）
					newLines = append(newLines, line)
				}

				req.Content = strings.Join(newLines, "\n")
			}
		}
	}

	// 写入新配置 (使用互斥锁保护)
	app.configMutex.Lock()
	defer app.configMutex.Unlock()

	if err := os.WriteFile(app.configPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存配置文件失败: %v", err)})
		return
	}

	// 配置文件监听器会自动重新加载配置
	c.JSON(http.StatusOK, gin.H{"message": message})
}

func upsertTopLevelConfigValue(content, key, value, insertAfter string) string {
	lines := strings.Split(content, "\n")
	newLine := fmt.Sprintf("%s: %q", key, value)
	insertAt := -1

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+":") {
			lines[i] = newLine
			return strings.Join(lines, "\n")
		}
		if strings.HasPrefix(trimmed, insertAfter+":") {
			insertAt = i + 1
		}
	}

	if insertAt >= 0 {
		lines = append(lines[:insertAt], append([]string{newLine}, lines[insertAt:]...)...)
		return strings.Join(lines, "\n")
	}
	if strings.HasSuffix(content, "\n") {
		return content + newLine + "\n"
	}
	return content + "\n" + newLine
}

// addConfig 添加单个或多个节点到数据库，或添加订阅链接到配置
func (app *App) addConfig(c *gin.Context) {
	var req struct {
		SubUrl string `json:"sub_url"`
		SS     string `json:"ss"`   // 单个或多个节点链接（支持换行分隔的多个节点链接）
		Test   bool   `json:"test"` // 是否在添加前进行检测（默认false）
		Log    bool   `json:"log"`  // 是否返回更详细的检测日志（默认false）
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求格式"})
		return
	}
	slog.Info("API /api/config/add 请求", "sub_url", req.SubUrl, "test", req.Test, "log", req.Log, "ss_present", req.SS != "")

	// 如果是单个节点添加
	if req.SS != "" {
		if req.Test {
			// 检测后添加模式
			proxies, err := proxyutils.ParseSingleNode(req.SS)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("解析节点失败: %v", err)})
				return
			}
			if len(proxies) == 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "未能解析出有效节点"})
				return
			}

			result := app.testAndAddNodes(proxies, req.Log)
			if result.PassedNodes == 0 {
				response := gin.H{
					"error":        "节点检测失败",
					"tested_nodes": result.TestedNodes,
					"passed_nodes": result.PassedNodes,
					"failed_nodes": result.FailedNodes,
					"added_nodes":  0,
					"duration":     result.Duration,
				}
				if req.Log {
					response["logs"] = result.Logs
				}
				if result.Timeout {
					response["timeout"] = true
					response["warning"] = "部分节点因超时未完成检测"
				}
				slog.Info("API /api/config/add 响应", "status", http.StatusBadRequest, "response", response)
				c.JSON(http.StatusBadRequest, response)
				return
			}

			response := gin.H{
				"message":      "节点检测并添加成功",
				"tested_nodes": result.TestedNodes,
				"passed_nodes": result.PassedNodes,
				"failed_nodes": result.FailedNodes,
				"added_nodes":  result.AddedNodes,
				"duration":     result.Duration,
			}
			if req.Log {
				response["logs"] = result.Logs
			}
			if result.Timeout {
				response["timeout"] = true
				response["warning"] = "部分节点因超时未完成检测"
			}
			slog.Info("API /api/config/add 响应", "status", http.StatusOK, "response", response)
			c.JSON(http.StatusOK, response)
			return
		}

		// 直接添加模式（支持多节点批量添加）
		proxies, err := proxyutils.ParseSingleNode(req.SS)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("解析节点失败: %v", err)})
			return
		}
		if len(proxies) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "未能解析出有效节点"})
			return
		}

		result := app.addMultipleNodesDirectly(proxies)
		if result.AddedNodes == 0 && result.DuplicateNodes == 0 {
			// 所有节点都失败
			response := gin.H{
				"error":        "所有节点添加失败",
				"tested_nodes": result.TotalNodes,
				"passed_nodes": result.TotalNodes,
				"failed_nodes": 0,
				"added_nodes":  0,
			}
			slog.Info("API /api/config/add 响应", "status", http.StatusBadRequest, "response", response)
			c.JSON(http.StatusBadRequest, response)
			return
		}

		// 返回统一格式的响应（与test:true格式一致）
		response := gin.H{
			"message":      "节点添加成功",
			"tested_nodes": result.TotalNodes,
			"passed_nodes": result.TotalNodes,
			"failed_nodes": 0,
			"added_nodes":  result.AddedNodes,
		}
		if result.DuplicateNodes > 0 {
			response["duplicate_nodes"] = result.DuplicateNodes
			response["message"] = fmt.Sprintf("成功添加 %d 个节点，%d 个节点已存在", result.AddedNodes, result.DuplicateNodes)
		}
		slog.Info("API /api/config/add 响应", "status", http.StatusOK, "response", response)
		c.JSON(http.StatusOK, response)
		return
	}

	if req.SubUrl == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sub_url或ss不能为空"})
		return
	}

	// 声明测试结果变量（如果启用了检测）
	var testResult TestResult

	// 第一次去重检查（在锁外，快速失败）
	configData, err := os.ReadFile(app.configPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取配置文件失败: %v", err)})
		return
	}

	// 解析YAML配置以检查重复
	var yamlData map[string]any
	if err := yaml.Unmarshal(configData, &yamlData); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("解析配置文件失败: %v", err)})
		return
	}

	// 获取现有的sub-urls并检查是否已存在
	if existing, ok := yamlData["sub-urls"]; ok {
		if urls, ok := existing.([]any); ok {
			for _, url := range urls {
				if strUrl, ok := url.(string); ok {
					if strUrl == req.SubUrl {
						c.JSON(http.StatusConflict, gin.H{"error": "该订阅链接已存在"})
						return
					}
				}
			}
		}
	}

	// 如果启用了检测后添加
	if req.Test {
		// 获取订阅内容
		actualUrl := util.WarpUrl(req.SubUrl)
		data, err := proxyutils.GetDateFromSubs(actualUrl)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("获取订阅失败: %v", err)})
			return
		}

		// 解析订阅为节点列表
		proxies, err := parseSubscriptionNodes(data)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("解析订阅失败: %v", err)})
			return
		}

		if len(proxies) == 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "订阅中没有有效节点"})
			return
		}

		// 检测并添加节点到数据库
		result := app.testAndAddNodes(proxies, req.Log)

		// 至少需要有一个节点通过检测
		if result.PassedNodes == 0 {
			response := gin.H{
				"error":        "订阅中没有节点通过检测",
				"sub_url":      req.SubUrl,
				"tested_nodes": result.TestedNodes,
				"passed_nodes": result.PassedNodes,
				"failed_nodes": result.FailedNodes,
				"added_nodes":  0,
				"duration":     result.Duration,
			}
			if req.Log {
				response["logs"] = result.Logs
			}
			if result.Timeout {
				response["timeout"] = true
				response["warning"] = "部分节点因超时未完成检测"
			}
			slog.Info("API /api/config/add 响应", "status", http.StatusBadRequest, "response", response)
			c.JSON(http.StatusBadRequest, response)
			return
		}

		// 有节点通过检测，保存检测结果，稍后在成功添加订阅后返回
		testResult = result
		// 继续执行添加订阅链接的逻辑...
	}

	// 第二次去重检查（在获取锁前，确保测速期间配置未被修改）
	configData2, err := os.ReadFile(app.configPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取配置文件失败: %v", err)})
		return
	}

	var yamlData2 map[string]any
	if err := yaml.Unmarshal(configData2, &yamlData2); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("解析配置文件失败: %v", err)})
		return
	}

	// 再次检查是否已存在（可能在测速期间被其他请求添加）
	if existing, ok := yamlData2["sub-urls"]; ok {
		if urls, ok := existing.([]any); ok {
			for _, url := range urls {
				if strUrl, ok := url.(string); ok {
					if strUrl == req.SubUrl {
						c.JSON(http.StatusConflict, gin.H{"error": "该订阅链接已存在"})
						return
					}
				}
			}
		}
	}

	// 二次检查通过，获取锁准备写入文件
	// 使用字符串追加的方式，保留原有格式和注释
	app.configMutex.Lock()
	defer app.configMutex.Unlock()

	file, err := os.Open(app.configPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("打开配置文件失败: %v", err)})
		return
	}
	defer file.Close()

	var newLines []string
	scanner := bufio.NewScanner(file)
	inSubUrls := false
	subUrlsIndent := ""
	lastSubUrlLine := -1
	lineNum := 0

	for scanner.Scan() {
		line := scanner.Text()
		newLines = append(newLines, line)

		// 检测 sub-urls 部分
		if !inSubUrls && (line == "sub-urls:" || line == "sub-urls: []") {
			inSubUrls = true
			lastSubUrlLine = lineNum
			// 如果是空数组，直接替换这行
			if line == "sub-urls: []" {
				newLines[lineNum] = "sub-urls:"
			}
		} else if inSubUrls {
			// 检测缩进（通常是2或4个空格）
			if len(line) > 0 && (line[0] == ' ' || line[0] == '-') {
				// 找到 sub-urls 下的项
				for i, ch := range line {
					if ch == '-' {
						subUrlsIndent = line[:i]
						lastSubUrlLine = lineNum
						break
					}
				}
			} else if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
				// 遇到新的顶级配置项，sub-urls 部分结束
				inSubUrls = false
			}
		}
		lineNum++
	}

	if err := scanner.Err(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取配置文件失败: %v", err)})
		return
	}

	// 确定缩进（默认2个空格）
	if subUrlsIndent == "" {
		subUrlsIndent = "  "
	}

	// 在 sub-urls 部分的最后添加新的 URL
	newUrlLine := fmt.Sprintf("%s- %s", subUrlsIndent, req.SubUrl)
	if lastSubUrlLine >= 0 {
		// 在 sub-urls 的最后一行后插入
		newLines = append(newLines[:lastSubUrlLine+1], append([]string{newUrlLine}, newLines[lastSubUrlLine+1:]...)...)
	} else {
		// 如果没有找到 sub-urls，添加到文件末尾
		newLines = append(newLines, "sub-urls:", newUrlLine)
	}

	// 写入更新后的配置
	newContent := ""
	for i, line := range newLines {
		newContent += line
		if i < len(newLines)-1 {
			newContent += "\n"
		}
	}

	if err := os.WriteFile(app.configPath, []byte(newContent), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存配置文件失败: %v", err)})
		return
	}

	// 配置文件监听器会自动重新加载配置
	// 根据是否启用了检测返回不同的响应
	if req.Test {
		// 返回详细的检测结果
		response := gin.H{
			"message":      "订阅检测并添加成功",
			"sub_url":      req.SubUrl,
			"tested_nodes": testResult.TestedNodes,
			"passed_nodes": testResult.PassedNodes,
			"failed_nodes": testResult.FailedNodes,
			"added_nodes":  testResult.AddedNodes,
			"duration":     testResult.Duration,
		}
		if req.Log {
			response["logs"] = testResult.Logs
		}
		if testResult.Timeout {
			response["timeout"] = true
			response["warning"] = "部分节点因超时未完成检测"
		}
		slog.Info("API /api/config/add 响应", "status", http.StatusOK, "response", response)
		c.JSON(http.StatusOK, response)
	} else {
		// 原有的简单响应
		response := gin.H{
			"message": "订阅链接已添加",
			"sub_url": req.SubUrl,
		}
		slog.Info("API /api/config/add 响应", "status", http.StatusOK, "response", response)
		c.JSON(http.StatusOK, response)
	}
}

// getStatus 获取应用状态
func (app *App) getStatus(c *gin.Context) {
	// 判断是否在订阅获取阶段
	subsTotal := proxyutils.SubsFetchTotal.Load()
	subsProgress := proxyutils.SubsFetchProgress.Load()

	var available uint32
	var failed int32
	var stage string // "subscription" 或 "nodetest"
	currentStageCode := "idle"
	currentStageName := "空闲"
	statusText := "空闲"

	if proxyutils.SubsFetchActive.Load() || (subsTotal > 0 && subsProgress < subsTotal) {
		// 订阅获取阶段:显示成功获取的订阅数
		available = uint32(proxyutils.SubsFetchSuccess.Load())
		failed = proxyutils.SubsFetchFailed.Load()
		stage = "subscription"
		currentStageCode = "subscription"
		currentStageName = "订阅获取"
	} else {
		// 节点检测阶段:显示可用节点数
		available = checker.Available.Load()
		stage = "nodetest"
		if checker.CurrentTracker != nil {
			// 只取当前阶段的失败数，避免跨阶段累计（如测活失败叠加到测速失败）。
			_, _, currentStageDone, currentStageSuccess, _ := checker.CurrentTracker.GetStageInfo()
			failed = currentStageDone - currentStageSuccess
			if failed < 0 {
				failed = 0
			}
		} else {
			failed = 0
		}
	}

	if app.checking.Load() {
		statusText = "检测中"
		if app.stopping.Load() || checker.ForceClose.Load() {
			statusText = "停止中"
		}
	} else if app.stopping.Load() || checker.ForceClose.Load() {
		statusText = "停止中"
	}

	response := gin.H{
		"checking":   app.checking.Load(),
		"stopping":   app.stopping.Load() || checker.ForceClose.Load(),
		"proxyCount": checker.ProxyCount.Load(),
		"available":  available,
		"progress":   checker.Progress.Load(),
		"progressPercent": func() float64 {
			return float64(checker.Progress.Load()) / 100
		}(),
		"failed":            failed,
		"stage":             stage,
		"statusText":        statusText,
		"currentStageCode":  currentStageCode,
		"currentStageName":  currentStageName,
		"subsFetchActive":   proxyutils.SubsFetchActive.Load(),
		"subsFetchTotal":    subsTotal,
		"subsFetchSuccess":  proxyutils.SubsFetchSuccess.Load(),
		"subsFetchFailed":   proxyutils.SubsFetchFailed.Load(),
		"subsFetchProgress": subsProgress,
	}

	// 添加详细统计信息
	if checker.CurrentTracker != nil {
		totalNodes, aliveSuccess, aliveDone, speedSuccess, speedDone, mediaDone := checker.CurrentTracker.GetStats()
		currentStage, trackerStageName, currentStageDone, currentStageSuccess, currentStageTotal := checker.CurrentTracker.GetStageInfo()
		timeoutRemaining := checker.CurrentTracker.GetTimeoutRemaining()
		detailedStats := checker.CurrentTracker.GetDetailedStats()

		if stage != "subscription" {
			currentStageName = trackerStageName
			switch currentStage {
			case 0:
				currentStageCode = "alive"
			case 1:
				currentStageCode = "speed"
			case 2:
				currentStageCode = "media"
			default:
				currentStageCode = "idle"
			}
		} else {
			currentStageCode = "subscription"
			currentStageName = "订阅获取"
		}

		response["detailStats"] = gin.H{
			"totalNodes":          totalNodes,
			"aliveSuccess":        aliveSuccess,
			"aliveDone":           aliveDone,
			"speedSuccess":        speedSuccess,
			"speedDone":           speedDone,
			"mediaDone":           mediaDone,
			"subsFetchTotal":      subsTotal,
			"subsFetchSuccess":    proxyutils.SubsFetchSuccess.Load(),
			"subsFetchFailed":     proxyutils.SubsFetchFailed.Load(),
			"subsFetchProgress":   subsProgress,
			"currentStage":        currentStage,
			"currentStageName":    currentStageName,
			"currentStageDone":    currentStageDone,
			"currentStageSuccess": currentStageSuccess,
			"currentStageTotal":   currentStageTotal,
			"timeoutRemaining":    timeoutRemaining, // 超时倒计时（秒）
			"stageStats":          detailedStats,
		}
		response["currentStageCode"] = currentStageCode
		response["currentStageName"] = currentStageName
	}

	c.JSON(http.StatusOK, response)
}

// triggerCheckHandler 手动触发检测
func (app *App) triggerCheckHandler(c *gin.Context) {
	app.TriggerCheck()
	c.JSON(http.StatusOK, gin.H{"message": "已触发检测"})
}

// forceCloseHandler 强制关闭
func (app *App) forceCloseHandler(c *gin.Context) {
	app.MarkStopping()
	checker.ForceClose.Store(true)
	c.JSON(http.StatusOK, gin.H{"message": "已强制停止"})
}

// getLogs 获取最近日志
func (app *App) getLogs(c *gin.Context) {
	// 简单实现，从日志文件读取最后xx行
	logPath := TempLog()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		c.JSON(http.StatusOK, gin.H{"logs": []string{}})
		return
	}
	lines, err := ReadLastNLines(logPath, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取日志失败: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": lines})
}

// getVersion 获取版本信息
func (app *App) getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": app.version})
}

func (app *App) getUIInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"configPath": app.configPath,
		"subpath":    app.subscriptionURL(c),
	})
}

// getBlacklist 查询所有鉴权失败记录（黑名单）
func (app *App) getBlacklist(c *gin.Context) {
	db, err := output.GetDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库未初始化"})
		return
	}

	records, err := db.QueryAuthRecords()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("查询黑名单失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"records": records})
}

// deleteBlacklist 按 IP 删除单条黑名单记录
func (app *App) deleteBlacklist(c *gin.Context) {
	ip := c.Param("ip")
	if ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "IP不能为空"})
		return
	}

	db, err := output.GetDB()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "数据库未初始化"})
		return
	}

	if err := db.DeleteAuthRecord(ip); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("删除失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("已删除 %s 的黑名单记录", ip)})
}

func (app *App) getSubscription(c *gin.Context) {
	ip := c.ClientIP()
	now := time.Now()
	db, err := output.GetDB()
	if err != nil {
		slog.Warn("打开订阅数据库失败", "error", err)
		c.Status(http.StatusNotFound)
		return
	}
	blocked, err := db.IsAuthBlocked(ip, now)
	if err != nil {
		slog.Warn("检查订阅封禁失败", "error", err)
		c.Status(http.StatusNotFound)
		return
	}
	if blocked {
		c.Status(http.StatusNotFound)
		return
	}

	if !validSubToken(c.Query("token")) {
		if err := db.RecordAuthFailure(ip, "sub", now, authMaxFailures, authBanDuration); err != nil {
			slog.Warn("记录订阅鉴权失败", "error", err)
		}
		c.Status(http.StatusNotFound)
		return
	}
	if err := db.ClearAuthFailure(ip); err != nil {
		slog.Debug("清理订阅鉴权失败记录失败", "error", err)
	}

	testStage := output.TestAlive
	if rawTest := strings.TrimSpace(c.Query("test")); len(rawTest) > 0 {
		parsed, err := strconv.Atoi(rawTest)
		if err != nil || parsed < output.TestAlive || parsed > output.TestMedia {
			c.Status(http.StatusNotFound)
			return
		}
		testStage = parsed
	}

	minSpeed := 0
	if rawSpeed := strings.TrimSpace(c.Query("speed")); len(rawSpeed) > 0 {
		parsed, err := strconv.Atoi(rawSpeed)
		if err != nil || parsed < 0 {
			c.Status(http.StatusNotFound)
			return
		}
		minSpeed = parsed
	}

	// speed>0 时隐含需要测速结果，自动提升 test 阶段
	if minSpeed > 0 && testStage < output.TestSpeed {
		testStage = output.TestSpeed
	}

	records, err := db.QueryRecords(testStage, minSpeed)
	if err != nil {
		slog.Warn("查询订阅失败", "error", err)
		c.Status(http.StatusNotFound)
		return
	}
	if len(records) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	proxies := make([]map[string]any, 0, len(records))
	for _, record := range records {
		if record.Proxy == nil {
			continue
		}
		proxies = append(proxies, record.Proxy)
	}
	if len(proxies) == 0 {
		c.Status(http.StatusNotFound)
		return
	}

	yamlData, err := yaml.Marshal(map[string]any{"proxies": proxies})
	if err != nil {
		slog.Warn("生成订阅内容失败", "error", err)
		c.Status(http.StatusNotFound)
		return
	}

	c.Header("Content-Type", "application/yaml; charset=utf-8")
	c.Header("Content-Disposition", "inline; filename=sub.yaml")
	c.String(http.StatusOK, string(yamlData))
}

func validSubToken(rawToken string) bool {
	expected := strings.TrimSpace(config.GlobalConfig.SubToken)
	provided := strings.TrimSpace(rawToken)
	if expected == "" || provided == "" || len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func ReadLastNLines(filePath string, n int) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	ring := make([]string, n)
	count := 0

	// 使用环形缓冲区读取
	for scanner.Scan() {
		ring[count%n] = scanner.Text()
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// 处理结果
	if count <= n {
		return ring[:count], nil
	}

	// 调整顺序，从最旧到最新
	start := count % n
	result := append(ring[start:], ring[:start]...)
	return result, nil
}

// isProxyDuplicate 判断节点是否重复（更健壮的判断方式）
func isProxyDuplicate(newProxy map[string]any, existingProxies []map[string]any) bool {
	for _, existing := range existingProxies {
		// 1. 基础字段必须匹配
		if existing["type"] != newProxy["type"] {
			continue
		}
		if existing["server"] != newProxy["server"] {
			continue
		}
		if existing["port"] != newProxy["port"] {
			continue
		}

		proxyType, _ := newProxy["type"].(string)

		// 2. 根据不同协议类型检查关键字段
		switch proxyType {
		case "vmess":
			// VMess: server + port + uuid + alterId
			if existing["uuid"] != newProxy["uuid"] {
				continue
			}
			if existing["alterId"] != newProxy["alterId"] {
				continue
			}

		case "vless":
			// VLESS: server + port + uuid + flow
			if existing["uuid"] != newProxy["uuid"] {
				continue
			}

		case "ss", "shadowsocks":
			// Shadowsocks: server + port + cipher + password
			if existing["cipher"] != newProxy["cipher"] {
				continue
			}
			if existing["password"] != newProxy["password"] {
				continue
			}

		case "ssr":
			// ShadowsocksR: server + port + cipher + password + protocol + obfs
			if existing["cipher"] != newProxy["cipher"] {
				continue
			}
			if existing["password"] != newProxy["password"] {
				continue
			}
			if existing["protocol"] != newProxy["protocol"] {
				continue
			}
			if existing["obfs"] != newProxy["obfs"] {
				continue
			}

		case "trojan":
			// Trojan: server + port + password + sni
			if existing["password"] != newProxy["password"] {
				continue
			}
			sni1, _ := existing["sni"].(string)
			sni2, _ := newProxy["sni"].(string)
			if sni1 != sni2 {
				continue
			}

		case "hysteria", "hysteria2", "hy2":
			// Hysteria: server + port + password/auth
			pass1 := existing["password"]
			pass2 := newProxy["password"]
			auth1 := existing["auth"]
			auth2 := newProxy["auth"]

			// password 和 auth 可能是不同字段名
			if pass1 != pass2 && auth1 != auth2 {
				continue
			}

		case "tuic":
			// TUIC: server + port + uuid + password
			if existing["uuid"] != newProxy["uuid"] {
				continue
			}
			if existing["password"] != newProxy["password"] {
				continue
			}

		case "anytls":
			// AnyTLS: server + port + password
			if existing["password"] != newProxy["password"] {
				continue
			}

		case "mieru":
			// Mieru: server + port + username + password
			if existing["username"] != newProxy["username"] {
				continue
			}
			if existing["password"] != newProxy["password"] {
				continue
			}

		case "sudoku":
			// Sudoku: server + port + key
			if existing["key"] != newProxy["key"] {
				continue
			}

		case "wireguard", "wg":
			// WireGuard: server + port + private-key + public-key
			privateKey1 := existing["private-key"]
			privateKey2 := newProxy["private-key"]
			if privateKey1 != privateKey2 {
				continue
			}
			publicKey1 := existing["public-key"]
			publicKey2 := newProxy["public-key"]
			if publicKey1 != publicKey2 {
				continue
			}

		case "ssh":
			// SSH: server + port + username + password/private-key
			if existing["username"] != newProxy["username"] {
				continue
			}
			// 检查 password 或 private-key
			pass1 := existing["password"]
			pass2 := newProxy["password"]
			key1 := existing["private-key"]
			key2 := newProxy["private-key"]
			if pass1 != pass2 && key1 != key2 {
				continue
			}

		case "snell":
			// Snell: server + port + psk
			if existing["psk"] != newProxy["psk"] {
				continue
			}

		case "http", "socks", "socks5", "socks4":
			// HTTP/SOCKS: server + port + username (如果有)
			user1, hasUser1 := existing["username"].(string)
			user2, hasUser2 := newProxy["username"].(string)
			if hasUser1 && hasUser2 && user1 != user2 {
				continue
			}
			// 如果都没有 username，仅依据 server+port 判断

		default:
			// 其他协议：比较 server + port + name
			if existing["name"] != newProxy["name"] {
				continue
			}
		}

		// 所有关键字段都匹配，判定为重复
		return true
	}

	return false
}

// TestResult 节点检测结果
type TestResult struct {
	TestedNodes int    // 总检测节点数
	PassedNodes int    // 通过检测的节点数
	FailedNodes int    // 失败的节点数
	AddedNodes  int    // 实际添加的节点数（排除重复）
	Duration    string // 检测耗时
	Timeout     bool   // 是否超时
	Logs        []string
}

// AddResult 节点直接添加结果（test:false模式）
type AddResult struct {
	TotalNodes     int // 总解析节点数
	AddedNodes     int // 成功添加的节点数
	DuplicateNodes int // 重复跳过的节点数
}

// addMultipleNodesDirectly 批量添加多个节点到数据库（不进行网络检测）
func (app *App) addMultipleNodesDirectly(proxies []map[string]any) AddResult {
	result := AddResult{
		TotalNodes: len(proxies),
	}

	if len(proxies) == 0 {
		return result
	}

	db, err := output.GetDB()
	if err != nil {
		slog.Warn("打开数据库失败", "error", err)
		return result
	}

	records := make([]output.DBNodeRecord, 0, len(proxies))
	for _, proxy := range proxies {
		records = append(records, output.DBNodeRecord{
			Batch:     output.BatchCurrent,
			TestStage: output.TestAlive,
			Proxy:     proxy,
		})
	}

	added, duplicates, err := db.InsertRecordsDedup(records)
	if err != nil {
		slog.Warn("写入节点到数据库失败", "error", err)
		return result
	}
	result.AddedNodes = added
	result.DuplicateNodes = duplicates
	return result
}

// testAndAddNodes 统一的节点检测和添加函数
// 并发检测所有节点，测速通过的添加到数据库
// 设置 120 秒总超时，超时后返回已完成的部分结果
func (app *App) testAndAddNodes(proxies []map[string]any, logEnabled bool) TestResult {
	startTime := time.Now()
	result := TestResult{
		TestedNodes: len(proxies),
	}

	if len(proxies) == 0 {
		result.Duration = "0s"
		return result
	}

	// 初始化 Bucket（如果未初始化）
	if checker.Bucket == nil {
		if config.GlobalConfig.TotalSpeedLimit > 0 {
			checker.Bucket = ratelimit.NewBucketWithRate(
				float64(config.GlobalConfig.TotalSpeedLimit)*1024,
				int64(config.GlobalConfig.TotalSpeedLimit)*1024,
			)
		} else {
			// 不限速
			checker.Bucket = ratelimit.NewBucketWithRate(1e9, 1e9)
		}
	}

	// 创建 120 秒超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 并发控制
	concurrent := config.GlobalConfig.GetAliveConcurrent()
	if concurrent <= 0 {
		concurrent = 5
	}
	semaphore := make(chan struct{}, concurrent)
	// 统计变量
	var passedCount, failedCount, addedCount atomic.Int32
	var wg sync.WaitGroup
	var logsMu sync.Mutex
	var logs []string
	appendLog := func(msg string) {
		if !logEnabled {
			return
		}
		logsMu.Lock()
		logs = append(logs, msg)
		logsMu.Unlock()
	}

	// 并发检测所有节点
	for _, proxy := range proxies {
		// 检查是否超时
		select {
		case <-ctx.Done():
			result.Timeout = true
			goto finish
		default:
		}

		wg.Add(1)
		semaphore <- struct{}{} // 获取信号量

		go func(proxyMap map[string]any) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放信号量

			// 检查是否已超时
			select {
			case <-ctx.Done():
				failedCount.Add(1)
				return
			default:
			}

			// 创建代理客户端
			client := checker.CreateClient(proxyMap)
			if client == nil {
				failedCount.Add(1)
				if logEnabled {
					appendLog(fmt.Sprintf("节点 %v: 创建客户端失败, type=%v, server=%v, port=%v", proxyMap["name"], proxyMap["type"], proxyMap["server"], proxyMap["port"]))
				}
				return
			}
			defer client.Close()

			// 检查是否超时
			select {
			case <-ctx.Done():
				failedCount.Add(1)
				return
			default:
			}

			// 存活检测
			var alive bool
			var err error
			if config.GlobalConfig.UnifiedDelay {
				alive, _, err = checker.CheckAliveWithWarmup(ctx, client.Client)
			} else {
				alive, err = checker.CheckAlive(ctx, client.Client)
			}
			if err != nil || !alive {
				failedCount.Add(1)
				if logEnabled {
					appendLog(fmt.Sprintf("节点 %v: 存活检测失败, alive=%v, unified_delay=%v, timeout=%d, warmup_timeout=%d, test_timeout=%d, error=%v", proxyMap["name"], alive, config.GlobalConfig.UnifiedDelay, config.GlobalConfig.Timeout, config.GlobalConfig.WarmupTimeout, config.GlobalConfig.TestTimeout, err))
				}
				return
			}

			// 检查是否超时
			select {
			case <-ctx.Done():
				failedCount.Add(1)
				return
			default:
			}

			// 速度测试
			if config.GlobalConfig.SpeedTestUrl != "" {
				_, err := checker.CheckSpeed(ctx, client.Client, client.BytesRead)
				if err != nil {
					failedCount.Add(1)
					if logEnabled {
						appendLog(fmt.Sprintf("节点 %v: 测速失败, speed_test_url=%s, download_timeout=%d, error=%v", proxyMap["name"], config.GlobalConfig.SpeedTestUrl, config.GlobalConfig.DownloadTimeout, err))
					}
					return
				}
			}

			// 测速通过，添加到数据库
			passedCount.Add(1)

			err = app.addSingleNodeFromProxy(proxyMap)
			if err == nil {
				addedCount.Add(1)
			}
			// 注意：即使添加失败（如重复），也不影响 passedCount
		}(proxy)
	}

finish:
	// 等待所有协程完成或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有任务完成
	case <-ctx.Done():
		// 超时，等待已启动的协程完成
		result.Timeout = true
		wg.Wait()
	}

	result.PassedNodes = int(passedCount.Load())
	result.FailedNodes = int(failedCount.Load())
	result.AddedNodes = int(addedCount.Load())
	result.Duration = time.Since(startTime).Round(time.Millisecond).String()
	if logEnabled {
		if len(logs) == 0 {
			logs = append(logs, fmt.Sprintf("节点检测未记录到详细失败原因, unified_delay=%v, timeout=%d, warmup_timeout=%d, test_timeout=%d, speed_test_url=%q", config.GlobalConfig.UnifiedDelay, config.GlobalConfig.Timeout, config.GlobalConfig.WarmupTimeout, config.GlobalConfig.TestTimeout, config.GlobalConfig.SpeedTestUrl))
		}
		result.Logs = logs
	}

	return result
}

// addSingleNodeFromProxy 从 proxy map 添加单个节点到数据库（内部使用）
func (app *App) addSingleNodeFromProxy(proxy map[string]any) error {
	db, err := output.GetDB()
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	testStage := output.TestAlive
	if len(strings.TrimSpace(config.GlobalConfig.SpeedTestUrl)) > 0 {
		testStage = output.TestSpeed
	}

	added, duplicates, err := db.InsertRecordsDedup([]output.DBNodeRecord{{
		Batch:     output.BatchCurrent,
		TestStage: testStage,
		Proxy:     proxy,
	}})
	if err != nil {
		return err
	}
	if added == 0 && duplicates > 0 {
		return fmt.Errorf("节点已存在")
	}
	return nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseSubscriptionNodes 解析订阅内容为节点列表
func parseSubscriptionNodes(data []byte) ([]map[string]any, error) {
	// 保存原始数据用于日志
	originalData := make([]byte, len(data))
	copy(originalData, data)

	// 尝试 base64 解码，失败就用原数据
	// 注意：去除所有空白字符，支持 URL-safe base64，自动补齐 padding
	trimmedData := strings.ReplaceAll(string(data), " ", "")
	trimmedData = strings.ReplaceAll(trimmedData, "\n", "")
	trimmedData = strings.ReplaceAll(trimmedData, "\r", "")
	trimmedData = strings.ReplaceAll(trimmedData, "\t", "")
	// 将 URL-safe base64 字符替换为标准格式
	trimmedData = strings.ReplaceAll(trimmedData, "-", "+")
	trimmedData = strings.ReplaceAll(trimmedData, "_", "/")
	// 自动补齐 padding
	padLen := len(trimmedData) % 4
	if padLen > 0 {
		trimmedData += strings.Repeat("=", 4-padLen)
	}

	wasDecoded := false
	var decodedData []byte

	if decoded, err := base64.StdEncoding.DecodeString(trimmedData); err == nil {
		// 启发式验证：检查解码结果是否是有效文本
		isValidText := true
		for i := 0; i < len(decoded) && i < 100; i++ {
			c := decoded[i]
			// 允许 Tab(9), LF(10), CR(13)
			if c == 9 || c == 10 || c == 13 {
				continue
			}
			// 其他控制字符和 DEL(127) 认为无效
			if c < 32 || c == 127 {
				isValidText = false
				break
			}
		}

		if isValidText {
			// 简单验证：解码后的内容应该包含常见协议或 proxies 关键字
			decodedStr := string(decoded)
			if strings.Contains(decodedStr, "://") || strings.Contains(decodedStr, "proxies:") {
				decodedData = decoded
				data = decoded
				wasDecoded = true
			}
		}
	}

	var proxies []map[string]any

	// 尝试 YAML 格式
	var con map[string]any
	err := yaml.Unmarshal(data, &con)
	if err == nil {
		// 验证必须包含 proxies 或 proxy-providers
		if !containsKey(con, "proxies") && !containsKey(con, "proxy-providers") {
			// 不是标准 Clash 配置，继续其他解析方式
		} else {
			// YAML 格式成功解析
			proxyInterface, ok := con["proxies"]
			if ok && proxyInterface != nil {
				proxyList, ok := proxyInterface.([]any)
				if ok {
					for _, p := range proxyList {
						if proxyMap, ok := p.(map[string]any); ok {
							proxyMap["skip-cert-verify"] = true
							if proxyMap["servername"] == nil {
								if sni, ok := proxyMap["sni"].(string); ok && strings.TrimSpace(sni) != "" {
									proxyMap["servername"] = sni
								}
							}
							// 为 WS 节点注入 client-fingerprint，提高握手成功率
							if proxyMap["network"] == "ws" && proxyMap["client-fingerprint"] == nil {
								proxyMap["client-fingerprint"] = "chrome"
							}
							// 不再注入 fingerprint 字段，避免与证书指纹验证冲突
							if wsOpts, ok := proxyMap["ws-opts"].(map[string]any); ok {
								if headers, ok := wsOpts["headers"].(map[string]any); ok {
									if host, ok := headers["Host"].(string); ok && strings.TrimSpace(host) != "" && proxyMap["servername"] == nil {
										proxyMap["servername"] = host
									}
								}
							}
							proxies = append(proxies, proxyMap)
						}
					}
					if len(proxies) > 0 {
						return proxies, nil
					}
				}
			}
		}
	}

	// 先尝试逐行提取链接（更可靠的方式）
	lines := strings.Split(string(data), "\n")
	var links []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "vmess://") ||
			strings.HasPrefix(line, "vless://") ||
			strings.HasPrefix(line, "ss://") ||
			strings.HasPrefix(line, "ssr://") ||
			strings.HasPrefix(line, "trojan://") ||
			strings.HasPrefix(line, "hysteria://") ||
			strings.HasPrefix(line, "hysteria2://") ||
			strings.HasPrefix(line, "tuic://") ||
			strings.HasPrefix(line, "anytls://") {
			links = append(links, line)
		}
	}

	// 如果找到了链接，用换行符连接后解析
	if len(links) > 0 {
		extractedData := []byte(strings.Join(links, "\n"))
		proxyList, err := convert.ConvertsV2Ray(extractedData)
		if err == nil && len(proxyList) > 0 {
			// 在提取的节点上强行注入跳过证书校验和sni等信息，防止被 CDN 或握手规则拦截
			for _, p := range proxyList {
				p["skip-cert-verify"] = true
				if p["servername"] == nil {
					if sni, ok := p["sni"].(string); ok && strings.TrimSpace(sni) != "" {
						p["servername"] = sni
					}
				}
				// 为 WS 节点注入 client-fingerprint，提高握手成功率
				if p["network"] == "ws" && p["client-fingerprint"] == nil {
					p["client-fingerprint"] = "chrome"
				}
				// 不再注入 fingerprint 字段，避免与证书指纹验证冲突
				if wsOpts, ok := p["ws-opts"].(map[string]any); ok {
					if headers, ok := wsOpts["headers"].(map[string]any); ok {
						if host, ok := headers["Host"].(string); ok && strings.TrimSpace(host) != "" && p["servername"] == nil {
							p["servername"] = host
						}
					}
				}
			}
			return proxyList, nil
		}
	}

	// 作为后备方案，直接尝试 V2Ray 链接格式（可能是单行或其他格式）
	proxyList, err := convert.ConvertsV2Ray(data)
	if err == nil && len(proxyList) > 0 {
		// 为 WS 节点注入 skip-cert-verify 和 client-fingerprint，提高握手成功率
		for _, p := range proxyList {
			p["skip-cert-verify"] = true
			if p["network"] == "ws" && p["client-fingerprint"] == nil {
				p["client-fingerprint"] = "chrome"
			}
			// 不再注入 fingerprint 字段，避免与证书指纹验证冲突
		}
		return proxyList, nil
	}

	// 解析失败，清理旧日志并将数据写入日志文件以便调试
	cleanupOldParseLogs()

	logFile := fmt.Sprintf("parse_error_%d.log", time.Now().Unix())
	if f, err := os.Create(logFile); err == nil {
		defer f.Close()
		f.WriteString(fmt.Sprintf("=== 解析失败时间: %s ===\n", time.Now().Format("2006-01-02 15:04:05")))

		// 字符编码信息
		f.WriteString("\n=== 字符编码检测 ===\n")
		if len(originalData) >= 3 && originalData[0] == 0xEF && originalData[1] == 0xBB && originalData[2] == 0xBF {
			f.WriteString("检测到 UTF-8 BOM: 是\n")
		} else {
			f.WriteString("检测到 UTF-8 BOM: 否\n")
		}
		f.WriteString(fmt.Sprintf("数据有效性: UTF-8=%v\n", utf8.Valid(originalData)))

		// 原始数据
		f.WriteString("\n=== 原始数据 ===\n")
		f.WriteString(fmt.Sprintf("长度: %d 字节\n", len(originalData)))
		f.WriteString(fmt.Sprintf("内容（前1000字节）:\n%s\n", string(originalData[:min(1000, len(originalData))])))

		// Base64 解码信息
		f.WriteString("\n=== Base64 解码尝试 ===\n")
		if wasDecoded {
			f.WriteString("解码结果: 成功\n")
			f.WriteString(fmt.Sprintf("解码后长度: %d 字节\n", len(decodedData)))
			f.WriteString(fmt.Sprintf("解码后内容（前1000字节）:\n%s\n", string(decodedData[:min(1000, len(decodedData))])))
		} else {
			f.WriteString("解码结果: 未解码或解码失败\n")
		}

		// YAML 解析详情
		f.WriteString("\n=== YAML 解析详情 ===\n")
		var con map[string]any
		if err := yaml.Unmarshal(data, &con); err == nil {
			f.WriteString("YAML 语法: 有效\n")
			f.WriteString(fmt.Sprintf("包含 'proxies' 字段: %v\n", containsKey(con, "proxies")))
			f.WriteString(fmt.Sprintf("包含 'proxy-providers' 字段: %v\n", containsKey(con, "proxy-providers")))

			if proxyInterface, ok := con["proxies"]; ok && proxyInterface != nil {
				if proxyList, ok := proxyInterface.([]any); ok {
					f.WriteString(fmt.Sprintf("proxies 数组长度: %d\n", len(proxyList)))
					if len(proxyList) > 0 {
						f.WriteString(fmt.Sprintf("第一个元素类型: %T\n", proxyList[0]))
						if pm, ok := proxyList[0].(map[string]any); ok {
							f.WriteString("第一个元素的前 5 个字段:\n")
							count := 0
							for k, v := range pm {
								if count >= 5 {
									break
								}
								f.WriteString(fmt.Sprintf("  %s: %v (类型: %T)\n", k, v, v))
								count++
							}
						}
					}
				} else {
					f.WriteString(fmt.Sprintf("proxies 类型错误: %T（期望 []any）\n", proxyInterface))
				}
			}
		} else {
			f.WriteString(fmt.Sprintf("YAML 语法: 无效 - %v\n", err))
		}

		// 前 20 个节点名称（检查乱码）
		if len(proxies) > 0 {
			f.WriteString("\n=== 前 20 个节点名称 ===\n")
			for i := 0; i < min(20, len(proxies)); i++ {
				if name, ok := proxies[i]["name"]; ok {
					f.WriteString(fmt.Sprintf("%d: %v\n", i+1, name))
				}
			}
		}

		// 提取的链接信息
		f.WriteString("\n=== 最终处理的数据 ===\n")
		f.WriteString(fmt.Sprintf("长度: %d 字节\n", len(data)))
		f.WriteString(fmt.Sprintf("提取的链接数: %d\n", len(links)))

		if len(links) > 0 {
			f.WriteString("\n=== 提取的链接（前10个）===\n")
			for i, link := range links[:min(10, len(links))] {
				f.WriteString(fmt.Sprintf("%d: %s\n", i+1, link))
			}
		}
	}

	return nil, fmt.Errorf("无法解析订阅内容")
}

// containsKey 检查 map 中是否包含指定的 key
func containsKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// cleanupOldParseLogs 清理超过 2 天的 parse_error 日志文件
func cleanupOldParseLogs() {
	files, err := filepath.Glob("parse_error_*.log")
	if err != nil {
		slog.Debug("查找 parse_error 日志文件失败", "error", err)
		return
	}

	now := time.Now()
	twoDaysAgo := now.AddDate(0, 0, -2).Truncate(24 * time.Hour)

	for _, file := range files {
		// 从文件名提取时间戳：parse_error_<timestamp>.log
		basename := filepath.Base(file)
		if !strings.HasPrefix(basename, "parse_error_") || !strings.HasSuffix(basename, ".log") {
			continue
		}

		timestampStr := strings.TrimPrefix(basename, "parse_error_")
		timestampStr = strings.TrimSuffix(timestampStr, ".log")

		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			slog.Debug("解析日志文件时间戳失败", "file", file, "error", err)
			continue
		}

		fileTime := time.Unix(timestamp, 0)
		fileDateOnly := fileTime.Truncate(24 * time.Hour)

		// 删除超过 2 天的日志（保留今天和昨天）
		if fileDateOnly.Before(twoDaysAgo) {
			if err := os.Remove(file); err != nil {
				slog.Debug("删除旧日志文件失败", "file", file, "error", err)
			} else {
				slog.Debug("删除旧日志文件", "file", file, "date", fileDateOnly.Format("2006-01-02"))
			}
		}
	}
}

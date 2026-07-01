package core

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/55gY/subs-check/config"
	proxyutils "github.com/55gY/subs-check/provider"
	"github.com/55gY/subs-check/util"
	"github.com/gin-gonic/gin"
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

	// 暂停文件监听，防止写入过程触发fsnotify导致配置被重复加载
	if app.watcher != nil {
		app.watcher.Remove(filepath.Dir(app.configPath))
		defer func() {
			if err := app.watcher.Add(filepath.Dir(app.configPath)); err != nil {
				slog.Warn("恢复配置文件监听失败", "error", err)
			}
		}()
	}

	if err := os.WriteFile(app.configPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存配置文件失败: %v", err)})
		return
	}

	// 配置文件监听器恢复后会自动重新加载配置
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
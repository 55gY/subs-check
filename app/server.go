package app

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/55gY/subs-check/check"
	"github.com/55gY/subs-check/config"
	proxyutils "github.com/55gY/subs-check/proxy"
	"github.com/55gY/subs-check/save/method"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// initHttpServer 初始化HTTP服务器
func (app *App) initHttpServer() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	saver, err := method.NewLocalSaver()
	if err != nil {
		return fmt.Errorf("获取http监听目录失败: %w", err)
	}

	// 静态文件路由 - 订阅服务相关，始终启用
	// 最初不应该不带路径，现在保持兼容
	router.StaticFile("/all.yaml", saver.OutputPath+"/all.yaml")
	router.StaticFile("/all.txt", saver.OutputPath+"/all.txt")
	router.StaticFile("/base64.txt", saver.OutputPath+"/base64.txt")
	router.StaticFile("/mihomo.yaml", saver.OutputPath+"/mihomo.yaml")
	router.StaticFile("/ACL4SSR_Online_Full.yaml", saver.OutputPath+"/ACL4SSR_Online_Full.yaml")
	// // CM佬用的布丁狗
	// router.StaticFile("/bdg.yaml", saver.OutputPath+"/bdg.yaml")

	router.Static("/sub/", saver.OutputPath)

	// 根据配置决定是否启用Web控制面板
	if config.GlobalConfig.EnableWebUI {
		if config.GlobalConfig.APIKey == "" {
			if apiKey := os.Getenv("API_KEY"); apiKey != "" {
				config.GlobalConfig.APIKey = apiKey
			} else {
				config.GlobalConfig.APIKey = GenerateSimpleKey()
				slog.Warn("未设置api-key，已生成一个随机api-key", "api-key", config.GlobalConfig.APIKey)
			}
		}
		slog.Info("启用Web控制面板", "path", "http://ip:port/admin", "api-key", config.GlobalConfig.APIKey)

		// 设置模板加载 - 只有在启用Web控制面板时才加载
		router.SetHTMLTemplate(template.Must(template.New("").ParseFS(configFS, "templates/*.html")))

		// API路由
		api := router.Group("/api")
		api.Use(app.authMiddleware(config.GlobalConfig.APIKey)) // 添加认证中间件
		{
			// 配置相关API
			api.GET("/config", app.getConfig)
			api.POST("/config", app.updateConfig)
			api.POST("/config/add", app.addConfig)

			// 状态相关API
			api.GET("/status", app.getStatus)
			api.POST("/trigger-check", app.triggerCheckHandler)
			api.POST("/force-close", app.forceCloseHandler)
			// 版本相关API
			api.GET("/version", app.getVersion)

			// 日志相关API
			api.GET("/logs", app.getLogs)
		}

		// 配置页面
		router.GET("/admin", func(c *gin.Context) {
			// 构建订阅路径
			scheme := "http"
			if c.Request.TLS != nil {
				scheme = "https"
			}
			host := c.Request.Host
			subpath := fmt.Sprintf("%s://%s/sub/all.yaml", scheme, host)

			c.HTML(http.StatusOK, "admin.html", gin.H{
				"configPath": app.configPath,
				"subpath":    subpath,
			})
		})
	} else {
		slog.Info("Web控制面板已禁用")
	}

	// 启动HTTP服务器
	go func() {
		for {
			if err := router.Run(config.GlobalConfig.ListenPort); err != nil {
				slog.Error(fmt.Sprintf("HTTP服务器启动失败，正在重启中: %v", err))
			}
			time.Sleep(30 * time.Second)
		}
	}()
	slog.Info("HTTP服务器启动", "port", config.GlobalConfig.ListenPort)
	return nil
}

// authMiddleware API认证中间件
func (app *App) authMiddleware(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(key)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "无效的API密钥"})
			return
		}
		c.Next()
	}
}

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

	// 写入新配置
	if err := os.WriteFile(app.configPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存配置文件失败: %v", err)})
		return
	}

	// 配置文件监听器会自动重新加载配置
	c.JSON(http.StatusOK, gin.H{"message": "配置已更新"})
}

// addConfig 向配置文件的sub-urls中新增一条数据，或添加单个节点到all.yaml
func (app *App) addConfig(c *gin.Context) {
	var req struct {
		SubUrl string `json:"sub_url"`
		SS     string `json:"ss"` // 单个节点链接（支持vmess/ss/trojan等）
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求格式"})
		return
	}

	// 如果是单个节点添加
	if req.SS != "" {
		if err := app.addSingleNode(req.SS); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("添加节点失败: %v", err)})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "节点已添加"})
		return
	}

	if req.SubUrl == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "sub_url或ss不能为空"})
		return
	}

	// 读取现有配置
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

	// 使用字符串追加的方式，保留原有格式和注释
	// 重新读取文件以逐行处理
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
	c.JSON(http.StatusOK, gin.H{
		"message": "订阅链接已添加",
		"sub_url": req.SubUrl,
	})
}

// getStatus 获取应用状态
func (app *App) getStatus(c *gin.Context) {
	// 判断是否在订阅获取阶段
	subsTotal := proxyutils.SubsFetchTotal.Load()
	subsProgress := proxyutils.SubsFetchProgress.Load()

	var available uint32
	var failed int32
	var stage string // "subscription" 或 "nodetest"

	if subsTotal > 0 && subsProgress < subsTotal {
		// 订阅获取阶段:显示成功获取的订阅数
		available = uint32(proxyutils.SubsFetchSuccess.Load())
		failed = proxyutils.SubsFetchFailed.Load()
		stage = "subscription"
	} else {
		// 节点检测阶段:显示可用节点数
		available = check.Available.Load()
		// 节点阶段的失败数 = 已处理 - 可用数
		failed = int32(check.Progress.Load()) - int32(available)
		stage = "nodetest"
	}

	response := gin.H{
		"checking":   app.checking.Load(),
		"proxyCount": check.ProxyCount.Load(),
		"available":  available,
		"progress":   check.Progress.Load(),
		"failed":     failed,
		"stage":      stage,
	}

	// 添加详细统计信息
	if check.CurrentTracker != nil {
		totalNodes, aliveSuccess, aliveDone, speedSuccess, speedDone, mediaDone := check.CurrentTracker.GetStats()
		response["detailStats"] = gin.H{
			"totalNodes":   totalNodes,
			"aliveSuccess": aliveSuccess,
			"aliveDone":    aliveDone,
			"speedSuccess": speedSuccess,
			"speedDone":    speedDone,
			"mediaDone":    mediaDone,
		}
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
	check.ForceClose.Store(true)
	c.JSON(http.StatusOK, gin.H{"message": "已强制关闭"})
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

// getLogs 获取最近日志
func (app *App) getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": app.version})
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

func GenerateSimpleKey() string {
	return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
}

// addSingleNode 添加单个节点到all.yaml
func (app *App) addSingleNode(nodeLink string) error {
	// 1. 解析节点链接
	proxies, err := proxyutils.ParseSingleNode(nodeLink)
	if err != nil {
		return fmt.Errorf("解析节点失败: %w", err)
	}

	if len(proxies) == 0 {
		return fmt.Errorf("未能解析出有效节点")
	}

	// 2. 读取现有的all.yaml
	saver, err := method.NewLocalSaver()
	if err != nil {
		return fmt.Errorf("创建保存器失败: %w", err)
	}

	allYamlPath := saver.OutputPath + "/all.yaml"
	var existingConfig map[string]any

	if data, err := os.ReadFile(allYamlPath); err == nil {
		if err := yaml.Unmarshal(data, &existingConfig); err != nil {
			return fmt.Errorf("解析all.yaml失败: %w", err)
		}
	} else {
		// 文件不存在，创建新配置
		existingConfig = make(map[string]any)
	}

	// 3. 获取现有proxies列表
	var existingProxies []map[string]any
	if proxiesInterface, ok := existingConfig["proxies"]; ok {
		if proxiesList, ok := proxiesInterface.([]any); ok {
			for _, p := range proxiesList {
				if proxyMap, ok := p.(map[string]any); ok {
					existingProxies = append(existingProxies, proxyMap)
				}
			}
		}
	}

	// 4. 检查是否已存在（使用更健壮的判断方式）
	newProxy := proxies[0]
	if isProxyDuplicate(newProxy, existingProxies) {
		return fmt.Errorf("节点已存在")
	}

	// 5. 添加新节点
	existingProxies = append(existingProxies, newProxy)
	existingConfig["proxies"] = existingProxies

	// 6. 写回文件
	newData, err := yaml.Marshal(existingConfig)
	if err != nil {
		return fmt.Errorf("序列化配置失败: %w", err)
	}

	if err := os.MkdirAll(saver.OutputPath, 0755); err != nil {
		return fmt.Errorf("创建输出目录失败: %w", err)
	}

	if err := os.WriteFile(allYamlPath, newData, 0644); err != nil {
		return fmt.Errorf("写入all.yaml失败: %w", err)
	}

	return nil
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

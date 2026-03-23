package core

import (
	"crypto/subtle"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/55gY/subs-check/config"
	"github.com/55gY/subs-check/output"
	"github.com/gin-gonic/gin"
)

//go:embed admin.html
var templateFS embed.FS

// initHttpServer 初始化HTTP服务器
func (app *App) initHttpServer() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	saver, err := output.NewLocalSaver()
	if err != nil {
		return fmt.Errorf("获取http监听目录失败: %w", err)
	}

	// 静态文件路由 - 订阅服务
	router.StaticFile("/all.yaml", saver.OutputPath+"/all.yaml")
	router.GET("/sub", app.getSubscription)
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
		router.SetHTMLTemplate(template.Must(template.New("").ParseFS(templateFS, "admin.html")))

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
			subpath := fmt.Sprintf("%s://%s/sub", scheme, host)

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

func GenerateSimpleKey() string {
	return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
}

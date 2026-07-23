package core

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/55gY/subs-check/config"
	"github.com/55gY/subs-check/output"
	"github.com/gin-gonic/gin"
)

const (
	authMaxFailures = 3
	authBanDuration = 30 * 24 * time.Hour
)

//go:embed admin.html
var templateFS embed.FS

// initHttpServer 初始化HTTP服务器
func (app *App) initHttpServer() error {
	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()

	if strings.TrimSpace(config.GlobalConfig.SubToken) == "" {
		config.GlobalConfig.SubToken = GenerateSecureToken()
		slog.Warn("未设置sub-token，已生成一个随机sub-token", "sub-token", config.GlobalConfig.SubToken)
		// 将生成的 token 写回配置文件，避免重启后再次生成
		if data, err := os.ReadFile(app.configPath); err == nil {
			updated := upsertTopLevelConfigValue(string(data), "sub-token", config.GlobalConfig.SubToken, "api-key")
			_ = os.WriteFile(app.configPath, []byte(updated), 0644)
		}
	}

	// 订阅服务只保留动态 DB 输出路径。
	router.GET("/sub", app.getSubscription)

	// 根据配置决定是否启用Web控制面板
	if config.GlobalConfig.EnableWebUI {
		if config.GlobalConfig.APIKey == "" {
			if apiKey := os.Getenv("API_KEY"); apiKey != "" {
				config.GlobalConfig.APIKey = apiKey
			} else {
				config.GlobalConfig.APIKey = GenerateSimpleKey()
				slog.Warn("未设置api-key，已生成一个随机api-key", "api-key", config.GlobalConfig.APIKey)
				// 将生成的 key 写回配置文件，避免重启后再次生成
				if data, err := os.ReadFile(app.configPath); err == nil {
					updated := upsertTopLevelConfigValue(string(data), "api-key", config.GlobalConfig.APIKey, "enable-web-ui")
					_ = os.WriteFile(app.configPath, []byte(updated), 0644)
				}
			}
		}
		slog.Info("启用Web控制面板", "path", "http://ip:port/admin", "api-key", config.GlobalConfig.APIKey)

		// 设置模板加载 - 只有在启用Web控制面板时才加载
		router.SetHTMLTemplate(template.Must(template.New("").ParseFS(templateFS, "admin.html")))

		// API路由
		api := router.Group("/api")
		api.Use(app.authMiddleware()) // 添加认证中间件
		{
			// 配置相关API
			api.GET("/ui-info", app.getUIInfo)
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

			// 黑名单管理API
			api.GET("/blacklist", app.getBlacklist)
			api.DELETE("/blacklist/:ip", app.deleteBlacklist)
		}

		// 配置页面
		router.GET("/admin", func(c *gin.Context) {
			c.HTML(http.StatusOK, "admin.html", gin.H{
				"configPath": "验证 API 密钥后显示",
				"subpath":    "验证 API 密钥后显示",
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
// 每次请求实时读取 config.GlobalConfig.APIKey，支持热更新
func (app *App) authMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()
		db, err := output.GetDB()
		if err != nil {
			slog.Warn("打开鉴权数据库失败", "error", err)
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		blocked, err := db.IsAuthBlocked(ip, now)
		if err != nil {
			slog.Warn("检查API封禁失败", "error", err)
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if blocked {
			c.AbortWithStatus(http.StatusNotFound)
			return
		}

		apiKey := c.GetHeader("X-API-Key")
		if apiKey == "" {
			if authHeader := strings.TrimSpace(c.GetHeader("Authorization")); strings.HasPrefix(authHeader, "Bearer ") {
				apiKey = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
			}
		}
		if !constantTimeEqual(apiKey, config.GlobalConfig.APIKey) {
			if err := db.RecordAuthFailure(ip, "api", now, authMaxFailures, authBanDuration); err != nil {
				slog.Warn("记录API鉴权失败", "error", err)
			}
			c.AbortWithStatus(http.StatusNotFound)
			return
		}
		if err := db.ClearAuthFailure(ip); err != nil {
			slog.Debug("清理API鉴权失败记录失败", "error", err)
		}
		c.Next()
	}
}

func (app *App) subscriptionURL(c *gin.Context) string {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/sub?token=%s", scheme, c.Request.Host, url.QueryEscape(config.GlobalConfig.SubToken))
}

func constantTimeEqual(provided, expected string) bool {
	provided = strings.TrimSpace(provided)
	expected = strings.TrimSpace(expected)
	if len(provided) == 0 || len(expected) == 0 || len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func GenerateSimpleKey() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// 极少数环境下 crypto/rand 不可用时，退回到同样使用 crypto/rand 的强 token 生成
		return GenerateSecureToken()
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func GenerateSecureToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

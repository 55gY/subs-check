package core

import (
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/55gY/subs-check/config"
	"github.com/55gY/subs-check/output"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// getSubscription 获取订阅内容
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

// validSubToken 验证订阅token（常量时间比较防止时序攻击）
func validSubToken(rawToken string) bool {
	expected := strings.TrimSpace(config.GlobalConfig.SubToken)
	provided := strings.TrimSpace(rawToken)
	if expected == "" || provided == "" || len(expected) != len(provided) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
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
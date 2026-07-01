package core

import (
	"fmt"
	"net/http"
	"os"

	"github.com/55gY/subs-check/checker"
	proxyutils "github.com/55gY/subs-check/provider"
	"github.com/gin-gonic/gin"
)

// getStatus 获取当前检测状态
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
		if tracker := checker.CurrentTracker.Load(); tracker != nil {
			// 只取当前阶段的失败数，避免跨阶段累计（如测活失败叠加到测速失败）。
			_, _, currentStageDone, currentStageSuccess, _ := tracker.GetStageInfo()
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
	if tracker := checker.CurrentTracker.Load(); tracker != nil {
		totalNodes, aliveSuccess, aliveDone, speedSuccess, speedDone, mediaDone := tracker.GetStats()
		currentStage, trackerStageName, currentStageDone, currentStageSuccess, currentStageTotal := tracker.GetStageInfo()
		timeoutRemaining := tracker.GetTimeoutRemaining()
		detailedStats := tracker.GetDetailedStats()

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

// getUIInfo 获取UI信息
func (app *App) getUIInfo(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"configPath": app.configPath,
		"subpath":    app.subscriptionURL(c),
	})
}
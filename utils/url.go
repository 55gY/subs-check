package utils

import (
	"strings"
	"time"

	"github.com/55gY/subs-check/config"
)

// WarpUrl 处理 URL，支持 GitHub 代理和时间占位符
func WarpUrl(url string) string {
	// 处理时间占位符
	now := time.Now()
	url = strings.ReplaceAll(url, "{Ymd}", now.Format("20060102"))
	url = strings.ReplaceAll(url, "{Y}", now.Format("2006"))
	url = strings.ReplaceAll(url, "{m}", now.Format("01"))
	url = strings.ReplaceAll(url, "{d}", now.Format("02"))
	url = strings.ReplaceAll(url, "{Y-m-d}", now.Format("2006-01-02"))

	// 处理 GitHub 代理
	if config.GlobalConfig.GithubProxy != "" && strings.Contains(url, "raw.githubusercontent.com") {
		url = strings.Replace(url, "https://raw.githubusercontent.com/", config.GlobalConfig.GithubProxy, 1)
	}

	return url
}

// UpdateSubs 更新订阅（原 Sub-Store 功能已移除，保留空函数以兼容）
func UpdateSubs() {
	// Sub-Store 功能已移除，此函数保留为空
}

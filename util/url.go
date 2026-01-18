package util

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
		// 在URL前面添加代理地址，而不是替换域名
		// 例如: https://ghfast.top/https://raw.githubusercontent.com/...
		url = config.GlobalConfig.GithubProxy + url
	}

	return url
}


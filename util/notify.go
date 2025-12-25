package util

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// NotifyRequest 定义发送通知的请求结构
type NotifyRequest struct {
	URLs  string `json:"urls"`  // 通知目标的 URL（如 mailto://、discord://）
	Body  string `json:"body"`  // 通知内容
	Title string `json:"title"` // 通知标题（可选）
}

// Notify 发送通知
func Notify(request NotifyRequest, appriseApiServer string) error {
	// 构建请求体
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("构建请求体失败: %w", err)
	}

	// 发送请求
	resp, err := http.Post(appriseApiServer, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("通知失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	return nil
}

func SendNotify(appriseApiServer string, recipientUrl []string, notifyTitle string, subUrlsReTry int, length int) {
	if appriseApiServer == "" {
		return
	} else if len(recipientUrl) == 0 {
		slog.Error("没有配置通知目标")
		return
	}

	for _, url := range recipientUrl {
		request := NotifyRequest{
			URLs: url,
			Body: fmt.Sprintf("✅ 可用节点：%d\n🕒 %s",
				length,
				GetCurrentTime()),
			Title: notifyTitle,
		}
		var err error
		for i := 0; i < subUrlsReTry; i++ {
			err = Notify(request, appriseApiServer)
			if err == nil {
				slog.Info(fmt.Sprintf("%s 通知发送成功", strings.SplitN(url, "://", 2)[0]))
				break
			}
		}
		if err != nil {
			slog.Error(fmt.Sprintf("%s 发送通知失败: %v", strings.SplitN(url, "://", 2)[0], err))
		}
	}
}

func GetCurrentTime() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

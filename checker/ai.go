package checker

import (
	"context"
	"io"
	"net/http"
	"strings"
)

func doRequestAndReadAIResponse(httpClient *http.Client, req *http.Request) ([]byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return body, nil
}

func lowerBodyText(body []byte) string {
	return strings.ToLower(string(body))
}

// CheckOpenAI 检测 OpenAI 可用性
// 1.如果全部通过，ChatGPT客户端可正常使用，res.Openai = true，tag为"GPT⁺"
// 2.如果只通过cookies检测 或 client检测，res.OpenaiWeb = true，tag为"GPT"
// 经在Windows和ios客户端测试，如果仅通过一项检测，客户端很大概率不能使用，但web端很大概率可以使用。所以如果全部通过添加了一个角标"⁺",保留仅通过一项检测的tag为"GPT",web端用户几乎不需要发现标签变化。
func CheckOpenAI(ctx context.Context, httpClient *http.Client) (bool, bool) {
	return CheckCookies(ctx, httpClient), CheckClient(ctx, httpClient)
}

// CheckCookies 通过检查cookies判断网络访问
func CheckCookies(ctx context.Context, httpClient *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.openai.com/compliance/cookie_requirements", nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	body, err := doRequestAndReadAIResponse(httpClient, req)
	if err != nil {
		return false
	}
	bodyText := lowerBodyText(body)

	return !strings.Contains(bodyText, "unsupported_country")
}

// CheckClient 通过模拟客户端访问检查app可用性
func CheckClient(ctx context.Context, httpClient *http.Client) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ios.chat.openai.com", nil)
	if err != nil {
		return false
	}

	// 设置 移动设备 请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 16_6_0 like Mac OS X) AppleWebKit/537.36 (KHTML, like Gecko) Mobile/16G29 ChatGPT/3.0")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "com.openai.chatgpt")
	req.Header.Set("Referer", "https://chat.openai.com/")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Origin", "https://chat.openai.com")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.Header.Set("sec-ch-ua-mobile", "?1")

	body, err := doRequestAndReadAIResponse(httpClient, req)
	if err != nil {
		return false
	}
	bodyText := lowerBodyText(body)

	// 检查是否包含 "unsupported_country" 和 "vpn 关键词
	return !strings.Contains(bodyText, "unsupported_country") && !strings.Contains(bodyText, "vpn")
}

// CheckGemini 检测 Gemini 可用性
// https://github.com/clash-verge-rev/clash-verge-rev/blob/c894a15d13d5bcce518f8412cc393b56272a9afa/src-tauri/src/cmd/media_unlock_checker.rs#L241
func CheckGemini(ctx context.Context, httpClient *http.Client) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://gemini.google.com/", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	body, err := doRequestAndReadAIResponse(httpClient, req)
	if err != nil {
		return false, err
	}
	bodyText := lowerBodyText(body)
	return strings.Contains(bodyText, "45631641,null,true"), nil
}

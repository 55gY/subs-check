package checker

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// CheckYoutube 检测 YouTube Premium 区域
// 在body中查找 INNERTUBE_CONTEXT_GL 并提取区域代码
func CheckYoutube(httpClient *http.Client) (string, error) {
	re := regexp.MustCompile(`"INNERTUBE_CONTEXT_GL"\s*:\s*"([^"]+)"`)

	// 创建请求
	req, err := http.NewRequest("GET", "https://www.youtube.com/premium", nil)
	if err != nil {
		return "", err
	}

	// 添加请求头
	req.Header.Set("accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("accept-language", "zh-CN,zh;q=0.9")
	req.Header.Set("sec-ch-ua", `"Chromium";v="131", "Not_A Brand";v="24", "Google Chrome";v="131"`)
	req.Header.Set("sec-ch-ua-platform", `"Windows"`)
	req.Header.Set("sec-fetch-dest", "document")
	req.Header.Set("sec-fetch-mode", "navigate")
	req.Header.Set("sec-fetch-site", "none")
	req.Header.Set("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	// 发送请求
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应内容
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 送中
	if idx := strings.Index(string(body), "www.google.cn"); idx != -1 {
		return "CN", nil
	}

	if idx := strings.Index(string(body), "Premium is not available in your country"); idx != -1 {
		return "", nil
	}

	// 先检测上方是否送中，在检测位置
	match := re.FindStringSubmatch(string(body))
	if len(match) > 1 {
		region := match[1]
		if region != "" {
			return region, nil
		}
	}

	return "", nil
}

// CheckNetflix 检测 Netflix 可用性
func CheckNetflix(httpClient *http.Client) (bool, error) {
	// https://www.netflix.com/title/81280792
	req, err := http.NewRequest("GET", "https://www.netflix.com/title/81280792", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		return true, nil
	}
	return false, nil
}

// CheckDisney 检测 Disney+ 可用性
func CheckDisney(httpClient *http.Client) (bool, error) {
	// 定义常量
	const (
		cookie    = "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange&latitude=0&longitude=0&platform=browser&subject_token=DISNEYASSERTION&subject_token_type=urn%3Abamtech%3Aparams%3Aoauth%3Atoken-type%3Adevice"
		assertion = `{"deviceFamily":"browser","applicationRuntime":"chrome","deviceProfile":"windows","attributes":{}}`
		authBear  = "Bearer ZGlzbmV5JmJyb3dzZXImMS4wLjA.Cu56AgSfBTDag5NiRA81oLHkDZfu5L3CKadnefEAY84"
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"
	)

	// 第一步：获取 assertion token
	req, err := http.NewRequest("POST", "https://disney.api.edge.bamgrid.com/devices", strings.NewReader(assertion))
	if err != nil {
		return false, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", authBear)
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var assertionResp map[string]interface{}
	if err := json.Unmarshal(body, &assertionResp); err != nil {
		return false, err
	}

	assertionToken, ok := assertionResp["assertion"].(string)
	if !ok {
		return false, fmt.Errorf("无法获取 assertion token")
	}

	// 第二步：获取 access token
	tokenData := strings.Replace(cookie, "DISNEYASSERTION", assertionToken, 1)
	req, err = http.NewRequest("POST", "https://disney.api.edge.bamgrid.com/token", strings.NewReader(tokenData))
	if err != nil {
		return false, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", authBear)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err = httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var tokenResp map[string]interface{}
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return false, err
	}

	if errDesc, ok := tokenResp["error_description"].(string); ok && errDesc == "forbidden-location" {
		return false, nil
	}

	refreshToken, ok := tokenResp["refresh_token"].(string)
	if !ok {
		return false, nil
	}

	// 第三步：检查区域
	gqlQuery := fmt.Sprintf(`{"query":"mutation refreshToken($input: RefreshTokenInput!) {refreshToken(refreshToken: $input) {activeSession {sessionId}}}","variables":{"input":{"refreshToken":"%s"}}}`, refreshToken)

	req, err = http.NewRequest("POST", "https://disney.api.edge.bamgrid.com/graph/v1/device/graphql", strings.NewReader(gqlQuery))
	if err != nil {
		return false, err
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", authBear)

	resp, err = httpClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err = io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var gqlResp map[string]interface{}
	if err := json.Unmarshal(body, &gqlResp); err != nil {
		return false, err
	}

	// 检查区域信息
	extensions, ok := gqlResp["extensions"].(map[string]interface{})
	if !ok {
		return false, nil
	}

	sdk, ok := extensions["sdk"].(map[string]interface{})
	if !ok {
		return false, nil
	}

	session, ok := sdk["session"].(map[string]interface{})
	if !ok {
		return false, nil
	}

	inSupportedLocation, _ := session["inSupportedLocation"].(bool)

	return inSupportedLocation, nil
}

// CheckTikTok 检测 TikTok 区域
func CheckTikTok(httpClient *http.Client) (string, error) {
	req, err := http.NewRequest("GET", "https://www.tiktok.com/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// 使用正则匹配 "region":"XX"
	re := regexp.MustCompile(`"region"\s*:\s*"([A-Z]{2})"`)
	matches := re.FindSubmatch(body)
	if len(matches) >= 2 {
		return string(matches[1]), nil
	}
	return "", nil
}

package checker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

func doRequestAndReadBody(httpClient *http.Client, req *http.Request) (*http.Response, []byte, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}

	body, err := io.ReadAll(resp.Body)
	closeErr := resp.Body.Close()
	if err != nil {
		if closeErr != nil {
			return nil, nil, closeErr
		}
		return nil, nil, err
	}
	if closeErr != nil {
		return nil, nil, closeErr
	}

	return resp, body, nil
}

func setDisneyCommonHeaders(req *http.Request, userAgent, authorization string) {
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Authorization", authorization)
}

func setDisneyJSONHeaders(req *http.Request, userAgent, authorization string) {
	setDisneyCommonHeaders(req, userAgent, authorization)
	req.Header.Set("Content-Type", "application/json")
}

func setDisneyFormHeaders(req *http.Request, userAgent, authorization string) {
	setDisneyCommonHeaders(req, userAgent, authorization)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
}

// CheckYoutube 检测 YouTube Premium 区域
// 在body中查找 INNERTUBE_CONTEXT_GL 并提取区域代码
func CheckYoutube(ctx context.Context, httpClient *http.Client) (string, error) {
	re := regexp.MustCompile(`"INNERTUBE_CONTEXT_GL"\s*:\s*"([^"]+)"`)

	// 创建请求
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.youtube.com/premium", nil)
	if err != nil {
		return "", err
	}

	// 添加请求头
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9")
	req.Header.Set("Sec-CH-UA", `"Chromium";v="131", "Not_A Brand";v="24", "Google Chrome";v="131"`)
	req.Header.Set("Sec-CH-UA-Platform", `"Windows"`)
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36")

	// 发送请求
	_, body, err := doRequestAndReadBody(httpClient, req)
	if err != nil {
		return "", err
	}
	bodyText := string(body)

	// 送中
	if strings.Contains(bodyText, "www.google.cn") {
		return "CN", nil
	}

	if strings.Contains(bodyText, "Premium is not available in your country") {
		return "", nil
	}

	// 先检测上方是否送中，在检测位置
	match := re.FindStringSubmatch(bodyText)
	if len(match) >= 2 {
		region := match[1]
		if region != "" {
			return region, nil
		}
	}

	return "", nil
}

// CheckNetflix 检测 Netflix 可用性
func CheckNetflix(ctx context.Context, httpClient *http.Client) (bool, error) {
	// https://www.netflix.com/title/81280792
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.netflix.com/title/81280792", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")
	resp, _, err := doRequestAndReadBody(httpClient, req)
	if err != nil {
		return false, err
	}

	return resp.StatusCode == http.StatusOK, nil
}

// CheckDisney 检测 Disney+ 可用性
func CheckDisney(ctx context.Context, httpClient *http.Client) (bool, error) {
	// 定义常量
	const (
		cookie    = "grant_type=urn%3Aietf%3Aparams%3Aoauth%3Agrant-type%3Atoken-exchange&latitude=0&longitude=0&platform=browser&subject_token=DISNEYASSERTION&subject_token_type=urn%3Abamtech%3Aparams%3Aoauth%3Atoken-type%3Adevice"
		assertion = `{"deviceFamily":"browser","applicationRuntime":"chrome","deviceProfile":"windows","attributes":{}}`
		authBear  = "Bearer ZGlzbmV5JmJyb3dzZXImMS4wLjA.Cu56AgSfBTDag5NiRA81oLHkDZfu5L3CKadnefEAY84"
		userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36"
	)

	// 第一步：获取 assertion token
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://disney.api.edge.bamgrid.com/devices", strings.NewReader(assertion))
	if err != nil {
		return false, err
	}

	setDisneyJSONHeaders(req, userAgent, authBear)

	_, body, err := doRequestAndReadBody(httpClient, req)
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
	req, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://disney.api.edge.bamgrid.com/token", strings.NewReader(tokenData))
	if err != nil {
		return false, err
	}

	setDisneyFormHeaders(req, userAgent, authBear)

	_, body, err = doRequestAndReadBody(httpClient, req)
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

	req, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://disney.api.edge.bamgrid.com/graph/v1/device/graphql", strings.NewReader(gqlQuery))
	if err != nil {
		return false, err
	}

	setDisneyCommonHeaders(req, userAgent, authBear)

	_, body, err = doRequestAndReadBody(httpClient, req)
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
func CheckTikTok(ctx context.Context, httpClient *http.Client) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.tiktok.com/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36")
	resp, body, err := doRequestAndReadBody(httpClient, req)
	if err != nil {
		return "", err
	}

	if resp.StatusCode != http.StatusOK {
		return "", nil
	}

	// 使用正则匹配 "region":"XX"
	re := regexp.MustCompile(`"region"\s*:\s*"([A-Z]{2})"`)
	matches := re.FindSubmatch(body)
	if len(matches) >= 2 {
		return string(matches[1]), nil
	}
	return "", nil
}

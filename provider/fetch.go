package provider

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	u "net/url"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/55gY/subs-check/config"
	"github.com/55gY/subs-check/util"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
)

// 订阅获取进度统计(供外部读取)
var SubsFetchProgress atomic.Int32 // 已完成数
var SubsFetchTotal atomic.Int32    // 总数
var SubsFetchSuccess atomic.Int32  // 成功数
var SubsFetchFailed atomic.Int32   // 失败数

func GetProxies() ([]map[string]any, []string, []string, map[string]bool, error) {

	// 解析本地与远程订阅清单
	subUrls, localUrls, localNum, remoteNum := resolveSubUrls()
	slog.Info("订阅链接数量", "本地", localNum, "远程", remoteNum, "总计", len(subUrls))

	// 初始化订阅获取进度
	SubsFetchTotal.Store(int32(len(subUrls)))
	SubsFetchProgress.Store(0)
	SubsFetchSuccess.Store(0)
	SubsFetchFailed.Store(0)

	if len(config.GlobalConfig.NodeType) > 0 {
		slog.Info("只筛选用户设置的协议", "type", config.GlobalConfig.NodeType)
	}

	var wg sync.WaitGroup
	proxyChan := make(chan map[string]any, 1)                              // 缓冲通道存储解析的代理
	concurrentLimit := make(chan struct{}, config.GlobalConfig.Concurrent) // 限制并发数
	failedSubsChan := make(chan string, len(subUrls))                      // 收集失败的订阅链接
	successSubsChan := make(chan string, len(subUrls))                     // 收集成功的订阅链接

	// 订阅获取进度统计
	var completedTotal, failedTotal, successTotal atomic.Int32

	// 辅助函数：标记成功
	markSuccess := func(url string) {
		successSubsChan <- url
		successTotal.Add(1)
		SubsFetchSuccess.Store(successTotal.Load())
		SubsFetchProgress.Store(completedTotal.Load())
	}

	// 辅助函数：标记失败
	markFailed := func(url string) {
		failedSubsChan <- url
		failedTotal.Add(1)
		SubsFetchFailed.Store(failedTotal.Load())
		SubsFetchProgress.Store(completedTotal.Load())
	}

	// 启动进度显示(如果配置开启)
	progressDone := make(chan struct{})
	if config.GlobalConfig.PrintProgress {
		go func() {
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-progressDone:
					return
				case <-ticker.C:
					completed := completedTotal.Load()
					failed := failedTotal.Load()
					success := successTotal.Load()
					pct := float64(completed) / float64(len(subUrls)) * 100
					fmt.Printf("\r订阅获取: [%-45s] %.1f%% (%d/%d) 成功:%d 失败:%d",
						strings.Repeat("=", int(pct/2))+">",
						pct,
						completed,
						len(subUrls),
						success,
						failed)
				}
			}
		}()
	}

	// 启动收集结果的协程
	var mihomoProxies []map[string]any
	done := make(chan struct{})
	go func() {
		for proxy := range proxyChan {
			mihomoProxies = append(mihomoProxies, proxy)
		}
		done <- struct{}{}
	}()

	// 启动工作协程
	for _, subUrl := range subUrls {
		wg.Add(1)
		concurrentLimit <- struct{}{} // 获取令牌

		go func(url string) {
			defer wg.Done()
			defer func() { <-concurrentLimit }() // 释放令牌
			defer completedTotal.Add(1)

			// 节点计数器：nodeCountTotal(过滤前), nodeCountValid(过滤后)
			var nodeCountTotal, nodeCountValid int

			// 使用 WarpUrl 处理后的 URL 获取数据
			actualUrl := util.WarpUrl(url)
			data, err := GetDateFromSubs(actualUrl)
			if err != nil {
				// 记录失败的订阅链接（使用原始URL）
				markFailed(url)
				return
			}

			var tag string
			if d, err := u.Parse(url); err == nil {
				tag = d.Fragment
			}

			var con map[string]any
			err = yaml.Unmarshal(data, &con)
			if err != nil {
				proxyList, err := convert.ConvertsV2Ray(data)
				if err != nil {
					// 结构化提取失败时，回退到对原始文本进行正则提取
					fallbackLinks := extractV2RayLinks(data)
					if len(fallbackLinks) > 0 {
						extractedData := []byte(strings.Join(fallbackLinks, "\n"))
						proxyList, err = convert.ConvertsV2Ray(extractedData)
						if err != nil {
							markFailed(url)
							return
						}
						// 最终回退：将按 URL 猜测协议头处理纯文本/数组
						if guessed := convertUnStandandTextViaConvert(url, data); len(guessed) > 0 {
							for _, proxy := range guessed {
								nodeCountTotal++
								if t, ok := proxy["type"].(string); ok {
									if len(config.GlobalConfig.NodeType) > 0 && !lo.Contains(config.GlobalConfig.NodeType, t) {
										continue
									}
								}
								nodeCountValid++
								proxy["sub_url"] = url
								proxy["sub_tag"] = tag
								proxyChan <- proxy
							}
							// 检查节点数量是否满足最低要求
							if config.GlobalConfig.SubUrlsMinNodeCount > 0 && nodeCountValid < config.GlobalConfig.SubUrlsMinNodeCount {
								slog.Warn("订阅节点数量不足，已标记为失败",
									"url", url,
									"过滤前节点数", nodeCountTotal,
									"过滤后节点数", nodeCountValid,
									"最低要求", config.GlobalConfig.SubUrlsMinNodeCount)
								markFailed(url)
								return
							}
							markSuccess(url)
							return
						}
					} else {
						slog.Debug(fmt.Sprintf("解析proxy错误: %v", err), "url", url)
						markFailed(url)
						return
					}
				}
				for _, proxy := range proxyList {
					nodeCountTotal++
					// 只测试指定协议
					if t, ok := proxy["type"].(string); ok {
						if len(config.GlobalConfig.NodeType) > 0 && !lo.Contains(config.GlobalConfig.NodeType, t) {
							continue
						}
					}
					nodeCountValid++

					// 为每个节点添加订阅链接来源信息和备注
					proxy["sub_url"] = url
					proxy["sub_tag"] = tag
					proxyChan <- proxy
				}
				// 检查节点数量是否满足最低要求
				if config.GlobalConfig.SubUrlsMinNodeCount > 0 && nodeCountValid < config.GlobalConfig.SubUrlsMinNodeCount {
					slog.Warn("订阅节点数量不足，已标记为失败",
						"url", url,
						"过滤前节点数", nodeCountTotal,
						"过滤后节点数", nodeCountValid,
						"最低要求", config.GlobalConfig.SubUrlsMinNodeCount)
					markFailed(url)
					return
				}
				markSuccess(url)
				return
			}

			proxyInterface, ok := con["proxies"]
			if !ok || proxyInterface == nil {
				markFailed(url)
				return
			}

			proxyList, ok := proxyInterface.([]any)
			if !ok {
				markFailed(url)
				return
			}
			// 若 proxies 是字符串数组（ip:port），按 URL 猜测协议头后统一走 ConvertsV2Ray
			{
				strArr := make([]string, 0, len(proxyList))
				for _, it := range proxyList {
					if s, ok := it.(string); ok {
						s = strings.TrimSpace(s)
						if s != "" {
							strArr = append(strArr, s)
						}
					}
				}
				if len(strArr) > 0 {
					con2 := map[string]any{guessSchemeByURL(url): strArr}
					converted := convertUnStandandJsonViaConvert(con2)
					if len(converted) > 0 {
						for _, proxy := range converted {
							nodeCountTotal++
							if t, ok := proxy["type"].(string); ok {
								if len(config.GlobalConfig.NodeType) > 0 && !lo.Contains(config.GlobalConfig.NodeType, t) {
									continue
								}
							}
							nodeCountValid++
							proxy["sub_url"] = url
							proxy["sub_tag"] = tag
							proxyChan <- proxy
						}
						// 检查节点数量是否满足最低要求
						if config.GlobalConfig.SubUrlsMinNodeCount > 0 && nodeCountValid < config.GlobalConfig.SubUrlsMinNodeCount {
							slog.Warn("订阅节点数量不足，已标记为失败",
								"url", url,
								"过滤前节点数", nodeCountTotal,
								"过滤后节点数", nodeCountValid,
								"最低要求", config.GlobalConfig.SubUrlsMinNodeCount)
							markFailed(url)
							return
						}
						markSuccess(url)
						return
					}
				}
			}
			for _, proxy := range proxyList {
				if proxyMap, ok := proxy.(map[string]any); ok {
					nodeCountTotal++
					if t, ok := proxyMap["type"].(string); ok {
						// 只测试指定协议
						if len(config.GlobalConfig.NodeType) > 0 && !lo.Contains(config.GlobalConfig.NodeType, t) {
							continue
						}
						// 虽然支持mihomo支持下划线，但是这里为了规范，还是改成横杠
						// todo: 不知道后边还有没有这类问题
						switch t {
						case "hysteria2", "hy2":
							if _, ok := proxyMap["obfs_password"]; ok {
								proxyMap["obfs-password"] = proxyMap["obfs_password"]
								delete(proxyMap, "obfs_password")
							}
						}
					}
					nodeCountValid++
					// 为每个节点添加订阅链接来源信息和备注
					proxyMap["sub_url"] = url
					proxyMap["sub_tag"] = tag
					proxyChan <- proxyMap
				}
			}
			// 检查节点数量是否满足最低要求
			if config.GlobalConfig.SubUrlsMinNodeCount > 0 && nodeCountValid < config.GlobalConfig.SubUrlsMinNodeCount {
				slog.Warn("订阅节点数量不足，已标记为失败",
					"url", url,
					"过滤前节点数", nodeCountTotal,
					"过滤后节点数", nodeCountValid,
					"最低要求", config.GlobalConfig.SubUrlsMinNodeCount)
				markFailed(url)
				return
			}
			markSuccess(url)
		}(subUrl)
	}

	// 等待所有工作协程完成
	wg.Wait()
	close(proxyChan)
	close(failedSubsChan)
	close(successSubsChan)

	// 关闭进度显示
	if config.GlobalConfig.PrintProgress {
		close(progressDone)
		fmt.Println() // 换行
	}

	<-done // 等待收集完成

	// 收集失败和成功的订阅链接
	failedSubs := make([]string, 0)
	for failedUrl := range failedSubsChan {
		failedSubs = append(failedSubs, failedUrl)
	}

	successSubs := make([]string, 0)
	for successUrl := range successSubsChan {
		successSubs = append(successSubs, successUrl)
	}

	return mihomoProxies, failedSubs, successSubs, localUrls, nil
}

// from 3k
// resolveSubUrls 合并本地与远程订阅清单并去重
func resolveSubUrls() ([]string, map[string]bool, int, int) {
	// 计数
	var localNum, remoteNum int
	localNum = len(config.GlobalConfig.SubUrls)

	// 创建本地订阅标识映射
	localUrls := make(map[string]bool)

	urls := make([]string, 0, len(config.GlobalConfig.SubUrls))
	// 本地配置
	for _, url := range config.GlobalConfig.SubUrls {
		urls = append(urls, url)
		localUrls[url] = true
	}

	// 远程清单
	if len(config.GlobalConfig.SubUrlsRemote) != 0 {
		for _, d := range config.GlobalConfig.SubUrlsRemote {
			if remote, err := fetchRemoteSubUrls(util.WarpUrl(d)); err != nil {
				slog.Warn("获取远程订阅清单失败，已忽略", "err", err)
			} else {
				remoteNum += len(remote)
				urls = append(urls, remote...)
				// 远程订阅不添加到 localUrls
			}
		}

	}

	// 规范化与去重
	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, s := range urls {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "#") { // 跳过空行与注释
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out, localUrls, localNum, remoteNum
}

// fetchRemoteSubUrls 从远程地址读取订阅URL清单
// 支持两种格式：
// 1) 纯文本，按换行分隔，支持以 # 开头的注释与空行
// 2) YAML/JSON 的字符串数组
func fetchRemoteSubUrls(listURL string) ([]string, error) {
	if listURL == "" {
		return nil, errors.New("empty list url")
	}
	data, err := GetDateFromSubs(listURL)
	if err != nil {
		return nil, err
	}

	// 优先尝试解析为字符串数组（YAML/JSON兼容）
	var arr []string
	if err := yaml.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		return arr, nil
	}

	// 回退为按行解析
	res := make([]string, 0, 16)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		res = append(res, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// 订阅链接中获取数据
func GetDateFromSubs(subUrl string) ([]byte, error) {
	maxRetries := config.GlobalConfig.SubUrlsReTry
	// 重试间隔
	retryInterval := config.GlobalConfig.SubUrlsRetryInterval
	if retryInterval == 0 {
		retryInterval = 1
	}
	// 超时时间
	timeout := config.GlobalConfig.SubUrlsTimeout
	if timeout == 0 {
		timeout = 10
	}

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	// 第一阶段：直连重试
	var directErr error
	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(retryInterval) * time.Second)
		}

		req, err := http.NewRequest("GET", subUrl, nil)
		if err != nil {
			directErr = err
			continue
		}

		if config.GlobalConfig.SubUrlsGetUA == "random" {
			req.Header.Set("User-Agent", convert.RandUserAgent())
		} else {
			req.Header.Set("User-Agent", config.GlobalConfig.SubUrlsGetUA)
		}

		resp, err := client.Do(req)
		if err != nil {
			directErr = err
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			directErr = fmt.Errorf("订阅链接: %s 返回状态码: %d", subUrl, resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			directErr = fmt.Errorf("读取订阅链接: %s 数据错误: %v", subUrl, err)
			continue
		}
		return body, nil
	}

	// 直连失败，检查是否配置了订阅代理
	subProxies := config.GlobalConfig.SubUrlsProxy
	if len(subProxies) == 0 {
		// 未配置订阅代理，直接返回失败
		slog.Warn("直连获取订阅失败，未配置订阅代理", "url", subUrl, "error", directErr)
		return nil, fmt.Errorf("直连重试%d次后失败: %v", maxRetries, directErr)
	}

	// 第二阶段：使用订阅代理重试
	slog.Info("直连获取订阅失败，尝试使用订阅代理", "url", subUrl, "proxy_count", len(subProxies))

	for i, proxyUrl := range subProxies {
		// 对原始订阅URL进行编码后拼接到代理URL
		encodedUrl := u.QueryEscape(subUrl)
		proxyFullUrl := proxyUrl + encodedUrl

		slog.Info("使用订阅代理", "index", fmt.Sprintf("%d/%d", i+1, len(subProxies)), "proxy", proxyUrl)

		req, err := http.NewRequest("GET", proxyFullUrl, nil)
		if err != nil {
			slog.Warn("创建代理请求失败", "proxy", proxyUrl, "error", err)
			continue
		}

		if config.GlobalConfig.SubUrlsGetUA == "random" {
			req.Header.Set("User-Agent", convert.RandUserAgent())
		} else {
			req.Header.Set("User-Agent", config.GlobalConfig.SubUrlsGetUA)
		}

		resp, err := client.Do(req)
		if err != nil {
			slog.Warn("订阅代理请求失败", "proxy", proxyUrl, "error", err)
			continue
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			slog.Warn("订阅代理返回非200状态码", "proxy", proxyUrl, "status", resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Warn("读取代理响应失败", "proxy", proxyUrl, "error", err)
			continue
		}

		slog.Info("订阅代理获取成功", "proxy", proxyUrl)
		return body, nil
	}

	// 所有代理都失败
	slog.Error("直连和所有订阅代理均失败", "url", subUrl, "direct_error", directErr, "proxy_count", len(subProxies))
	return nil, fmt.Errorf("直连重试%d次失败，所有订阅代理(%d个)也失败: %v", maxRetries, len(subProxies), directErr)
}

func extractV2RayLinks(data []byte) []string {
	re := regexp.MustCompile(`(?i)(vmess|vless|ss|ssr|trojan|hysteria|hysteria2|tuic|juicity)://[a-zA-Z0-9\-_@.:/?=&%#]+`)
	matches := re.FindAllString(string(data), -1)
	return matches
}

func guessSchemeByURL(url string) string {
	lower := strings.ToLower(url)
	if strings.Contains(lower, "vmess") {
		return "vmess"
	}
	if strings.Contains(lower, "vless") {
		return "vless"
	}
	if strings.Contains(lower, "ss") || strings.Contains(lower, "shadowsocks") {
		return "ss"
	}
	if strings.Contains(lower, "ssr") {
		return "ssr"
	}
	if strings.Contains(lower, "trojan") {
		return "trojan"
	}
	if strings.Contains(lower, "hysteria2") || strings.Contains(lower, "hy2") {
		return "hysteria2"
	}
	if strings.Contains(lower, "hysteria") {
		return "hysteria"
	}
	if strings.Contains(lower, "tuic") {
		return "tuic"
	}
	return "vmess"
}

func convertUnStandandTextViaConvert(url string, data []byte) []map[string]any {
	lines := strings.Split(string(data), "\n")
	var links []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "://") {
			links = append(links, line)
		}
	}

	if len(links) > 0 {
		newData := []byte(strings.Join(links, "\n"))
		proxies, err := convert.ConvertsV2Ray(newData)
		if err == nil && len(proxies) > 0 {
			return proxies
		}
	}
	return nil
}

func convertUnStandandJsonViaConvert(data map[string]any) []map[string]any {
	var links []string
	for _, v := range data {
		if arr, ok := v.([]string); ok {
			links = append(links, arr...)
		} else if arr, ok := v.([]any); ok {
			for _, item := range arr {
				if s, ok := item.(string); ok {
					links = append(links, s)
				}
			}
		}
	}

	if len(links) > 0 {
		newData := []byte(strings.Join(links, "\n"))
		proxies, err := convert.ConvertsV2Ray(newData)
		if err == nil && len(proxies) > 0 {
			return proxies
		}
	}
	return nil
}

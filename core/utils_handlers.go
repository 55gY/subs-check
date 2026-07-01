package core

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/metacubex/mihomo/common/convert"
	"gopkg.in/yaml.v3"
)

// ReadLastNLines 读取文件最后N行
func ReadLastNLines(filePath string, n int) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	ring := make([]string, n)
	count := 0

	// 使用环形缓冲区读取
	for scanner.Scan() {
		ring[count%n] = scanner.Text()
		count++
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// 处理结果
	if count <= n {
		return ring[:count], nil
	}

	// 调整顺序，从最旧到最新
	start := count % n
	result := append(ring[start:], ring[:start]...)
	return result, nil
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseSubscriptionNodes 解析订阅内容为节点列表
func parseSubscriptionNodes(data []byte) ([]map[string]any, error) {
	// 保存原始数据用于日志
	originalData := make([]byte, len(data))
	copy(originalData, data)

	// 尝试 base64 解码，失败就用原数据
	// 注意：去除所有空白字符，支持 URL-safe base64，自动补齐 padding
	trimmedData := strings.ReplaceAll(string(data), " ", "")
	trimmedData = strings.ReplaceAll(trimmedData, "\n", "")
	trimmedData = strings.ReplaceAll(trimmedData, "\r", "")
	trimmedData = strings.ReplaceAll(trimmedData, "\t", "")
	// 将 URL-safe base64 字符替换为标准格式
	trimmedData = strings.ReplaceAll(trimmedData, "-", "+")
	trimmedData = strings.ReplaceAll(trimmedData, "_", "/")
	// 自动补齐 padding
	padLen := len(trimmedData) % 4
	if padLen > 0 {
		trimmedData += strings.Repeat("=", 4-padLen)
	}

	wasDecoded := false
	var decodedData []byte

	if decoded, err := base64.StdEncoding.DecodeString(trimmedData); err == nil {
		// 启发式验证：检查解码结果是否是有效文本
		isValidText := true
		for i := 0; i < len(decoded) && i < 100; i++ {
			c := decoded[i]
			// 允许 Tab(9), LF(10), CR(13)
			if c == 9 || c == 10 || c == 13 {
				continue
			}
			// 其他控制字符和 DEL(127) 认为无效
			if c < 32 || c == 127 {
				isValidText = false
				break
			}
		}

		if isValidText {
			// 简单验证：解码后的内容应该包含常见协议或 proxies 关键字
			decodedStr := string(decoded)
			if strings.Contains(decodedStr, "://") || strings.Contains(decodedStr, "proxies:") {
				decodedData = decoded
				data = decoded
				wasDecoded = true
			}
		}
	}

	var proxies []map[string]any

	// 尝试 YAML 格式
	var con map[string]any
	err := yaml.Unmarshal(data, &con)
	if err == nil {
		// 验证必须包含 proxies 或 proxy-providers
		if !containsKey(con, "proxies") && !containsKey(con, "proxy-providers") {
			// 不是标准 Clash 配置，继续其他解析方式
		} else {
			// YAML 格式成功解析
			proxyInterface, ok := con["proxies"]
			if ok && proxyInterface != nil {
				proxyList, ok := proxyInterface.([]any)
				if ok {
					for _, p := range proxyList {
						if proxyMap, ok := p.(map[string]any); ok {
							proxyMap["skip-cert-verify"] = true
							if proxyMap["servername"] == nil {
								if sni, ok := proxyMap["sni"].(string); ok && strings.TrimSpace(sni) != "" {
									proxyMap["servername"] = sni
								}
							}
							// 为 WS 节点注入 client-fingerprint，提高握手成功率
							if proxyMap["network"] == "ws" && proxyMap["client-fingerprint"] == nil {
								proxyMap["client-fingerprint"] = "chrome"
							}
							// 不再注入 fingerprint 字段，避免与证书指纹验证冲突
							if wsOpts, ok := proxyMap["ws-opts"].(map[string]any); ok {
								if headers, ok := wsOpts["headers"].(map[string]any); ok {
									if host, ok := headers["Host"].(string); ok && strings.TrimSpace(host) != "" && proxyMap["servername"] == nil {
										proxyMap["servername"] = host
									}
								}
							}
							proxies = append(proxies, proxyMap)
						}
					}
					if len(proxies) > 0 {
						return proxies, nil
					}
				}
			}
		}
	}

	// 先尝试逐行提取链接（更可靠的方式）
	lines := strings.Split(string(data), "\n")
	var links []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "vmess://") ||
			strings.HasPrefix(line, "vless://") ||
			strings.HasPrefix(line, "ss://") ||
			strings.HasPrefix(line, "ssr://") ||
			strings.HasPrefix(line, "trojan://") ||
			strings.HasPrefix(line, "hysteria://") ||
			strings.HasPrefix(line, "hysteria2://") ||
			strings.HasPrefix(line, "tuic://") ||
			strings.HasPrefix(line, "anytls://") {
			links = append(links, line)
		}
	}

	// 如果找到了链接，用换行符连接后解析
	if len(links) > 0 {
		extractedData := []byte(strings.Join(links, "\n"))
		proxyList, err := convert.ConvertsV2Ray(extractedData)
		if err == nil && len(proxyList) > 0 {
			// 在提取的节点上强行注入跳过证书校验和sni等信息，防止被 CDN 或握手规则拦截
			for _, p := range proxyList {
				p["skip-cert-verify"] = true
				if p["servername"] == nil {
					if sni, ok := p["sni"].(string); ok && strings.TrimSpace(sni) != "" {
						p["servername"] = sni
					}
				}
				// 为 WS 节点注入 client-fingerprint，提高握手成功率
				if p["network"] == "ws" && p["client-fingerprint"] == nil {
					p["client-fingerprint"] = "chrome"
				}
				// 不再注入 fingerprint 字段，避免与证书指纹验证冲突
				if wsOpts, ok := p["ws-opts"].(map[string]any); ok {
					if headers, ok := wsOpts["headers"].(map[string]any); ok {
						if host, ok := headers["Host"].(string); ok && strings.TrimSpace(host) != "" && p["servername"] == nil {
							p["servername"] = host
						}
					}
				}
			}
			return proxyList, nil
		}
	}

	// 作为后备方案，直接尝试 V2Ray 链接格式（可能是单行或其他格式）
	proxyList, err := convert.ConvertsV2Ray(data)
	if err == nil && len(proxyList) > 0 {
		// 为 WS 节点注入 skip-cert-verify 和 client-fingerprint，提高握手成功率
		for _, p := range proxyList {
			p["skip-cert-verify"] = true
			if p["network"] == "ws" && p["client-fingerprint"] == nil {
				p["client-fingerprint"] = "chrome"
			}
			// 不再注入 fingerprint 字段，避免与证书指纹验证冲突
		}
		return proxyList, nil
	}

	// 解析失败，清理旧日志并将数据写入日志文件以便调试
	cleanupOldParseLogs()

	logFile := filepath.Join(os.TempDir(), fmt.Sprintf("parse_error_%d.log", time.Now().Unix()))
	if f, err := os.Create(logFile); err == nil {
		defer f.Close()
		f.WriteString(fmt.Sprintf("=== 解析失败时间: %s ===\n", time.Now().Format("2006-01-02 15:04:05")))

		// 字符编码信息
		f.WriteString("\n=== 字符编码检测 ===\n")
		if len(originalData) >= 3 && originalData[0] == 0xEF && originalData[1] == 0xBB && originalData[2] == 0xBF {
			f.WriteString("检测到 UTF-8 BOM: 是\n")
		} else {
			f.WriteString("检测到 UTF-8 BOM: 否\n")
		}
		f.WriteString(fmt.Sprintf("数据有效性: UTF-8=%v\n", utf8.Valid(originalData)))

		// 原始数据
		f.WriteString("\n=== 原始数据 ===\n")
		f.WriteString(fmt.Sprintf("长度: %d 字节\n", len(originalData)))
		f.WriteString(fmt.Sprintf("内容（前1000字节）:\n%s\n", string(originalData[:min(1000, len(originalData))])))

		// Base64 解码信息
		f.WriteString("\n=== Base64 解码尝试 ===\n")
		if wasDecoded {
			f.WriteString("解码结果: 成功\n")
			f.WriteString(fmt.Sprintf("解码后长度: %d 字节\n", len(decodedData)))
			f.WriteString(fmt.Sprintf("解码后内容（前1000字节）:\n%s\n", string(decodedData[:min(1000, len(decodedData))])))
		} else {
			f.WriteString("解码结果: 未解码或解码失败\n")
		}

		// YAML 解析详情
		f.WriteString("\n=== YAML 解析详情 ===\n")
		var con map[string]any
		if err := yaml.Unmarshal(data, &con); err == nil {
			f.WriteString("YAML 语法: 有效\n")
			f.WriteString(fmt.Sprintf("包含 'proxies' 字段: %v\n", containsKey(con, "proxies")))
			f.WriteString(fmt.Sprintf("包含 'proxy-providers' 字段: %v\n", containsKey(con, "proxy-providers")))

			if proxyInterface, ok := con["proxies"]; ok && proxyInterface != nil {
				if proxyList, ok := proxyInterface.([]any); ok {
					f.WriteString(fmt.Sprintf("proxies 数组长度: %d\n", len(proxyList)))
					if len(proxyList) > 0 {
						f.WriteString(fmt.Sprintf("第一个元素类型: %T\n", proxyList[0]))
						if pm, ok := proxyList[0].(map[string]any); ok {
							f.WriteString("第一个元素的前 5 个字段:\n")
							count := 0
							for k, v := range pm {
								if count >= 5 {
									break
								}
								f.WriteString(fmt.Sprintf("  %s: %v (类型: %T)\n", k, v, v))
								count++
							}
						}
					}
				} else {
					f.WriteString(fmt.Sprintf("proxies 类型错误: %T（期望 []any）\n", proxyInterface))
				}
			}
		} else {
			f.WriteString(fmt.Sprintf("YAML 语法: 无效 - %v\n", err))
		}

		// 前 20 个节点名称（检查乱码）
		if len(proxies) > 0 {
			f.WriteString("\n=== 前 20 个节点名称 ===\n")
			for i := 0; i < min(20, len(proxies)); i++ {
				if name, ok := proxies[i]["name"]; ok {
					f.WriteString(fmt.Sprintf("%d: %v\n", i+1, name))
				}
			}
		}

		// 提取的链接信息
		f.WriteString("\n=== 最终处理的数据 ===\n")
		f.WriteString(fmt.Sprintf("长度: %d 字节\n", len(data)))
		f.WriteString(fmt.Sprintf("提取的链接数: %d\n", len(links)))

		if len(links) > 0 {
			f.WriteString("\n=== 提取的链接（前10个）===\n")
			for i, link := range links[:min(10, len(links))] {
				f.WriteString(fmt.Sprintf("%d: %s\n", i+1, link))
			}
		}
	}

	return nil, fmt.Errorf("无法解析订阅内容")
}

// containsKey 检查 map 中是否包含指定的 key
func containsKey(m map[string]any, key string) bool {
	_, ok := m[key]
	return ok
}

// cleanupOldParseLogs 清理超过 2 天的 parse_error 日志文件
func cleanupOldParseLogs() {
	pattern := filepath.Join(os.TempDir(), "parse_error_*.log")
	files, err := filepath.Glob(pattern)
	if err != nil {
		slog.Debug("查找 parse_error 日志文件失败", "error", err)
		return
	}

	now := time.Now()
	twoDaysAgo := now.AddDate(0, 0, -2).Truncate(24 * time.Hour)

	for _, file := range files {
		// 从文件名提取时间戳：parse_error_<timestamp>.log
		basename := filepath.Base(file)
		if !strings.HasPrefix(basename, "parse_error_") || !strings.HasSuffix(basename, ".log") {
			continue
		}

		timestampStr := strings.TrimPrefix(basename, "parse_error_")
		timestampStr = strings.TrimSuffix(timestampStr, ".log")

		timestamp, err := strconv.ParseInt(timestampStr, 10, 64)
		if err != nil {
			slog.Debug("解析日志文件时间戳失败", "file", file, "error", err)
			continue
		}

		fileTime := time.Unix(timestamp, 0)
		fileDateOnly := fileTime.Truncate(24 * time.Hour)

		// 删除超过 2 天的日志（保留今天和昨天）
		if fileDateOnly.Before(twoDaysAgo) {
			if err := os.Remove(file); err != nil {
				slog.Debug("删除旧日志文件失败", "file", file, "error", err)
			} else {
				slog.Debug("删除旧日志文件", "file", file, "date", fileDateOnly.Format("2006-01-02"))
			}
		}
	}
}
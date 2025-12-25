package util

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// ToString JSON序列化后转Base64
func ToString(v any) string {
	d, _ := json.Marshal(v)
	return base64.StdEncoding.EncodeToString(d)
}

// ParseV2ray 将代理转换为V2Ray格式链接
func ParseV2ray(proxy any) (string, error) {
	if p, ok := proxy.(map[string]any); ok {
		vmess := make(map[string]any)
		vmess["ps"] = p["name"]
		vmess["add"] = p["server"]
		vmess["port"] = p["port"]
		vmess["id"] = p["uuid"]
		vmess["aid"] = p["alterId"]
		vmess["net"] = p["network"]
		vmess["type"] = "none"
		vmess["host"] = ""
		vmess["path"] = ""
		vmess["tls"] = ""

		if wsOpts, ok := p["ws-opts"].(map[string]any); ok {
			if path, ok := wsOpts["path"].(string); ok {
				vmess["path"] = path
			}
			if headers, ok := wsOpts["headers"].(map[string]any); ok {
				if host, ok := headers["Host"].(string); ok {
					vmess["host"] = host
				}
			}
		}

		if tls, ok := p["tls"].(bool); ok && tls {
			vmess["tls"] = "tls"
		}

		return fmt.Sprintf("vmess://%s", ToString(vmess)), nil
	}

	return "", fmt.Errorf("unsupported proxy type: %T", proxy)
}

// ToBase64 将代理转换为Base64格式（未完成实现）
func ToBase64(name string, proxy map[string]any) string {
	proxyType := proxy["type"].(string)
	var b string

	if proxyType == "vmess" {
		// TODO: 实现vmess转Base64
	} else if proxyType == "ss" {
		// TODO: 实现ss转Base64
	} else if proxyType == "trojan" {
		// TODO: 实现trojan转Base64
	} else {
		return ""
	}

	return b
}

// ParseProxy 从YAML数据中解析代理列表
func ParseProxy(data []byte) ([]map[string]any, error) {
	var config map[string]any

	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	proxies, ok := config["proxies"].([]any)
	if !ok {
		return nil, fmt.Errorf("proxies not found in yaml")
	}

	var result []map[string]any
	for _, p := range proxies {
		if proxy, ok := p.(map[string]any); ok {
			result = append(result, proxy)
		}
	}

	return result, nil
}

// ToClash 将代理列表转换为Clash YAML格式
func ToClash(proxies []map[string]any) ([]byte, error) {
	config := make(map[string]any)
	config["proxies"] = proxies

	return yaml.Marshal(config)
}

// GetProxiesFromSub 从订阅文本中提取代理链接
func GetProxiesFromSub(sub string) ([]string, error) {
	lines := strings.Split(sub, "\n")
	var proxies []string
	for _, line := range lines {
		if strings.HasPrefix(line, "vmess://") || strings.HasPrefix(line, "ss://") || strings.HasPrefix(line, "trojan://") {
			proxies = append(proxies, line)
		}
	}
	return proxies, nil
}

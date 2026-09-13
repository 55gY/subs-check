package provider

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var shadowrocketVMessRE = regexp.MustCompile(`^([^:]+?):([^@]+)@(.+):(\d+)$`)

// NormalizeV2RayLinks 在调用 Mihomo 转换器前兼容客户端特有的 vmess 格式。
// 其他协议保持原样，使后端继续作为协议解析的唯一归属。
func NormalizeV2RayLinks(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(trimmed), "vmess://") {
			continue
		}
		if normalized, err := normalizeShadowrocketVMess(trimmed); err == nil {
			lines[i] = normalized
		}
	}
	return []byte(strings.Join(lines, "\n"))
}

func normalizeShadowrocketVMess(link string) (string, error) {
	raw := strings.TrimSpace(link)[len("vmess://"):]
	base, query, _ := strings.Cut(raw, "?")
	decoded, err := decodeVMessBase64(base)
	if err != nil {
		return link, err
	}
	var payload map[string]any
	if json.Unmarshal([]byte(decoded), &payload) == nil {
		return link, nil
	}
	match := shadowrocketVMessRE.FindStringSubmatch(decoded)
	if len(match) != 5 {
		return link, fmt.Errorf("Shadowrocket vmess 内容格式无效")
	}
	port, err := strconv.Atoi(match[4])
	if err != nil || port < 1 || port > 65535 {
		return link, fmt.Errorf("vmess 端口无效")
	}
	params, _ := url.ParseQuery(query)
	name := params.Get("ps")
	if name == "" {
		name = params.Get("remarks")
	}
	if name == "" {
		name = match[3]
	}
	payload = map[string]any{
		"v": "2", "ps": name, "add": match[3], "port": port,
		"id": match[2], "aid": 0, "scy": match[1],
	}
	for _, key := range []string{"net", "type", "host", "path", "tls", "sni", "alpn"} {
		if value := params.Get(key); value != "" {
			payload[key] = value
		}
	}
	if payload["host"] == nil {
		if value := params.Get("obfsParam"); value != "" {
			payload["host"] = value
		}
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return link, err
	}
	return "vmess://" + base64.StdEncoding.EncodeToString(canonical), nil
}

func decodeVMessBase64(value string) (string, error) {
	value = strings.NewReplacer("-", "+", "_", "/").Replace(value)
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		decoded, err := encoding.DecodeString(value)
		if err == nil && len(decoded) > 0 {
			return string(decoded), nil
		}
	}
	return "", fmt.Errorf("vmess Base64 内容无效")
}

package provider

import (
	"github.com/metacubex/mihomo/common/convert"
)

// ParseSingleNode 解析单个节点链接（vmess/ss/trojan等）
func ParseSingleNode(nodeLink string) ([]map[string]any, error) {
	// 使用mihomo的convert包解析
	proxies, err := convert.ConvertsV2Ray([]byte(nodeLink))
	if err != nil {
		return nil, err
	}
	// 为 WS 节点注入 skip-cert-verify 和 client-fingerprint，提高握手成功率
	for _, p := range proxies {
		p["skip-cert-verify"] = true
		if p["network"] == "ws" && p["client-fingerprint"] == nil {
			p["client-fingerprint"] = "chrome"
		}
		// 不再注入 fingerprint 字段，避免与证书指纹验证冲突
	}
	return proxies, nil
}

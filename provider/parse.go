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
	return proxies, nil
}

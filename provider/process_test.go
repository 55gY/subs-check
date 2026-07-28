package provider

import (
	"fmt"
	"testing"
)

// oldDedupKey 复刻修复前的去重键，用于对比证明旧逻辑的“过度去重”问题。
func oldDedupKey(proxy map[string]any) string {
	server, _ := proxy["server"].(string)
	servername, _ := proxy["servername"].(string)
	password, _ := proxy["password"].(string)
	if password == "" {
		password, _ = proxy["uuid"].(string)
	}
	return fmt.Sprintf("%s:%v:%s:%s", server, proxy["port"], servername, password)
}

// 同 server:port，但协议与凭证均不同，应视为 4 个不同节点。
// 旧逻辑下 hysteria/wireguard/snell 的凭证字段不是 password/uuid（且 servername 为空），
// 会被碰撞合并成 1 个，vless 因有 uuid 幸免，最终 4 个节点被过度去重成 2 个；
// 这正是大量节点被过度去重（如 41 万→2.4 万）的核心原因。
func TestDeduplicate_DifferentProtocolsNotMerged(t *testing.T) {
	proxies := []map[string]any{
		{"type": "hysteria", "server": "1.1.1.1", "port": 443, "auth": "A"},
		{"type": "wireguard", "server": "1.1.1.1", "port": 443, "private-key": "B"},
		{"type": "snell", "server": "1.1.1.1", "port": 443, "psk": "C"},
		{"type": "vless", "server": "1.1.1.1", "port": 443, "uuid": "D"},
	}

	// 前置断言：旧 key 把 hysteria/wireguard/snell 碰撞合并，4 个不同节点被去重成 2 个
	oldSeen := map[string]bool{}
	for _, p := range proxies {
		oldSeen[oldDedupKey(p)] = true
	}
	if len(oldSeen) != 2 {
		t.Fatalf("前置断言失败：旧 key 预期碰撞为 2，实际 %d", len(oldSeen))
	}

	// 新逻辑应保留全部 4 个
	if got := DeduplicateProxies(proxies); len(got) != 4 {
		t.Fatalf("新逻辑应保留 4 个不同节点，实际 %d", len(got))
	}
}

// 完全相同的节点应被去重为 1 个。
func TestDeduplicate_TrueDuplicatesMerged(t *testing.T) {
	proxies := []map[string]any{
		{"type": "vless", "server": "1.1.1.1", "port": 443, "uuid": "same"},
		{"type": "vless", "server": "1.1.1.1", "port": 443, "uuid": "same"},
		{"type": "vless", "server": "1.1.1.1", "port": 443, "uuid": "same"},
	}
	if got := DeduplicateProxies(proxies); len(got) != 1 {
		t.Fatalf("完全相同的节点应去重为 1，实际 %d", len(got))
	}
}

// 同 server:port 同协议但不同密码，应保留为 2 个。
func TestDeduplicate_SameServerDifferentPasswordKept(t *testing.T) {
	proxies := []map[string]any{
		{"type": "ss", "server": "1.1.1.1", "port": 8388, "cipher": "aes-256-gcm", "password": "p1"},
		{"type": "ss", "server": "1.1.1.1", "port": 8388, "cipher": "aes-256-gcm", "password": "p2"},
	}
	if got := DeduplicateProxies(proxies); len(got) != 2 {
		t.Fatalf("同服务器不同密码应保留 2 个，实际 %d", len(got))
	}
}

// server 为空的节点应被跳过。
func TestDeduplicate_EmptyServerSkipped(t *testing.T) {
	proxies := []map[string]any{
		{"type": "vless", "server": "", "port": 443, "uuid": "x"},
		{"type": "vless", "server": "2.2.2.2", "port": 443, "uuid": "y"},
	}
	if got := DeduplicateProxies(proxies); len(got) != 1 {
		t.Fatalf("空 server 应跳过，保留 1 个，实际 %d", len(got))
	}
}

// hysteria 的 password 与 auth 是同一凭证的别名，同凭证不同字段名应视为同一节点。
// 这验证统一后的 ProxyKey 对别名字段取“第一个非空值”，比旧 proxyExists 的 || 更准确。
func TestDeduplicate_AliasCredentialSameNode(t *testing.T) {
	proxies := []map[string]any{
		{"type": "hysteria", "server": "1.1.1.1", "port": 443, "password": "SECRET"},
		{"type": "hysteria", "server": "1.1.1.1", "port": 443, "auth": "SECRET"},
	}
	if got := DeduplicateProxies(proxies); len(got) != 1 {
		t.Fatalf("同凭证不同字段名应视为同一节点（去重为 1），实际 %d", len(got))
	}
}

// ProxyKey 应保证同一节点在两处去重（拉取批量 / 入库增量）得到一致的键。
func TestProxyKey_Stable(t *testing.T) {
	p := map[string]any{"type": "vmess", "server": "1.1.1.1", "port": 443, "uuid": "u", "alterId": 0}
	if ProxyKey(p) != ProxyKey(p) {
		t.Fatal("ProxyKey 对同一节点应返回稳定一致的键")
	}
}

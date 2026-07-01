package core

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/55gY/subs-check/checker"
	"github.com/55gY/subs-check/config"
	"github.com/55gY/subs-check/output"
	"github.com/juju/ratelimit"
)

// TestResult 节点检测结果
type TestResult struct {
	TestedNodes int    // 总检测节点数
	PassedNodes int    // 通过检测的节点数
	FailedNodes int    // 失败的节点数
	AddedNodes  int    // 实际添加的节点数（排除重复）
	Duration    string // 检测耗时
	Timeout     bool   // 是否超时
	Logs        []string
}

// AddResult 节点直接添加结果（test:false模式）
type AddResult struct {
	TotalNodes     int // 总解析节点数
	AddedNodes     int // 成功添加的节点数
	DuplicateNodes int // 重复跳过的节点数
}

// isProxyDuplicate 判断节点是否重复（更健壮的判断方式）
func isProxyDuplicate(newProxy map[string]any, existingProxies []map[string]any) bool {
	for _, existing := range existingProxies {
		// 1. 基础字段必须匹配
		if existing["type"] != newProxy["type"] {
			continue
		}
		if existing["server"] != newProxy["server"] {
			continue
		}
		if existing["port"] != newProxy["port"] {
			continue
		}

		proxyType, _ := newProxy["type"].(string)

		// 2. 根据不同协议类型检查关键字段
		switch proxyType {
		case "vmess":
			// VMess: server + port + uuid + alterId
			if existing["uuid"] != newProxy["uuid"] {
				continue
			}
			if existing["alterId"] != newProxy["alterId"] {
				continue
			}

		case "vless":
			// VLESS: server + port + uuid + flow
			if existing["uuid"] != newProxy["uuid"] {
				continue
			}

		case "ss", "shadowsocks":
			// Shadowsocks: server + port + cipher + password
			if existing["cipher"] != newProxy["cipher"] {
				continue
			}
			if existing["password"] != newProxy["password"] {
				continue
			}

		case "ssr":
			// ShadowsocksR: server + port + cipher + password + protocol + obfs
			if existing["cipher"] != newProxy["cipher"] {
				continue
			}
			if existing["password"] != newProxy["password"] {
				continue
			}
			if existing["protocol"] != newProxy["protocol"] {
				continue
			}
			if existing["obfs"] != newProxy["obfs"] {
				continue
			}

		case "trojan":
			// Trojan: server + port + password + sni
			if existing["password"] != newProxy["password"] {
				continue
			}
			sni1, _ := existing["sni"].(string)
			sni2, _ := newProxy["sni"].(string)
			if sni1 != sni2 {
				continue
			}

		case "hysteria", "hysteria2", "hy2":
			// Hysteria: server + port + password/auth
			pass1 := existing["password"]
			pass2 := newProxy["password"]
			auth1 := existing["auth"]
			auth2 := newProxy["auth"]

			// password 和 auth 可能是不同字段名
			if pass1 != pass2 && auth1 != auth2 {
				continue
			}

		case "tuic":
			// TUIC: server + port + uuid + password
			if existing["uuid"] != newProxy["uuid"] {
				continue
			}
			if existing["password"] != newProxy["password"] {
				continue
			}

		case "anytls":
			// AnyTLS: server + port + password
			if existing["password"] != newProxy["password"] {
				continue
			}

		case "mieru":
			// Mieru: server + port + username + password
			if existing["username"] != newProxy["username"] {
				continue
			}
			if existing["password"] != newProxy["password"] {
				continue
			}

		case "sudoku":
			// Sudoku: server + port + key
			if existing["key"] != newProxy["key"] {
				continue
			}

		case "wireguard", "wg":
			// WireGuard: server + port + private-key + public-key
			privateKey1 := existing["private-key"]
			privateKey2 := newProxy["private-key"]
			if privateKey1 != privateKey2 {
				continue
			}
			publicKey1 := existing["public-key"]
			publicKey2 := newProxy["public-key"]
			if publicKey1 != publicKey2 {
				continue
			}

		case "ssh":
			// SSH: server + port + username + password/private-key
			if existing["username"] != newProxy["username"] {
				continue
			}
			// 检查 password 或 private-key
			pass1 := existing["password"]
			pass2 := newProxy["password"]
			key1 := existing["private-key"]
			key2 := newProxy["private-key"]
			if pass1 != pass2 && key1 != key2 {
				continue
			}

		case "snell":
			// Snell: server + port + psk
			if existing["psk"] != newProxy["psk"] {
				continue
			}

		case "http", "socks", "socks5", "socks4":
			// HTTP/SOCKS: server + port + username (如果有)
			user1, hasUser1 := existing["username"].(string)
			user2, hasUser2 := newProxy["username"].(string)
			if hasUser1 && hasUser2 && user1 != user2 {
				continue
			}
			// 如果都没有 username，仅依据 server+port 判断

		default:
			// 其他协议：比较 server + port + name
			if existing["name"] != newProxy["name"] {
				continue
			}
		}

		// 所有关键字段都匹配，判定为重复
		return true
	}

	return false
}

// addMultipleNodesDirectly 批量添加多个节点到数据库（不进行网络检测）
func (app *App) addMultipleNodesDirectly(proxies []map[string]any) AddResult {
	result := AddResult{
		TotalNodes: len(proxies),
	}

	if len(proxies) == 0 {
		return result
	}

	db, err := output.GetDB()
	if err != nil {
		slog.Warn("打开数据库失败", "error", err)
		return result
	}

	// TestStage 赋值逻辑与 persistResults/addSingleNodeFromProxy 保持一致
	testStage := output.TestAlive
	if config.GlobalConfig.MediaCheck {
		testStage = output.TestMedia
	} else if strings.TrimSpace(config.GlobalConfig.SpeedTestUrl) != "" {
		testStage = output.TestSpeed
	}

	records := make([]output.DBNodeRecord, 0, len(proxies))
	for _, proxy := range proxies {
		records = append(records, output.DBNodeRecord{
			Batch:     output.BatchCurrent,
			TestStage: testStage,
			Proxy:     proxy,
		})
	}

	added, duplicates, err := db.InsertRecordsDedup(records)
	if err != nil {
		slog.Warn("写入节点到数据库失败", "error", err)
		return result
	}
	result.AddedNodes = added
	result.DuplicateNodes = duplicates
	return result
}

// testAndAddNodes 统一的节点检测和添加函数
// 并发检测所有节点，测速通过的添加到数据库
// 设置 120 秒总超时，超时后返回已完成的部分结果
func (app *App) testAndAddNodes(proxies []map[string]any, logEnabled bool) TestResult {
	startTime := time.Now()
	result := TestResult{
		TestedNodes: len(proxies),
	}

	if len(proxies) == 0 {
		result.Duration = "0s"
		return result
	}

	// 初始化 Bucket（如果未初始化）
	if checker.Bucket == nil {
		if config.GlobalConfig.TotalSpeedLimit > 0 {
			checker.Bucket = ratelimit.NewBucketWithRate(
				float64(config.GlobalConfig.TotalSpeedLimit)*1024,
				int64(config.GlobalConfig.TotalSpeedLimit)*1024,
			)
		} else {
			// 不限速
			checker.Bucket = ratelimit.NewBucketWithRate(1e9, 1e9)
		}
	}

	// 创建 120 秒超时的 context
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// 并发控制
	concurrent := config.GlobalConfig.GetAliveConcurrent()
	if concurrent <= 0 {
		concurrent = 5
	}
	semaphore := make(chan struct{}, concurrent)
	// 统计变量
	var passedCount, failedCount, addedCount atomic.Int32
	var wg sync.WaitGroup
	var logsMu sync.Mutex
	var logs []string
	appendLog := func(msg string) {
		if !logEnabled {
			return
		}
		logsMu.Lock()
		logs = append(logs, msg)
		logsMu.Unlock()
	}

	// 并发检测所有节点
	for _, proxy := range proxies {
		// 检查是否超时
		select {
		case <-ctx.Done():
			result.Timeout = true
			goto finish
		default:
		}

		wg.Add(1)
		semaphore <- struct{}{} // 获取信号量

		go func(proxyMap map[string]any) {
			defer wg.Done()
			defer func() { <-semaphore }() // 释放信号量

			// 检查是否已超时
			select {
			case <-ctx.Done():
				failedCount.Add(1)
				return
			default:
			}

			// 创建代理客户端
			client := checker.CreateClient(proxyMap)
			if client == nil {
				failedCount.Add(1)
				if logEnabled {
					appendLog(fmt.Sprintf("节点 %v: 创建客户端失败, type=%v, server=%v, port=%v", proxyMap["name"], proxyMap["type"], proxyMap["server"], proxyMap["port"]))
				}
				return
			}
			defer client.Close()

			// 检查是否超时
			select {
			case <-ctx.Done():
				failedCount.Add(1)
				return
			default:
			}

			// 存活检测
			var alive bool
			var err error
			if config.GlobalConfig.UnifiedDelay {
				alive, _, err = checker.CheckAliveWithWarmup(ctx, client.Client)
			} else {
				alive, err = checker.CheckAlive(ctx, client.Client)
			}
			if err != nil || !alive {
				failedCount.Add(1)
				if logEnabled {
					appendLog(fmt.Sprintf("节点 %v: 存活检测失败, alive=%v, unified_delay=%v, timeout=%d, warmup_timeout=%d, test_timeout=%d, error=%v", proxyMap["name"], alive, config.GlobalConfig.UnifiedDelay, config.GlobalConfig.Timeout, config.GlobalConfig.WarmupTimeout, config.GlobalConfig.TestTimeout, err))
				}
				return
			}

			// 检查是否超时
			select {
			case <-ctx.Done():
				failedCount.Add(1)
				return
			default:
			}

			// 速度测试
			if config.GlobalConfig.SpeedTestUrl != "" {
				_, err := checker.CheckSpeed(ctx, client.Client, client.BytesRead)
				if err != nil {
					failedCount.Add(1)
					if logEnabled {
						appendLog(fmt.Sprintf("节点 %v: 测速失败, speed_test_url=%s, download_timeout=%d, error=%v", proxyMap["name"], config.GlobalConfig.SpeedTestUrl, config.GlobalConfig.DownloadTimeout, err))
					}
					return
				}
			}

			// 测速通过，添加到数据库
			passedCount.Add(1)

			err = app.addSingleNodeFromProxy(proxyMap)
			if err == nil {
				addedCount.Add(1)
			}
			// 注意：即使添加失败（如重复），也不影响 passedCount
		}(proxy)
	}

finish:
	// 等待所有协程完成或超时
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// 所有任务完成
	case <-ctx.Done():
		// 超时，等待已启动的协程完成
		result.Timeout = true
		wg.Wait()
	}

	result.PassedNodes = int(passedCount.Load())
	result.FailedNodes = int(failedCount.Load())
	result.AddedNodes = int(addedCount.Load())
	result.Duration = time.Since(startTime).Round(time.Millisecond).String()
	if logEnabled {
		if len(logs) == 0 {
			logs = append(logs, fmt.Sprintf("节点检测未记录到详细失败原因, unified_delay=%v, timeout=%d, warmup_timeout=%d, test_timeout=%d, speed_test_url=%q", config.GlobalConfig.UnifiedDelay, config.GlobalConfig.Timeout, config.GlobalConfig.WarmupTimeout, config.GlobalConfig.TestTimeout, config.GlobalConfig.SpeedTestUrl))
		}
		result.Logs = logs
	}

	return result
}

// addSingleNodeFromProxy 从 proxy map 添加单个节点到数据库（内部使用）
func (app *App) addSingleNodeFromProxy(proxy map[string]any) error {
	db, err := output.GetDB()
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}

	// TestStage 赋值逻辑与 persistResults 保持一致：基于 pipeline 配置
	testStage := output.TestAlive
	if config.GlobalConfig.MediaCheck {
		testStage = output.TestMedia
	} else if strings.TrimSpace(config.GlobalConfig.SpeedTestUrl) != "" {
		testStage = output.TestSpeed
	}

	added, duplicates, err := db.InsertRecordsDedup([]output.DBNodeRecord{{
		Batch:     output.BatchCurrent,
		TestStage: testStage,
		Proxy:     proxy,
	}})
	if err != nil {
		return err
	}
	if added == 0 && duplicates > 0 {
		return fmt.Errorf("节点已存在")
	}
	return nil
}
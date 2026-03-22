package provider

import (
	"fmt"
	"math/rand"
	"net"
	"sync"
	"time"
)

// ==================== 去重功能 (from dedup.go) ====================

func DeduplicateProxies(proxies []map[string]any) []map[string]any {
	seenKeys := make(map[string]bool)
	result := make([]map[string]any, 0, len(proxies))

	for _, proxy := range proxies {
		server, _ := proxy["server"].(string)
		if server == "" {
			continue
		}
		servername, _ := proxy["servername"].(string)

		password, _ := proxy["password"].(string)
		if password == "" {
			password, _ = proxy["uuid"].(string)
		}

		key := fmt.Sprintf("%s:%v:%s:%s", server, proxy["port"], servername, password)
		if !seenKeys[key] {
			seenKeys[key] = true
			result = append(result, proxy)
		}
	}

	return result
}

// ==================== 重命名功能 (from rename.go) ====================

var (
	counter     = make(map[string]int)
	counterLock = sync.Mutex{}
)

// Rename 根据国家代码生成节点名称
// 格式: CountryCode
func Rename(countryCode string, fraudScore int) string {
	return countryCode
}

// GetFraudScoreLabel 根据 fraudScore 返回纯净度中文描述
func GetFraudScoreLabel(score int) string {
	if score >= 0 && score <= 10 {
		return "极佳"
	} else if score >= 11 && score <= 30 {
		return "优秀"
	} else if score >= 31 && score <= 50 {
		return "良好"
	} else if score >= 51 && score <= 70 {
		return "中等"
	} else if score >= 71 && score <= 90 {
		return "差"
	} else if score > 90 {
		return "极差"
	}
	return ""
}

// ResetRenameCounter 将所有计数器重置为 0（保留向后兼容）
func ResetRenameCounter() {
	counterLock.Lock()
	defer counterLock.Unlock()

	counter = make(map[string]int)
}

func CountryCodeToFlag(code string) string {
	if len(code) != 2 {
		return "❓Other"
	}

	code = string([]rune(code)[0]&^0x20) + string([]rune(code)[1]&^0x20) // 转成大写（ASCII 位运算）

	r1 := rune(code[0]-'A') + 0x1F1E6
	r2 := rune(code[1]-'A') + 0x1F1E6

	return string([]rune{r1, r2})
}

// ==================== 洗牌功能 (from shuffle.go) ====================

type ShuffleConfig struct {
	Threshold  float64    // 相邻相似度阈值，IPv4 /24 ≈ 0.75
	Passes     int        // 改善轮数（1~3）
	MinSpacing int        // 同一 IPv4 /24 的最小间距；<=0 关闭
	ScanLimit  int        // 冲突向前扫描的最大距离
	Rand       *rand.Rand // 随机数，为空则使用 time.Now().UnixNano()
}

type serverMeta struct {
	raw      string
	isIPv4   bool
	octets   [4]byte
	prefix24 uint32
	prefixOK bool
}

// SmartShuffleByServer 对 items 就地打乱，避免相邻相似，并尽量满足最小间距
func SmartShuffleByServer(items []map[string]any, cfg ShuffleConfig) {
	n := len(items)
	if n < 2 {
		return
	}

	// 默认参数
	if cfg.Passes <= 0 {
		cfg.Passes = 2
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = 0.75
	}
	if cfg.ScanLimit <= 0 {
		cfg.ScanLimit = 64
	}
	rnd := cfg.Rand
	if rnd == nil {
		rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	// 先进行一次完全随机乱序
	rnd.Shuffle(n, func(i, j int) {
		items[i], items[j] = items[j], items[i]
	})

	// 预解析服务器元数据
	metas := make([]serverMeta, n)
	for i := range items {
		if s, _ := items[i]["server"].(string); s != "" {
			metas[i] = parseServerMeta(s)
		}
	}

	// 初次打乱
	rnd.Shuffle(n, func(i, j int) {
		swap(items, metas, i, j)
	})

	// 检查最小间距
	checkSpacing := func(lp map[uint32]int, idx int, m serverMeta) bool {
		if cfg.MinSpacing <= 0 || !m.prefixOK {
			return true
		}
		if last, ok := lp[m.prefix24]; !ok || idx-last > cfg.MinSpacing {
			return true
		}
		return false
	}

	for pass := 0; pass < cfg.Passes; pass++ {
		changed := false
		lastPos := make(map[uint32]int, 64)

		if metas[0].prefixOK {
			lastPos[metas[0].prefix24] = 0
		}

		for i := 0; i < n-1; i++ {
			m1, m2 := metas[i], metas[i+1]
			if m1.prefixOK {
				if _, ok := lastPos[m1.prefix24]; !ok {
					lastPos[m1.prefix24] = i
				}
			}

			if similarity(m1, m2) >= cfg.Threshold || (cfg.MinSpacing > 0 && same24(m1, m2)) {
				bestJ, bestScore := -1, 2.0
				for j := i + 2; j < n && j < i+2+cfg.ScanLimit; j++ {
					mj := metas[j]
					if !checkSpacing(lastPos, i+1, mj) {
						continue
					}
					score := similarity(m1, mj)
					if score < bestScore {
						bestScore = score
						bestJ = j
					}
				}

				if bestJ != -1 {
					swap(items, metas, i+1, bestJ)
					changed = true
					if metas[i+1].prefixOK {
						lastPos[metas[i+1].prefix24] = i + 1
					}
				}
			} else {
				if m2.prefixOK {
					lastPos[m2.prefix24] = i + 1
				}
			}
		}
		if !changed {
			break
		}
	}
}

func swap(items []map[string]any, metas []serverMeta, i, j int) {
	items[i], items[j] = items[j], items[i]
	metas[i], metas[j] = metas[j], metas[i]
}

func parseServerMeta(s string) serverMeta {
	m := serverMeta{raw: s}
	ip := net.ParseIP(s)
	if ip != nil {
		if v4 := ip.To4(); v4 != nil {
			m.isIPv4 = true
			copy(m.octets[:], v4)
			m.prefix24 = uint32(v4[0])<<16 | uint32(v4[1])<<8 | uint32(v4[2])
			m.prefixOK = true
		}
	}
	return m
}

func similarity(a, b serverMeta) float64 {
	if a.raw == b.raw {
		return 1.0
	}
	if a.isIPv4 && b.isIPv4 {
		match := 0
		for i := 0; i < 4; i++ {
			if a.octets[i] == b.octets[i] {
				match++
			} else {
				break
			}
		}
		return float64(match) / 4.0
	}
	return 0.0
}

func same24(a, b serverMeta) bool {
	return a.prefixOK && b.prefixOK && a.prefix24 == b.prefix24
}

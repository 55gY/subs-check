package proxies

import (
	"strconv"
	"sync"
)

var (
	counter     = make(map[string]int)
	counterLock = sync.Mutex{}
)

// Rename 根据国家代码和fraudScore生成节点名称
// 格式: CountryCode_纯净度描述 (例如: HK_中等)
func Rename(countryCode string, fraudScore int) string {
	qualityLabel := GetFraudScoreLabel(fraudScore)
	if qualityLabel != "" {
		return countryCode + "_" + qualityLabel
	}
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

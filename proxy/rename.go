package proxies

import (
	"strconv"
	"sync"
)

var (
	counter     = make(map[string]int)
	counterLock = sync.Mutex{}
)

func Rename(name string) string {
	counterLock.Lock()
	defer counterLock.Unlock()

	counter[name]++
	return CountryCodeToFlag(name) + name + "_" + strconv.Itoa(counter[name])

}

// ResetRenameCounter 将所有计数器重置为 0
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

	// 国家代码到emoji的映射
	flagMap := map[string]string{
		"CN": "🇨🇳", "US": "🇺🇸", "JP": "🇯🇵", "KR": "🇰🇷", "HK": "🇭🇰", "TW": "🇹🇼", 
		"SG": "🇸🇬", "GB": "🇬🇧", "DE": "🇩🇪", "FR": "🇫🇷", "CA": "🇨🇦", "AU": "🇦🇺", 
		"RU": "🇷🇺", "BR": "🇧🇷", "IN": "🇮🇳", "IT": "🇮🇹", "ES": "🇪🇸", "NL": "🇳🇱", 
		"CH": "🇨🇭", "SE": "🇸🇪", "NO": "🇳🇴", "DK": "🇩🇰", "FI": "🇫🇮", "PL": "🇵🇱", 
		"CZ": "🇨🇿", "AT": "🇦🇹", "BE": "🇧🇪", "IE": "🇮🇪", "PT": "🇵🇹", "GR": "🇬🇷", 
		"TR": "🇹🇷", "IL": "🇮🇱", "SA": "🇸🇦", "AE": "🇦🇪", "TH": "🇹🇭", "VN": "🇻🇳", 
		"MY": "🇲🇾", "ID": "🇮🇩", "PH": "🇵🇭", "AR": "🇦🇷", "CL": "🇨🇱", "CO": "🇨🇴", 
		"PE": "🇵🇪", "MX": "🇲🇽", "ZA": "🇿🇦", "EG": "🇪🇬", "NG": "🇳🇬", "KE": "🇰🇪", 
		"MA": "🇲🇦", "TN": "🇹🇳", "DZ": "🇩🇿", "UA": "🇺🇦", "KZ": "🇰🇿", "UZ": "🇺🇿",
	}

	if flag, ok := flagMap[code]; ok {
		return flag
	}

	// 如果没有找到对应的emoji，则使用原来的Unicode国旗
	r1 := rune(code[0]-'A') + 0x1F1E6
	r2 := rune(code[1]-'A') + 0x1F1E6
	return string([]rune{r1, r2})
}

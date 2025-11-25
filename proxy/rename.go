package proxies

import (
	"strconv"
	"strings"
	"sync"
)

var (
	counter     = make(map[string]int)
	counterLock = sync.Mutex{}
)

func Rename(name, countryCodeTag string) string {

	flag := CountryCodeToFlag(name)

	key, label := name, name
	// 如果节点名称包含CN，则丢弃该节点
	if strings.Contains(strings.ToUpper(name), "CN") {
		return ""
	}

	counterLock.Lock()
	defer counterLock.Unlock()

	counter[key]++
	n := counter[key]

	return flag + label + "_" + strconv.Itoa(n)
}

// ResetRenameCounter 将所有计数器重置为 0
func ResetRenameCounter() {
	counterLock.Lock()
	defer counterLock.Unlock()

	counter = make(map[string]int)
}

func CountryCodeToFlag(code string) string {
	if len(code) != 2 {
		return "🏴‍☠"
	}

	code = string([]rune(code)[0]&^0x20) + string([]rune(code)[1]&^0x20) // 转成大写（ASCII 位运算）

	r1 := rune(code[0]-'A') + 0x1F1E6
	r2 := rune(code[1]-'A') + 0x1F1E6

	return string([]rune{r1, r2})
}

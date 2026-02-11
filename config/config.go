package config

import (
	"bufio"
	_ "embed"
	"fmt"
	"os"
	"strings"
)

// ConcurrentConfig 分阶段并发配置
type ConcurrentConfig struct {
	Alive int `yaml:"alive"` // 存活检测并发数
	Speed int `yaml:"speed"` // 测速检测并发数
	Media int `yaml:"media"` // 媒体检测并发数
}

type Config struct {
	PrintProgress        bool              `yaml:"print-progress"`
	Concurrent           int               `yaml:"concurrent"`           // 旧版配置，保持向后兼容
	ConcurrentStage      *ConcurrentConfig `yaml:"concurrent-stage"`     // 新版分阶段配置
	CheckInterval        int               `yaml:"check-interval"`
	CronExpression       string            `yaml:"cron-expression"`
	AliveTestUrl         string            `yaml:"alive-test-url"`
	SpeedTestUrl         string            `yaml:"speed-test-url"`
	DownloadTimeout      int               `yaml:"download-timeout"`
	DownloadMB           int               `yaml:"download-mb"`
	TotalSpeedLimit      int               `yaml:"total-speed-limit"`
	MinSpeed             int               `yaml:"min-speed"`
	Timeout              int               `yaml:"timeout"`
	UnifiedDelay         bool              `yaml:"unified-delay"`          // 是否启用统一延迟（两次测试）
	WarmupTimeout        int               `yaml:"warmup-timeout"`         // 预热阶段超时（秒）
	TestTimeout          int               `yaml:"test-timeout"`           // 实际测试超时（秒）
	SubUrlsReTry         int               `yaml:"sub-urls-retry"`
	SubUrlsRetryInterval int               `yaml:"sub-urls-retry-interval"`
	SubUrlsTimeout       int               `yaml:"sub-urls-timeout"`
	SubUrlsProxy         []string          `yaml:"sub-urls-proxy"`
	SubUrlsGetUA         string            `yaml:"sub-urls-get-ua"`
	SubUrlsRemote        []string          `yaml:"sub-urls-remote"`
	SubUrls              []string          `yaml:"sub-urls"`
	ListenPort           string            `yaml:"listen-port"`
	RenameNode           bool              `yaml:"rename-node"`
	OutputDir            string            `yaml:"output-dir"`
	AppriseApiServer     string            `yaml:"apprise-api-server"`
	RecipientUrl         []string          `yaml:"recipient-url"`
	NotifyTitle          string            `yaml:"notify-title"`
	MediaCheck           bool              `yaml:"media-check"`
	Platforms            []string          `yaml:"platforms"`
	NodePrefix           string            `yaml:"node-prefix"`
	NodeType             []string          `yaml:"node-type"`
	Filters              []string          `yaml:"filters"`
	EnableWebUI          bool              `yaml:"enable-web-ui"`
	APIKey               string            `yaml:"api-key"`
	GithubProxy          string            `yaml:"github-proxy"`
	Proxy                string            `yaml:"proxy"`
	SubUrlsRetryFailed   int               `yaml:"sub-urls-retry-failed"`
	SubUrlsMinNodeCount  int               `yaml:"sub-urls-min-node-count"`
}

var GlobalConfig = &Config{
	// 新增配置，给未更改配置文件的用户一个默认值
	ListenPort:  ":8199",
	NotifyTitle: "🔔 节点状态更新",
	Platforms:   []string{"openai", "youtube", "netflix", "disney", "gemini"},
	DownloadMB:  20,
	AliveTestUrl: "http://gstatic.com/generate_204",
	SubUrlsGetUA: "clash",
	SubUrlsRetryFailed:  -1, // 默认永不删除，避免误删用户订阅
	SubUrlsMinNodeCount: 0,  // 默认不启用节点数量检查
	Filters:             []string{"CN"},
	// 统一延迟配置默认值
	UnifiedDelay:  false, // 默认关闭，保持向后兼容
	WarmupTimeout: 15,    // 预热超时 15 秒
	TestTimeout:   10,    // 实际测试超时 10 秒
}

// GetAliveConcurrent 获取存活检测并发数
func (c *Config) GetAliveConcurrent() int {
	if c.ConcurrentStage != nil && c.ConcurrentStage.Alive > 0 {
		return c.ConcurrentStage.Alive
	}
	// 向后兼容：使用旧配置
	if c.Concurrent > 0 {
		return c.Concurrent
	}
	return 100 // 默认值
}

// GetSpeedConcurrent 获取测速检测并发数
func (c *Config) GetSpeedConcurrent() int {
	if c.ConcurrentStage != nil && c.ConcurrentStage.Speed > 0 {
		return c.ConcurrentStage.Speed
	}
	// 向后兼容：使用旧配置的 40%
	if c.Concurrent > 0 {
		conc := int(float64(c.Concurrent) * 0.4)
		if conc < 1 {
			conc = 1
		}
		return conc
	}
	return 40 // 默认值
}

// GetMediaConcurrent 获取媒体检测并发数
func (c *Config) GetMediaConcurrent() int {
	if c.ConcurrentStage != nil && c.ConcurrentStage.Media > 0 {
		return c.ConcurrentStage.Media
	}
	// 向后兼容：使用旧配置的 20%
	if c.Concurrent > 0 {
		conc := int(float64(c.Concurrent) * 0.2)
		if conc < 1 {
			conc = 1
		}
		return conc
	}
	return 20 // 默认值
}

//go:embed config.example.yaml
var DefaultConfigTemplate []byte

// GlobalConfigPath 全局配置文件路径
var GlobalConfigPath string

// DeduplicateSubUrls 去除配置文件中sub-urls的重复项（保留注释和格式）
func DeduplicateSubUrls(configPath string) error {
	// 读取配置文件
	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("打开配置文件失败: %w", err)
	}
	defer file.Close()

	var newLines []string
	scanner := bufio.NewScanner(file)
	inSubUrls := false
	seenUrls := make(map[string]bool) // 记录已经出现的URL

	for scanner.Scan() {
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		// 检测 sub-urls 部分
		if !inSubUrls && (trimmedLine == "sub-urls:" || trimmedLine == "sub-urls: []") {
			inSubUrls = true
			newLines = append(newLines, line)
			continue
		}

		shouldSkip := false
		if inSubUrls {
			// 检测缩进
			if len(line) > 0 && line[0] == ' ' {
				// 找到 sub-urls 下的项
				for i, ch := range line {
					if ch == '-' {
						// 提取URL部分（去掉 "- " 和前后空格）
						urlPart := strings.TrimSpace(line[i+1:])
						// 去掉可能的引号
						urlPart = strings.Trim(urlPart, `"'`)
						
						// 检查是否重复
						if seenUrls[urlPart] {
							// 重复的URL，跳过这一行
							shouldSkip = true
						} else {
							// 第一次出现，记录它
							seenUrls[urlPart] = true
						}
						break
					}
				}
			} else if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
				// 遇到新的顶级配置项，sub-urls 部分结束
				inSubUrls = false
			}
		}

		// 只有不需要跳过的行才添加
		if !shouldSkip {
			newLines = append(newLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 写入更新后的配置
	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("保存配置文件失败: %w", err)
	}

	return nil
}

// DeduplicateSubUrlsFromConfig 去除全局配置文件中sub-urls的重复项
func DeduplicateSubUrlsFromConfig() error {
	if GlobalConfigPath == "" {
		return fmt.Errorf("配置文件路径未设置")
	}
	return DeduplicateSubUrls(GlobalConfigPath)
}

// RemoveSubUrlFromConfig 从配置文件中删除指定的订阅链接（保留注释和格式）
func RemoveSubUrlFromConfig(subUrl string) error {
	if GlobalConfigPath == "" {
		return fmt.Errorf("配置文件路径未设置")
	}
	return RemoveSubUrl(GlobalConfigPath, subUrl)
}

// RemoveSubUrl 从配置文件中删除指定的订阅链接（保留注释和格式）
func RemoveSubUrl(configPath, subUrl string) error {
	// 读取配置文件
	file, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("打开配置文件失败: %w", err)
	}
	defer file.Close()

	var newLines []string
	scanner := bufio.NewScanner(file)
	inSubUrls := false

	for scanner.Scan() {
		line := scanner.Text()
		trimmedLine := strings.TrimSpace(line)

		// 检测 sub-urls 部分
		if !inSubUrls && (trimmedLine == "sub-urls:" || trimmedLine == "sub-urls: []") {
			inSubUrls = true
			newLines = append(newLines, line)
			continue
		}

		shouldSkip := false
		if inSubUrls {
			// 检测缩进
			if len(line) > 0 && line[0] == ' ' {
				// 找到 sub-urls 下的项
				for i, ch := range line {
					if ch == '-' {
						// 提取URL部分（去掉 "- " 和前后空格）
						urlPart := strings.TrimSpace(line[i+1:])
						// 去掉可能的引号
						urlPart = strings.Trim(urlPart, `"'`)
						// 如果这行包含要删除的URL，标记跳过这一行
						if urlPart == subUrl {
							shouldSkip = true
						}
						break
					}
				}
			} else if len(line) > 0 && line[0] != ' ' && line[0] != '#' {
				// 遇到新的顶级配置项，sub-urls 部分结束
				inSubUrls = false
			}
		}

		// 只有不需要跳过的行才添加
		if !shouldSkip {
			newLines = append(newLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("读取配置文件失败: %w", err)
	}

	// 写入更新后的配置
	newContent := strings.Join(newLines, "\n")
	if err := os.WriteFile(configPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("保存配置文件失败: %w", err)
	}

	return nil
}

// FailureRecord 失败记录结构
type FailureRecord struct {
	URL     string
	Count   int
	Updated string
}

// GetFailureRecordPath 获取失败记录文件路径
func GetFailureRecordPath() string {
	return "sub_failure_record.txt"
}

// ReadFailureRecord 读取失败记录
func ReadFailureRecord() (map[string]int, error) {
	records := make(map[string]int)
	filePath := GetFailureRecordPath()

	file, err := os.Open(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return records, nil // 文件不存在时返回空记录
		}
		return records, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.Split(line, "|")
		if len(parts) >= 2 {
			url := strings.TrimSpace(parts[0])
			var count int
			if _, err := fmt.Sscanf(parts[1], "%d", &count); err == nil {
				records[url] = count
			}
		}
	}

	return records, scanner.Err()
}

// WriteFailureRecord 写入失败记录
func WriteFailureRecord(records map[string]int) error {
	filePath := GetFailureRecordPath()

	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// 写入文件头注释
	fmt.Fprintln(file, "# 订阅链接失败记录")
	fmt.Fprintln(file, "# 格式: URL|失败次数|最后更新时间")
	fmt.Fprintln(file, "# 当失败次数达到配置的重试次数时，该订阅链接将被自动删除")
	fmt.Fprintln(file, "")

	// 写入记录
	for url, count := range records {
		fmt.Fprintf(file, "%s|%d|%s\n", url, count, "")
	}

	return nil
}

// IncrementFailureCount 增加失败次数
func IncrementFailureCount(subUrl string) (int, error) {
	records, err := ReadFailureRecord()
	if err != nil {
		return 0, err
	}

	// 增加失败次数
	records[subUrl]++
	currentCount := records[subUrl]

	// 保存更新后的记录
	err = WriteFailureRecord(records)
	if err != nil {
		return currentCount, err
	}

	return currentCount, nil
}

// ResetFailureCount 重置失败次数（成功获取时调用）
func ResetFailureCount(subUrl string) error {
	records, err := ReadFailureRecord()
	if err != nil {
		return err
	}

	// 如果该URL存在失败记录，删除它
	if _, exists := records[subUrl]; exists {
		delete(records, subUrl)
		return WriteFailureRecord(records)
	}

	return nil
}

// ShouldRemoveFailedSub 检查是否应该删除失败的订阅
func ShouldRemoveFailedSub(subUrl string, failureCount int) bool {
	retryConfig := GlobalConfig.SubUrlsRetryFailed

	// < 0: 永不删除
	if retryConfig < 0 {
		return false
	}

	// = 0: 失败1次就删除
	if retryConfig == 0 {
		return failureCount >= 1
	}

	// > 0: 失败N次后删除
	return failureCount >= retryConfig
}

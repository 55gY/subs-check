package config

import (
	_ "embed"
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"strings"
)

type Config struct {
	PrintProgress        bool     `yaml:"print-progress"`
	Concurrent           int      `yaml:"concurrent"`
	CheckInterval        int      `yaml:"check-interval"`
	CronExpression       string   `yaml:"cron-expression"`
	AliveTestUrl         string   `yaml:"alive-test-url"`
	SpeedTestUrl         string   `yaml:"speed-test-url"`
	DownloadTimeout      int      `yaml:"download-timeout"`
	DownloadMB           int      `yaml:"download-mb"`
	TotalSpeedLimit      int      `yaml:"total-speed-limit"`
	MinSpeed             int      `yaml:"min-speed"`
	Timeout              int      `yaml:"timeout"`
	FilterRegex          string   `yaml:"filter-regex"`
	SaveMethod           string   `yaml:"save-method"`
	WebDAVURL            string   `yaml:"webdav-url"`
	WebDAVUsername       string   `yaml:"webdav-username"`
	WebDAVPassword       string   `yaml:"webdav-password"`
	GithubToken          string   `yaml:"github-token"`
	GithubGistID         string   `yaml:"github-gist-id"`
	GithubAPIMirror      string   `yaml:"github-api-mirror"`
	WorkerURL            string   `yaml:"worker-url"`
	WorkerToken          string   `yaml:"worker-token"`
	S3Endpoint           string   `yaml:"s3-endpoint"`
	S3AccessID           string   `yaml:"s3-access-id"`
	S3SecretKey          string   `yaml:"s3-secret-key"`
	S3Bucket             string   `yaml:"s3-bucket"`
	S3UseSSL             bool     `yaml:"s3-use-ssl"`
	S3BucketLookup       string   `yaml:"s3-bucket-lookup"`
	SubUrlsReTry         int      `yaml:"sub-urls-retry"`
	SubUrlsRetryInterval int      `yaml:"sub-urls-retry-interval"`
	SubUrlsTimeout       int      `yaml:"sub-urls-timeout"`
	SubUrlsGetUA         string   `yaml:"sub-urls-get-ua"`
	SubUrlsRemote        []string `yaml:"sub-urls-remote"`
	SubUrls              []string `yaml:"sub-urls"`
	SuccessRate          float32  `yaml:"success-rate"`
	MihomoApiUrl         string   `yaml:"mihomo-api-url"`
	MihomoApiSecret      string   `yaml:"mihomo-api-secret"`
	ListenPort           string   `yaml:"listen-port"`
	RenameNode           bool     `yaml:"rename-node"`
	KeepSuccessProxies   bool     `yaml:"keep-success-proxies"`
	OutputDir            string   `yaml:"output-dir"`
	AppriseApiServer     string   `yaml:"apprise-api-server"`
	RecipientUrl         []string `yaml:"recipient-url"`
	NotifyTitle          string   `yaml:"notify-title"`
	SubStorePort         string   `yaml:"sub-store-port"`
	SubStorePath         string   `yaml:"sub-store-path"`
	SubStoreSyncCron     string   `yaml:"sub-store-sync-cron"`
	SubStorePushService  string   `yaml:"sub-store-push-service"`
	SubStoreProduceCron  string   `yaml:"sub-store-produce-cron"`
	MihomoOverwriteUrl   string   `yaml:"mihomo-overwrite-url"`
	MediaCheck           bool     `yaml:"media-check"`
	Platforms            []string `yaml:"platforms"`
	SuccessLimit         int32    `yaml:"success-limit"`
	NodePrefix           string   `yaml:"node-prefix"`
	NodeType             []string `yaml:"node-type"`
	EnableWebUI          bool     `yaml:"enable-web-ui"`
	APIKey               string   `yaml:"api-key"`
	GithubProxy          string   `yaml:"github-proxy"`
	Proxy                string   `yaml:"proxy"`
	CallbackScript       string   `yaml:"callback-script"`
	RemoveFailedSub      bool     `yaml:"remove-failed-sub"`
	RemoveFailedSubRetry int      `yaml:"remove-failed-sub-retry"`
}

var GlobalConfig = &Config{
	// 新增配置，给未更改配置文件的用户一个默认值
	ListenPort:           ":8199",
	NotifyTitle:          "🔔 节点状态更新",
	MihomoOverwriteUrl:   "http://127.0.0.1:8199/sub/ACL4SSR_Online_Full.yaml",
	Platforms:            []string{"openai", "youtube", "netflix", "disney", "gemini", "iprisk"},
	DownloadMB:           20,
	AliveTestUrl:         "http://gstatic.com/generate_204",
	SubUrlsGetUA:         "clash.meta (https://github.com/beck-8/subs-check)",
	RemoveFailedSubRetry: 3, // 默认失败3次后删除
}

//go:embed config.example.yaml
var DefaultConfigTemplate []byte

var GlobalProxies []map[string]any

// GlobalConfigPath 全局配置文件路径
var GlobalConfigPath string

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
						// 如果这行包含要删除的URL，标记跳过这一行
						if urlPart == subUrl {
							slog.Info("从配置文件中删除失败的订阅链接", "url", subUrl)
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
	
	slog.Info("记录订阅链接失败", "url", subUrl, "失败次数", currentCount, "删除阈值", GlobalConfig.RemoveFailedSubRetry)
	
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
		slog.Info("重置订阅链接失败计数", "url", subUrl)
		return WriteFailureRecord(records)
	}
	
	return nil
}

// ShouldRemoveFailedSub 检查是否应该删除失败的订阅
func ShouldRemoveFailedSub(subUrl string, failureCount int) bool {
	if !GlobalConfig.RemoveFailedSub {
		return false
	}
	
	if GlobalConfig.RemoveFailedSubRetry <= 0 {
		return true // 如果设置为0或负数，第一次失败就删除
	}
	
	return failureCount >= GlobalConfig.RemoveFailedSubRetry
}

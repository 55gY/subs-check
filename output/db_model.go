package output

import "time"

const (
	BatchCurrent  = 0
	BatchPrevious = -1

	TestAlive = 0
	TestSpeed = 1
	TestMedia = 2
)

const (
	dbFileName      = "subs.db"
	nodesBucketName = "nodes"
	authBucketName  = "auth"
)

type DBNodeRecord struct {
	ID        uint64         `json:"id"`
	Batch     int            `json:"batch"`
	TestStage int            `json:"testStage"`
	SpeedKBps int            `json:"speedKBps"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Proxy     map[string]any `json:"proxy"`

	Openai    bool   `json:"openai"`
	OpenaiWeb bool   `json:"openaiWeb"`
	Youtube   string `json:"youtube"`
	Netflix   bool   `json:"netflix"`
	Google    bool   `json:"google"`
	Disney    bool   `json:"disney"`
	Gemini    bool   `json:"gemini"`
	TikTok    string `json:"tikTok"`
	IP        string `json:"ip"`
	IPRisk    string `json:"ipRisk"`
	Country   string `json:"country"`
}

type AuthFailureRecord struct {
	Key         string    `json:"key"`         // IP 地址
	FailCount   int       `json:"failCount"`   // 连续鉴权失败次数
	LastScope   string    `json:"lastScope"`   // 最后触发的 scope（api 或 sub）
	LastFailAt  time.Time `json:"lastFailAt"`  // 最近一次鉴权失败时间
	BannedUntil time.Time `json:"bannedUntil"` // 封禁截止时间（零值表示未封禁）
	UpdatedAt   time.Time `json:"updatedAt"`   // 记录最后更新时间
}

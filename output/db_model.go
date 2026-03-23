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

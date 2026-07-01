package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"

	bbolt "github.com/metacubex/bbolt"
)

type DBNodeRecord struct {
	ID        uint64         `json:"id"`
	Batch     int            `json:"batch"`
	TestStage int            `json:"testStage"`
	SpeedKBps int            `json:"speedKBps"`
	Proxy     map[string]any `json:"proxy"`
}

func main() {
	dbPath := "output/subs.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}

	db, err := bbolt.Open(dbPath, 0644, &bbolt.Options{ReadOnly: true})
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer db.Close()

	var records []DBNodeRecord
	err = db.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte("nodes"))
		if bucket == nil {
			return fmt.Errorf("nodes bucket 不存在")
		}
		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			var record DBNodeRecord
			if err := json.Unmarshal(value, &record); err != nil {
				continue
			}
			records = append(records, record)
		}
		return nil
	})
	if err != nil {
		log.Fatalf("查询失败: %v", err)
	}

	fmt.Printf("总记录数: %d\n\n", len(records))

	// 按速度降序排列
	sort.Slice(records, func(i, j int) bool {
		return records[i].SpeedKBps > records[j].SpeedKBps
	})

	// 统计
	speedZero := 0
	speedNonZero := 0
	for _, r := range records {
		if r.SpeedKBps == 0 {
			speedZero++
		} else {
			speedNonZero++
		}
	}
	fmt.Printf("速度为0的节点: %d\n", speedZero)
	fmt.Printf("速度非0的节点: %d\n\n", speedNonZero)

	// 显示前20个非0速度节点的名称和速度
	fmt.Println("=== 速度非0的前20个节点 ===")
	count := 0
	for _, r := range records {
		if r.SpeedKBps > 0 {
			name := ""
			if r.Proxy != nil {
				if n, ok := r.Proxy["name"].(string); ok {
					name = n
				}
			}
			fmt.Printf("ID=%d SpeedKBps=%d TestStage=%d Name=%s\n", r.ID, r.SpeedKBps, r.TestStage, name)
			count++
			if count >= 20 {
				break
			}
		}
	}

	// 显示前10个速度为0但TestStage>=1的节点
	fmt.Println("\n=== 速度为0但经过测速的节点(前10个) ===")
	count = 0
	for _, r := range records {
		if r.SpeedKBps == 0 && r.TestStage >= 1 {
			name := ""
			if r.Proxy != nil {
				if n, ok := r.Proxy["name"].(string); ok {
					name = n
				}
			}
			fmt.Printf("ID=%d SpeedKBps=%d TestStage=%d Name=%s\n", r.ID, r.SpeedKBps, r.TestStage, name)
			count++
			if count >= 10 {
				break
			}
		}
	}

	// 显示所有节点的名称（包含速率信息的关键字）
	fmt.Println("\n=== 节点名称中包含速率信息的记录(前20个) ===")
	count = 0
	for _, r := range records {
		if r.Proxy != nil {
			if name, ok := r.Proxy["name"].(string); ok {
				// 检查名称中是否包含 MB/s 或 KB/s
				if len(name) > 0 {
					fmt.Printf("ID=%d SpeedKBps=%d TestStage=%d Batch=%d Name=%s\n", r.ID, r.SpeedKBps, r.TestStage, r.Batch, name)
					count++
					if count >= 20 {
						break
					}
				}
			}
		}
	}
}
package output

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"sync"
	"time"

	bbolt "github.com/metacubex/bbolt"
)

type DB struct {
	conn *bbolt.DB
}

var (
	dbInstance *DB
	dbOnce     sync.Once
	dbInitErr  error
)

func GetDB() (*DB, error) {
	dbOnce.Do(func() {
		dbInstance, dbInitErr = OpenDB()
	})
	if dbInitErr != nil {
		return nil, dbInitErr
	}
	return dbInstance, nil
}

func OpenDB() (*DB, error) {
	saver, err := NewLocalSaver()
	if err != nil {
		return nil, fmt.Errorf("创建本地保存器失败: %w", err)
	}

	if err := saver.ensureOutputDir(); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}

	dbPath := filepath.Join(saver.OutputPath, dbFileName)
	conn, err := bbolt.Open(dbPath, fileMode, nil)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.Init(); err != nil {
		_ = conn.Close()
		return nil, err
	}

	return db, nil
}

func (db *DB) Init() error {
	return db.conn.Update(func(tx *bbolt.Tx) error {
		if _, err := tx.CreateBucketIfNotExists([]byte(nodesBucketName)); err != nil {
			return fmt.Errorf("初始化 nodes bucket 失败: %w", err)
		}
		if _, err := tx.CreateBucketIfNotExists([]byte(authBucketName)); err != nil {
			return fmt.Errorf("初始化 auth bucket 失败: %w", err)
		}
		return nil
	})
}

func (db *DB) Close() error {
	if db == nil || db.conn == nil {
		return nil
	}
	return db.conn.Close()
}

func (db *DB) InsertRecord(record DBNodeRecord) (uint64, error) {
	if db == nil || db.conn == nil {
		return 0, fmt.Errorf("数据库未初始化")
	}

	record.UpdatedAt = time.Now()
	if record.Batch != BatchCurrent && record.Batch != BatchPrevious {
		record.Batch = BatchCurrent
	}

	var id uint64
	err := db.conn.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(nodesBucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s 不存在", nodesBucketName)
		}

		nextID, err := bucket.NextSequence()
		if err != nil {
			return fmt.Errorf("生成记录ID失败: %w", err)
		}

		record.ID = nextID
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("序列化记录失败: %w", err)
		}

		if err := bucket.Put(itob(nextID), data); err != nil {
			return fmt.Errorf("写入记录失败: %w", err)
		}

		id = nextID
		return nil
	})
	if err != nil {
		return 0, err
	}

	return id, nil
}

func (db *DB) InsertRecordsDedup(records []DBNodeRecord) (int, int, error) {
	if db == nil || db.conn == nil {
		return 0, 0, fmt.Errorf("数据库未初始化")
	}
	if len(records) == 0 {
		return 0, 0, nil
	}

	now := time.Now()
	added := 0
	duplicates := 0
	err := db.conn.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(nodesBucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s 不存在", nodesBucketName)
		}

		existing, err := loadProxyList(bucket)
		if err != nil {
			return err
		}
		pending := make([]map[string]any, 0, len(records))

		for _, record := range records {
			if record.Proxy == nil {
				continue
			}
			if proxyExists(record.Proxy, existing) || proxyExists(record.Proxy, pending) {
				duplicates++
				continue
			}

			if record.Batch != BatchCurrent && record.Batch != BatchPrevious {
				record.Batch = BatchCurrent
			}
			record.UpdatedAt = now

			nextID, err := bucket.NextSequence()
			if err != nil {
				return fmt.Errorf("生成记录ID失败: %w", err)
			}
			record.ID = nextID

			data, err := json.Marshal(record)
			if err != nil {
				return fmt.Errorf("序列化记录失败: %w", err)
			}
			if err := bucket.Put(itob(nextID), data); err != nil {
				return fmt.Errorf("写入记录失败: %w", err)
			}

			pending = append(pending, record.Proxy)
			added++
		}
		return nil
	})
	if err != nil {
		return 0, 0, err
	}

	return added, duplicates, nil
}

func (db *DB) ReplaceCurrentBatch(records []DBNodeRecord) error {
	if db == nil || db.conn == nil {
		return fmt.Errorf("数据库未初始化")
	}

	return db.conn.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(nodesBucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s 不存在", nodesBucketName)
		}

		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			var record DBNodeRecord
			if err := json.Unmarshal(value, &record); err != nil {
				return fmt.Errorf("解析现有记录失败: %w", err)
			}
			if record.Batch == BatchCurrent {
				record.Batch = BatchPrevious
				record.UpdatedAt = time.Now()
				data, err := json.Marshal(record)
				if err != nil {
					return fmt.Errorf("序列化上一批记录失败: %w", err)
				}
				if err := bucket.Put(key, data); err != nil {
					return fmt.Errorf("写入上一批记录失败: %w", err)
				}
			}
		}

		for _, record := range records {
			record.Batch = BatchCurrent
			record.UpdatedAt = time.Now()

			nextID, err := bucket.NextSequence()
			if err != nil {
				return fmt.Errorf("生成记录ID失败: %w", err)
			}
			record.ID = nextID

			data, err := json.Marshal(record)
			if err != nil {
				return fmt.Errorf("序列化新批次记录失败: %w", err)
			}
			if err := bucket.Put(itob(nextID), data); err != nil {
				return fmt.Errorf("写入新批次记录失败: %w", err)
			}
		}

		return nil
	})
}

func (db *DB) QueryRecords(testStage int, minSpeed int) ([]DBNodeRecord, error) {
	if db == nil || db.conn == nil {
		return nil, fmt.Errorf("数据库未初始化")
	}

	records, err := db.queryBatch(testStage, minSpeed, BatchCurrent)
	if err != nil {
		return nil, err
	}
	if len(records) > 0 {
		sortRecordsBySpeedDesc(records)
		return records, nil
	}

	records, err = db.queryBatch(testStage, minSpeed, BatchPrevious)
	if err != nil {
		return nil, err
	}
	sortRecordsBySpeedDesc(records)
	return records, nil
}

func (db *DB) IsAuthBlocked(key string, now time.Time) (bool, error) {
	if db == nil || db.conn == nil {
		return false, fmt.Errorf("数据库未初始化")
	}

	blocked := false
	err := db.conn.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(authBucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s 不存在", authBucketName)
		}

		record, found, err := getAuthRecord(bucket, key)
		if err != nil || !found {
			return err
		}
		if record.BannedUntil.After(now) {
			blocked = true
			return nil
		}
		if record.BannedUntil.IsZero() {
			return nil
		}
		return bucket.Delete([]byte(key))
	})
	return blocked, err
}

func (db *DB) RecordAuthFailure(key string, now time.Time, maxFailures int, banDuration time.Duration) error {
	if db == nil || db.conn == nil {
		return fmt.Errorf("数据库未初始化")
	}

	return db.conn.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(authBucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s 不存在", authBucketName)
		}

		record, found, err := getAuthRecord(bucket, key)
		if err != nil {
			return err
		}
		if found && record.BannedUntil.After(now) {
			return nil
		}
		if !found || (!record.BannedUntil.IsZero() && !record.BannedUntil.After(now)) {
			record = AuthFailureRecord{Key: key}
		}

		record.FailCount++
		record.LastFailAt = now
		record.UpdatedAt = now
		if record.FailCount >= maxFailures {
			record.BannedUntil = now.Add(banDuration)
		}

		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("序列化鉴权失败记录失败: %w", err)
		}
		return bucket.Put([]byte(key), data)
	})
}

func (db *DB) ClearAuthFailure(key string) error {
	if db == nil || db.conn == nil {
		return fmt.Errorf("数据库未初始化")
	}

	return db.conn.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(authBucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s 不存在", authBucketName)
		}
		return bucket.Delete([]byte(key))
	})
}

func (db *DB) CleanupAuthFailures(now time.Time) error {
	if db == nil || db.conn == nil {
		return fmt.Errorf("数据库未初始化")
	}

	return db.conn.Update(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(authBucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s 不存在", authBucketName)
		}

		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			var record AuthFailureRecord
			if err := json.Unmarshal(value, &record); err != nil {
				return fmt.Errorf("解析鉴权失败记录失败: %w", err)
			}
			if !record.BannedUntil.IsZero() && !record.BannedUntil.After(now) {
				if err := bucket.Delete(key); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (db *DB) queryBatch(testStage int, minSpeed int, batch int) ([]DBNodeRecord, error) {
	var records []DBNodeRecord
	err := db.conn.View(func(tx *bbolt.Tx) error {
		bucket := tx.Bucket([]byte(nodesBucketName))
		if bucket == nil {
			return fmt.Errorf("bucket %s 不存在", nodesBucketName)
		}

		cursor := bucket.Cursor()
		for key, value := cursor.First(); key != nil; key, value = cursor.Next() {
			var record DBNodeRecord
			if err := json.Unmarshal(value, &record); err != nil {
				return fmt.Errorf("解析记录失败: %w", err)
			}
			if record.Batch != batch {
				continue
			}
			if record.TestStage < testStage {
				continue
			}
			if testStage > TestAlive && record.SpeedKBps < minSpeed {
				continue
			}
			records = append(records, record)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return records, nil
}

func itob(v uint64) []byte {
	b := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}

func sortRecordsBySpeedDesc(records []DBNodeRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].SpeedKBps == records[j].SpeedKBps {
			return records[i].ID < records[j].ID
		}
		return records[i].SpeedKBps > records[j].SpeedKBps
	})
}

func loadProxyList(bucket *bbolt.Bucket) ([]map[string]any, error) {
	proxies := make([]map[string]any, 0)
	cursor := bucket.Cursor()
	for _, value := cursor.First(); value != nil; _, value = cursor.Next() {
		var record DBNodeRecord
		if err := json.Unmarshal(value, &record); err != nil {
			return nil, fmt.Errorf("解析现有记录失败: %w", err)
		}
		if record.Proxy != nil {
			proxies = append(proxies, record.Proxy)
		}
	}
	return proxies, nil
}

func proxyExists(newProxy map[string]any, existingProxies []map[string]any) bool {
	for _, existing := range existingProxies {
		if existing["type"] != newProxy["type"] || existing["server"] != newProxy["server"] || existing["port"] != newProxy["port"] {
			continue
		}

		switch proxyType, _ := newProxy["type"].(string); proxyType {
		case "vmess":
			if existing["uuid"] != newProxy["uuid"] || existing["alterId"] != newProxy["alterId"] {
				continue
			}
		case "vless":
			if existing["uuid"] != newProxy["uuid"] {
				continue
			}
		case "ss", "shadowsocks":
			if existing["cipher"] != newProxy["cipher"] || existing["password"] != newProxy["password"] {
				continue
			}
		case "ssr":
			if existing["cipher"] != newProxy["cipher"] || existing["password"] != newProxy["password"] || existing["protocol"] != newProxy["protocol"] || existing["obfs"] != newProxy["obfs"] {
				continue
			}
		case "trojan":
			sni1, _ := existing["sni"].(string)
			sni2, _ := newProxy["sni"].(string)
			if existing["password"] != newProxy["password"] || sni1 != sni2 {
				continue
			}
		case "hysteria", "hysteria2", "hy2":
			if existing["password"] != newProxy["password"] && existing["auth"] != newProxy["auth"] {
				continue
			}
		case "tuic":
			if existing["uuid"] != newProxy["uuid"] || existing["password"] != newProxy["password"] {
				continue
			}
		case "anytls", "snell":
			if existing["password"] != newProxy["password"] && existing["psk"] != newProxy["psk"] {
				continue
			}
		case "mieru":
			if existing["username"] != newProxy["username"] || existing["password"] != newProxy["password"] {
				continue
			}
		case "sudoku":
			if existing["key"] != newProxy["key"] {
				continue
			}
		case "wireguard", "wg":
			if existing["private-key"] != newProxy["private-key"] || existing["public-key"] != newProxy["public-key"] {
				continue
			}
		case "ssh":
			if existing["username"] != newProxy["username"] || (existing["password"] != newProxy["password"] && existing["private-key"] != newProxy["private-key"]) {
				continue
			}
		case "http", "socks", "socks5", "socks4":
			user1, hasUser1 := existing["username"].(string)
			user2, hasUser2 := newProxy["username"].(string)
			if hasUser1 && hasUser2 && user1 != user2 {
				continue
			}
		default:
			if existing["name"] != newProxy["name"] {
				continue
			}
		}

		return true
	}
	return false
}

func getAuthRecord(bucket *bbolt.Bucket, key string) (AuthFailureRecord, bool, error) {
	value := bucket.Get([]byte(key))
	if value == nil {
		return AuthFailureRecord{}, false, nil
	}

	var record AuthFailureRecord
	if err := json.Unmarshal(value, &record); err != nil {
		return AuthFailureRecord{}, false, fmt.Errorf("解析鉴权失败记录失败: %w", err)
	}
	return record, true, nil
}

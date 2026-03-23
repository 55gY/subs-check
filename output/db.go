package output

import (
	"encoding/json"
	"fmt"
	"path/filepath"
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
			return fmt.Errorf("初始化 bucket 失败: %w", err)
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
		return records, nil
	}

	return db.queryBatch(testStage, minSpeed, BatchPrevious)
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

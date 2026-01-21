package entity

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// JSONData 用于存储 Editor.js 的 Block 数据
type JSONData map[string]interface{}

func (j *JSONData) Scan(value interface{}) error {
	if value == nil {
		*j = make(JSONData)
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONData) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "{}", nil
	}
	return json.Marshal(j)
}

type Block struct {
	ID        string         `gorm:"primaryKey;size:64;column:id"`
	PageID    uint64         `gorm:"not null;index:idx_page_sort;column:page_id"`
	Type      string         `gorm:"size:50;not null;column:type"`
	Data      JSONData       `gorm:"type:json;not null;column:data"`
	SortOrder uint           `gorm:"not null;index:idx_page_sort,priority:2;column:sort_order"`
	CreatedAt time.Time      `gorm:"autoCreateTime;column:created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index;column:deleted_at"`
}

func (Block) TableName() string {
	return "blocks"
}

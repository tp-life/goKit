package entity

import (
	"database/sql/driver"
	"encoding/json"
	"time"

	"gorm.io/gorm"
)

// JSONStringArray 用于存储 JSON 数组字符串
type JSONStringArray []string

func (j *JSONStringArray) Scan(value interface{}) error {
	if value == nil {
		*j = []string{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, j)
}

func (j JSONStringArray) Value() (driver.Value, error) {
	if len(j) == 0 {
		return "[]", nil
	}
	return json.Marshal(j)
}

type Memo struct {
	ID        uint64          `gorm:"primaryKey;column:id"`
	UserID    uint64          `gorm:"not null;index:idx_user_time;column:user_id"`
	Content   string          `gorm:"type:text;not null;column:content"`
	Images    JSONStringArray `gorm:"type:json;column:images"`
	Source    string          `gorm:"size:20;default:'mobile';column:source"`
	CreatedAt time.Time       `gorm:"autoCreateTime;precision:3;index:idx_user_time,priority:2,sort:desc;column:created_at"`
	DeletedAt gorm.DeletedAt  `gorm:"index;column:deleted_at"`
	Tags      []Tag           `gorm:"many2many:memo_tags;"` // 标签关联
}

func (Memo) TableName() string {
	return "memos"
}

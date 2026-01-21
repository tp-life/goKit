package entity

import (
	"time"

	"gorm.io/gorm"
)

type Page struct {
	ID        uint64         `gorm:"primaryKey;column:id"`
	UserID    uint64         `gorm:"not null;column:user_id"`
	Title     string         `gorm:"size:255;column:title"`
	Cover     string         `gorm:"size:255;column:cover"`
	Summary   string         `gorm:"size:500;column:summary"`
	IsShared  bool           `gorm:"default:0;column:is_shared"`
	ShareID   *string        `gorm:"size:64;uniqueIndex;column:share_id"` // 使用指针类型，允许 NULL
	CreatedAt time.Time      `gorm:"autoCreateTime;precision:3;column:created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;precision:3;column:updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index;column:deleted_at"`
	Tags      []Tag          `gorm:"many2many:page_tags;"` // 标签关联
}

func (Page) TableName() string {
	return "pages"
}

package entity

import "time"

type Tag struct {
	ID        uint64    `gorm:"primaryKey;column:id"`
	UserID    uint64    `gorm:"not null;index:idx_user_id;column:user_id"`
	Name      string    `gorm:"size:50;not null;column:name"`
	CreatedAt time.Time `gorm:"autoCreateTime;precision:3;column:created_at"`
}

func (Tag) TableName() string {
	return "tags"
}

package entity

import "time"

type UserSession struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement;column:id"`
	UserID       uint64    `gorm:"not null;index;column:user_id"`
	RefreshToken string    `gorm:"size:255;uniqueIndex;not null;column:refresh_token"`
	DeviceInfo   string    `gorm:"size:255;column:device_info"`
	ExpiresAt    time.Time `gorm:"not null;column:expires_at"`
	CreatedAt    time.Time `gorm:"autoCreateTime;column:created_at"`
}

func (UserSession) TableName() string {
	return "user_sessions"
}

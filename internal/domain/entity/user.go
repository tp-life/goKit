package entity

import "time"

type User struct {
	ID           uint64    `gorm:"primaryKey;autoIncrement;column:id"`
	Email        string    `gorm:"size:128;uniqueIndex;not null;column:email"`
	PasswordHash string    `gorm:"size:255;not null;column:password_hash"`
	Salt         string    `gorm:"size:32;column:salt"`
	CreatedAt    time.Time `gorm:"autoCreateTime;column:created_at"`
}

func (User) TableName() string {
	return "users"
}

package user

import "time"

// User 系统用户
type User struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	Username  string    `gorm:"size:64;uniqueIndex;not null"`
	Password  string    `gorm:"size:128;not null"` // bcrypt 哈希
	Nickname  string    `gorm:"size:64"`
	Email     string    `gorm:"size:128"`
	DeptID    uint64    `gorm:"index"`
	Status    int8      `gorm:"default:1"` // 1 正常 0 停用
	IsSuper   bool      `gorm:"default:false"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (User) TableName() string { return "sys_users" }

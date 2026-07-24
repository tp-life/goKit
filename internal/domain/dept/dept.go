package dept

import "time"

// Dept 部门（树形）
type Dept struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	ParentID  uint64    `gorm:"index;default:0"`
	Name      string    `gorm:"size:64;not null"`
	Sort      int       `gorm:"default:0"`
	Status    int8      `gorm:"default:1"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (Dept) TableName() string { return "sys_depts" }

package menu

import "time"

// 菜单类型
const (
	MenuTypeDir    int8 = 1 // 目录
	MenuTypeMenu   int8 = 2 // 菜单
	MenuTypeButton int8 = 3 // 按钮/权限点
)

// Menu 菜单与权限点（树形）。权限校验只关心 PermCode。
type Menu struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	ParentID  uint64    `gorm:"index;default:0"`
	Title     string    `gorm:"size:64;not null"`
	Type      int8      `gorm:"default:2"`
	Path      string    `gorm:"size:128"`
	PermCode  string    `gorm:"size:128;index"` // 如 system:user:list，仅按钮/权限点需要
	Sort      int       `gorm:"default:0"`
	Status    int8      `gorm:"default:1"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (Menu) TableName() string { return "sys_menus" }

package entity

import "time"

// 数据权限范围
const (
	DataScopeAll             int8 = 1 // 全部数据
	DataScopeCustom          int8 = 2 // 自定义部门
	DataScopeDeptAndChildren int8 = 3 // 本部门及以下
	DataScopeDept            int8 = 4 // 本部门
	DataScopeSelf            int8 = 5 // 仅本人
)

// Role 角色
type Role struct {
	ID        uint64    `gorm:"primaryKey;autoIncrement"`
	Name      string    `gorm:"size:64;not null"`
	Code      string    `gorm:"size:64;uniqueIndex;not null"`
	DataScope int8      `gorm:"default:1"`
	Status    int8      `gorm:"default:1"` // 1 正常 0 停用
	Remark    string    `gorm:"size:255"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (Role) TableName() string { return "sys_roles" }

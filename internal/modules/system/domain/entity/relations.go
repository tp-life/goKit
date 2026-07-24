package entity

// UserRole 用户-角色关联
type UserRole struct {
	UserID uint64 `gorm:"primaryKey"`
	RoleID uint64 `gorm:"primaryKey"`
}

func (UserRole) TableName() string { return "sys_user_roles" }

// RoleMenu 角色-菜单(权限)关联
type RoleMenu struct {
	RoleID uint64 `gorm:"primaryKey"`
	MenuID uint64 `gorm:"primaryKey"`
}

func (RoleMenu) TableName() string { return "sys_role_menus" }

// RoleDept 角色-部门关联（data_scope=自定义 时生效）
type RoleDept struct {
	RoleID uint64 `gorm:"primaryKey"`
	DeptID uint64 `gorm:"primaryKey"`
}

func (RoleDept) TableName() string { return "sys_role_depts" }

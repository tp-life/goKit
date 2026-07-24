package role

import "context"

// RoleRepository 角色仓储
type RoleRepository interface {
	Create(ctx context.Context, role *Role) error
	Update(ctx context.Context, role *Role) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*Role, error)
	List(ctx context.Context, page, pageSize int) ([]Role, int64, error)
	// FindByUserID 查询用户拥有的全部角色
	FindByUserID(ctx context.Context, userID uint64) ([]Role, error)
}

// AssignRepository 三类关联关系仓储
type AssignRepository interface {
	// 用户-角色
	SetUserRoles(ctx context.Context, userID uint64, roleIDs []uint64) error
	GetRoleIDsByUser(ctx context.Context, userID uint64) ([]uint64, error)
	// 角色-菜单
	SetRoleMenus(ctx context.Context, roleID uint64, menuIDs []uint64) error
	GetMenuIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error)
	// 角色-部门（自定义数据范围）
	SetRoleDepts(ctx context.Context, roleID uint64, deptIDs []uint64) error
	GetDeptIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error)
	// 角色-用户
	SetRoleUsers(ctx context.Context, roleID uint64, userIDs []uint64) error
	GetUserIDsByRole(ctx context.Context, roleID uint64) ([]uint64, error)
}

package repository

import (
	"context"

	"goKit/internal/modules/system/domain/entity"
	domainsvc "goKit/internal/modules/system/domain/service"
)

// UserRepository 用户仓储
type UserRepository interface {
	Create(ctx context.Context, user *entity.User) error
	Update(ctx context.Context, user *entity.User) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*entity.User, error)
	FindByUsername(ctx context.Context, username string) (*entity.User, error)
	// List 按数据权限过滤分页查询
	List(ctx context.Context, filter domainsvc.Filter, page, pageSize int) ([]entity.User, int64, error)
	UpdatePassword(ctx context.Context, id uint64, hashedPwd string) error
}

// RoleRepository 角色仓储
type RoleRepository interface {
	Create(ctx context.Context, role *entity.Role) error
	Update(ctx context.Context, role *entity.Role) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*entity.Role, error)
	List(ctx context.Context, page, pageSize int) ([]entity.Role, int64, error)
	// FindByUserID 查询用户拥有的全部角色
	FindByUserID(ctx context.Context, userID uint64) ([]entity.Role, error)
}

// DeptRepository 部门仓储
type DeptRepository interface {
	Create(ctx context.Context, dept *entity.Dept) error
	Update(ctx context.Context, dept *entity.Dept) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*entity.Dept, error)
	List(ctx context.Context) ([]entity.Dept, error)
	// FindDescendantIDs 返回含自身在内的所有子孙部门 ID
	FindDescendantIDs(ctx context.Context, deptID uint64) ([]uint64, error)
}

// MenuRepository 菜单/权限点仓储
type MenuRepository interface {
	Create(ctx context.Context, menu *entity.Menu) error
	Update(ctx context.Context, menu *entity.Menu) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*entity.Menu, error)
	List(ctx context.Context) ([]entity.Menu, error)
	// FindPermCodesByRoleIDs 查询角色集合拥有的全部权限点（去重，空权限码已过滤）
	FindPermCodesByRoleIDs(ctx context.Context, roleIDs []uint64) ([]string, error)
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

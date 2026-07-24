package menu

import "context"

// MenuRepository 菜单/权限点仓储
type MenuRepository interface {
	Create(ctx context.Context, menu *Menu) error
	Update(ctx context.Context, menu *Menu) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*Menu, error)
	List(ctx context.Context) ([]Menu, error)
	// FindPermCodesByRoleIDs 查询角色集合拥有的全部权限点（去重，空权限码已过滤）
	FindPermCodesByRoleIDs(ctx context.Context, roleIDs []uint64) ([]string, error)
}

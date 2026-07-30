package persistence

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"goKit/internal/domain/menu"
	"goKit/pkg/kit/db"
)

type menuRepo struct {
	client *db.Client
}

func NewMenuRepository(client *db.Client) menu.MenuRepository {
	return &menuRepo{client: client}
}

func (r *menuRepo) Create(ctx context.Context, m *menu.Menu) error {
	return r.client.GetDB(ctx).Create(m).Error
}

func (r *menuRepo) Update(ctx context.Context, m *menu.Menu) error {
	return r.client.GetDB(ctx).Model(&menu.Menu{}).Where("id = ?", m.ID).
		Select("parent_id", "title", "type", "path", "perm_code", "sort", "status").Updates(m).Error
}

func (r *menuRepo) Delete(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).Where("id = ?", id).Delete(&menu.Menu{}).Error
}

func (r *menuRepo) FindByID(ctx context.Context, id uint64) (*menu.Menu, error) {
	var m menu.Menu
	err := r.client.GetDB(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &m, err
}

func (r *menuRepo) List(ctx context.Context) ([]menu.Menu, error) {
	var menus []menu.Menu
	err := r.client.GetDB(ctx).Order("sort, id").Find(&menus).Error
	return menus, err
}

func (r *menuRepo) FindIDsByRoleIDs(ctx context.Context, roleIDs []uint64) ([]uint64, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var ids []uint64
	err := r.client.GetDB(ctx).Table("sys_role_menus").
		Distinct("menu_id").
		Where("role_id IN ?", roleIDs).
		Pluck("menu_id", &ids).Error
	return ids, err
}

func (r *menuRepo) FindPermCodesByRoleIDs(ctx context.Context, roleIDs []uint64) ([]string, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var codes []string
	err := r.client.GetDB(ctx).Model(&menu.Menu{}).
		Distinct("sys_menus.perm_code").
		Joins("JOIN sys_role_menus rm ON rm.menu_id = sys_menus.id").
		Where("rm.role_id IN ? AND sys_menus.status = 1 AND sys_menus.perm_code <> ''", roleIDs).
		Pluck("sys_menus.perm_code", &codes).Error
	return codes, err
}

package persistence

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"goKit/internal/modules/system/domain/entity"
	"goKit/internal/modules/system/domain/repository"
	"goKit/pkg/kit/db"
)

type menuRepo struct {
	client *db.Client
}

func NewMenuRepository(client *db.Client) repository.MenuRepository {
	return &menuRepo{client: client}
}

func (r *menuRepo) Create(ctx context.Context, menu *entity.Menu) error {
	return r.client.GetDB(ctx).Create(menu).Error
}

func (r *menuRepo) Update(ctx context.Context, menu *entity.Menu) error {
	return r.client.GetDB(ctx).Model(&entity.Menu{}).Where("id = ?", menu.ID).
		Select("parent_id", "title", "type", "path", "perm_code", "sort", "status").Updates(menu).Error
}

func (r *menuRepo) Delete(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).Where("id = ?", id).Delete(&entity.Menu{}).Error
}

func (r *menuRepo) FindByID(ctx context.Context, id uint64) (*entity.Menu, error) {
	var m entity.Menu
	err := r.client.GetDB(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &m, err
}

func (r *menuRepo) List(ctx context.Context) ([]entity.Menu, error) {
	var menus []entity.Menu
	err := r.client.GetDB(ctx).Order("sort, id").Find(&menus).Error
	return menus, err
}

func (r *menuRepo) FindPermCodesByRoleIDs(ctx context.Context, roleIDs []uint64) ([]string, error) {
	if len(roleIDs) == 0 {
		return nil, nil
	}
	var codes []string
	err := r.client.GetDB(ctx).Model(&entity.Menu{}).
		Distinct("sys_menus.perm_code").
		Joins("JOIN sys_role_menus rm ON rm.menu_id = sys_menus.id").
		Where("rm.role_id IN ? AND sys_menus.status = 1 AND sys_menus.perm_code <> ''", roleIDs).
		Pluck("sys_menus.perm_code", &codes).Error
	return codes, err
}

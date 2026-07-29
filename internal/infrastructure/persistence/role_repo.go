package persistence

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"goKit/internal/domain/role"
	"goKit/pkg/kit/db"
)

type roleRepo struct {
	client *db.Client
}

func NewRoleRepository(client *db.Client) role.RoleRepository {
	return &roleRepo{client: client}
}

func (r *roleRepo) Create(ctx context.Context, ro *role.Role) error {
	return r.client.GetDB(ctx).Create(ro).Error
}

func (r *roleRepo) Update(ctx context.Context, ro *role.Role) error {
	return r.client.GetDB(ctx).Model(&role.Role{}).Where("id = ?", ro.ID).
		Select("name", "code", "data_scope", "status", "remark").Updates(ro).Error
}

func (r *roleRepo) Delete(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).Where("id = ?", id).Delete(&role.Role{}).Error
}

func (r *roleRepo) FindByID(ctx context.Context, id uint64) (*role.Role, error) {
	var role role.Role
	err := r.client.GetDB(ctx).Where("id = ?", id).First(&role).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &role, err
}

func (r *roleRepo) List(ctx context.Context, q role.Query, page, pageSize int) ([]role.Role, int64, error) {
	tx := r.client.GetDB(ctx).Model(&role.Role{})
	if q.Keyword != "" {
		like := "%" + q.Keyword + "%"
		tx = tx.Where("name LIKE ? OR code LIKE ?", like, like)
	}
	if q.Status != nil {
		tx = tx.Where("status = ?", *q.Status)
	}
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var roles []role.Role
	err := tx.Order("id").Offset((page - 1) * pageSize).Limit(pageSize).Find(&roles).Error
	return roles, total, err
}

func (r *roleRepo) FindByUserID(ctx context.Context, userID uint64) ([]role.Role, error) {
	var roles []role.Role
	err := r.client.GetDB(ctx).
		Joins("JOIN sys_user_roles ur ON ur.role_id = sys_roles.id").
		Where("ur.user_id = ? AND sys_roles.status = 1", userID).
		Find(&roles).Error
	return roles, err
}

package persistence

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"goKit/internal/modules/system/domain/entity"
	"goKit/internal/modules/system/domain/repository"
	domainsvc "goKit/internal/modules/system/domain/service"
	"goKit/pkg/kit/db"
)

type userRepo struct {
	client *db.Client
}

func NewUserRepository(client *db.Client) repository.UserRepository {
	return &userRepo{client: client}
}

func (r *userRepo) Create(ctx context.Context, user *entity.User) error {
	return r.client.GetDB(ctx).Create(user).Error
}

func (r *userRepo) Update(ctx context.Context, user *entity.User) error {
	return r.client.GetDB(ctx).Model(&entity.User{}).Where("id = ?", user.ID).
		Select("nickname", "email", "dept_id", "status").Updates(user).Error
}

func (r *userRepo) Delete(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).Where("id = ?", id).Delete(&entity.User{}).Error
}

func (r *userRepo) FindByID(ctx context.Context, id uint64) (*entity.User, error) {
	var u entity.User
	err := r.client.GetDB(ctx).Where("id = ?", id).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) FindByUsername(ctx context.Context, username string) (*entity.User, error) {
	var u entity.User
	err := r.client.GetDB(ctx).Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) List(ctx context.Context, filter domainsvc.Filter, page, pageSize int) ([]entity.User, int64, error) {
	tx := r.client.GetDB(ctx).Model(&entity.User{})
	tx = applyDataScope(tx, filter, "dept_id", "id")

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []entity.User
	err := tx.Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&users).Error
	return users, total, err
}

func (r *userRepo) UpdatePassword(ctx context.Context, id uint64, hashedPwd string) error {
	return r.client.GetDB(ctx).Model(&entity.User{}).Where("id = ?", id).
		Update("password", hashedPwd).Error
}

// applyDataScope 将数据权限过滤翻译成 SQL 条件。
// deptField 为归属部门字段，selfField 为“本人”匹配字段（用户表即主键 id，业务表可为 created_by）。
func applyDataScope(tx *gorm.DB, f domainsvc.Filter, deptField, selfField string) *gorm.DB {
	switch {
	case f.All:
		return tx
	case f.DenyAll:
		return tx.Where("1 = 0")
	case len(f.DeptIDs) > 0 && f.SelfID > 0:
		return tx.Where(deptField+" IN ? OR "+selfField+" = ?", f.DeptIDs, f.SelfID)
	case len(f.DeptIDs) > 0:
		return tx.Where(deptField+" IN ?", f.DeptIDs)
	default: // 仅本人
		return tx.Where(selfField+" = ?", f.SelfID)
	}
}

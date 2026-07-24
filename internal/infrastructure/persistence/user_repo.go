package persistence

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"goKit/internal/domain/shared/datascope"
	"goKit/internal/domain/user"
	"goKit/pkg/kit/db"
)

type userRepo struct {
	client *db.Client
}

func NewUserRepository(client *db.Client) user.UserRepository {
	return &userRepo{client: client}
}

func (r *userRepo) Create(ctx context.Context, u *user.User) error {
	return r.client.GetDB(ctx).Create(u).Error
}

func (r *userRepo) Update(ctx context.Context, u *user.User) error {
	return r.client.GetDB(ctx).Model(&user.User{}).Where("id = ?", u.ID).
		Select("nickname", "email", "dept_id", "status").Updates(u).Error
}

func (r *userRepo) Delete(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).Where("id = ?", id).Delete(&user.User{}).Error
}

func (r *userRepo) FindByID(ctx context.Context, id uint64) (*user.User, error) {
	var u user.User
	err := r.client.GetDB(ctx).Where("id = ?", id).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) FindByUsername(ctx context.Context, username string) (*user.User, error) {
	var u user.User
	err := r.client.GetDB(ctx).Where("username = ?", username).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &u, err
}

func (r *userRepo) FindByIDs(ctx context.Context, ids []uint64) ([]user.User, error) {
	var users []user.User
	if len(ids) == 0 {
		return users, nil
	}
	err := r.client.GetDB(ctx).Where("id IN ?", ids).Find(&users).Error
	return users, err
}

func (r *userRepo) List(ctx context.Context, filter datascope.Filter, page, pageSize int) ([]user.User, int64, error) {
	tx := r.client.GetDB(ctx).Model(&user.User{})
	tx = applyDataScope(tx, filter, "dept_id", "id")

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var users []user.User
	err := tx.Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&users).Error
	return users, total, err
}

func (r *userRepo) UpdatePassword(ctx context.Context, id uint64, hashedPwd string) error {
	return r.client.GetDB(ctx).Model(&user.User{}).Where("id = ?", id).
		Update("password", hashedPwd).Error
}

// applyDataScope 将数据权限过滤翻译成 SQL 条件。
// deptField 为归属部门字段，selfField 为“本人”匹配字段（用户表即主键 id，业务表可为 created_by）。
func applyDataScope(tx *gorm.DB, f datascope.Filter, deptField, selfField string) *gorm.DB {
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

package user

import (
	"context"

	"goKit/internal/domain/shared/datascope"
)

// UserRepository 用户仓储
type UserRepository interface {
	Create(ctx context.Context, user *User) error
	Update(ctx context.Context, user *User) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*User, error)
	FindByUsername(ctx context.Context, username string) (*User, error)
	// FindByIDs 按 ID 集合批量查询（仅返回存在的用户）
	FindByIDs(ctx context.Context, ids []uint64) ([]User, error)
	// List 按数据权限过滤分页查询
	List(ctx context.Context, filter datascope.Filter, page, pageSize int) ([]User, int64, error)
	UpdatePassword(ctx context.Context, id uint64, hashedPwd string) error
}

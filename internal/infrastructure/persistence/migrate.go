package persistence

import (
	"context"

	"goKit/internal/domain/dept"
	"goKit/internal/domain/menu"
	"goKit/internal/domain/role"
	"goKit/internal/domain/user"
	"goKit/pkg/kit/db"
)

// AutoMigrate 迁移 system 模块全部表结构
func AutoMigrate(ctx context.Context, client *db.Client) error {
	return client.GetDB(ctx).AutoMigrate(
		&user.User{},
		&role.Role{},
		&dept.Dept{},
		&menu.Menu{},
		&role.UserRole{},
		&role.RoleMenu{},
		&role.RoleDept{},
	)
}

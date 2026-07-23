package persistence

import (
	"context"

	"goKit/internal/modules/system/domain/entity"
	"goKit/pkg/kit/db"
)

// AutoMigrate 迁移 system 模块全部表结构
func AutoMigrate(ctx context.Context, client *db.Client) error {
	return client.GetDB(ctx).AutoMigrate(
		&entity.User{},
		&entity.Role{},
		&entity.Dept{},
		&entity.Menu{},
		&entity.UserRole{},
		&entity.RoleMenu{},
		&entity.RoleDept{},
	)
}

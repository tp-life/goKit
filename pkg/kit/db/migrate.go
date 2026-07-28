package db

import (
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/mysql"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// RunMigrations 基于 client 的 *sql.DB 执行内嵌的版本化迁移（golang-migrate）。
// ErrNoChange 视为成功；数据库处于 dirty 状态时返回明确的人工处理提示。
func RunMigrations(client *Client, fsys embed.FS) error {
	sqlDB, err := client.SQLDB()
	if err != nil {
		return fmt.Errorf("get sql.DB: %w", err)
	}

	source, err := iofs.New(fsys, ".")
	if err != nil {
		return fmt.Errorf("load embedded migrations: %w", err)
	}

	driver, err := mysql.WithInstance(sqlDB, &mysql.Config{})
	if err != nil {
		return fmt.Errorf("init mysql migrate driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "mysql", driver)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}

	if err := m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		var dirty migrate.ErrDirty
		if errors.As(err, &dirty) {
			return fmt.Errorf("database is dirty at version %d: fix the failed statement manually, then run `migrate force %d` before restarting: %w", dirty.Version, dirty.Version, err)
		}
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/plugin/dbresolver"
)

type Client struct {
	db *gorm.DB
}

type txKey struct{}

func NewClient(cfg Config, l *slog.Logger) (*Client, error) {
	gormLogger := NewSlogAdapter(l, parseLogLevel(cfg.LogMode), cfg.SlowThreshold)
	gormConfig := &gorm.Config{Logger: gormLogger, SkipDefaultTransaction: true, PrepareStmt: true}

	var dialector gorm.Dialector
	switch strings.ToLower(cfg.Driver) {
	case "mysql":
		dialector = mysql.Open(cfg.DSN)
	case "sqlite", "sqlite3":
		if err := ensureSQLiteDir(cfg.DSN); err != nil {
			return nil, err
		}
		dialector = sqlite.Open(cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported driver: %s", cfg.Driver)
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(cfg.Driver, "mysql") && len(cfg.Replicas) > 0 {
		var replicas []gorm.Dialector
		for _, dsn := range cfg.Replicas {
			replicas = append(replicas, mysql.Open(dsn))
		}
		if err := db.Use(dbresolver.Register(dbresolver.Config{Sources: []gorm.Dialector{dialector}, Replicas: replicas, Policy: dbresolver.RandomPolicy{}})); err != nil {
			return nil, err
		}
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	if cfg.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	}
	if cfg.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	}
	if cfg.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)
	}
	if strings.EqualFold(cfg.Driver, "sqlite") || strings.EqualFold(cfg.Driver, "sqlite3") {
		if _, err := sqlDB.Exec("PRAGMA journal_mode=WAL;"); err != nil {
			l.Warn("sqlite_pragma_wal_failed", slog.Any("err", err))
		}
		if _, err := sqlDB.Exec("PRAGMA busy_timeout=5000;"); err != nil {
			l.Warn("sqlite_pragma_busy_timeout_failed", slog.Any("err", err))
		}
	}
	return &Client{db: db}, nil
}

func ensureSQLiteDir(dsn string) error {
	if dsn == "" || dsn == ":memory:" || strings.HasPrefix(dsn, "file:") {
		return nil
	}
	dir := filepath.Dir(dsn)
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0o755)
}

func (c *Client) GetDB(ctx context.Context) *gorm.DB {
	if ctx != nil {
		if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
			return tx
		}
		return c.db.WithContext(ctx)
	}
	return c.db
}

func (c *Client) WithTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return c.GetDB(ctx).Transaction(func(tx *gorm.DB) error {
		txCtx := context.WithValue(ctx, txKey{}, tx)
		return fn(txCtx)
	})
}

func parseLogLevel(lvl string) logger.LogLevel {
	switch lvl {
	case "silent":
		return logger.Silent
	case "info":
		return logger.Info
	case "warn":
		return logger.Warn
	default:
		return logger.Error
	}
}

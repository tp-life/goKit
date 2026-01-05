package db

import (
	"context"
	"fmt"
	"log/slog"

	"gorm.io/driver/mysql"
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

	gormConfig := &gorm.Config{
		Logger:                 gormLogger,
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
	}

	var dialector gorm.Dialector
	if cfg.Driver == "mysql" {
		dialector = mysql.Open(cfg.DSN)
	} else {
		return nil, fmt.Errorf("unsupported driver: %s", cfg.Driver)
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, err
	}

	if len(cfg.Replicas) > 0 {
		var replicas []gorm.Dialector
		for _, dsn := range cfg.Replicas {
			replicas = append(replicas, mysql.Open(dsn))
		}
		err = db.Use(dbresolver.Register(dbresolver.Config{
			Sources:  []gorm.Dialector{dialector},
			Replicas: replicas,
			Policy:   dbresolver.RandomPolicy{},
		}))
		if err != nil {
			return nil, err
		}
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	return &Client{db: db}, nil
}

func (c *Client) GetDB(ctx context.Context) *gorm.DB {
	if tx, ok := ctx.Value(txKey{}).(*gorm.DB); ok {
		return tx
	}
	return c.db.WithContext(ctx)
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

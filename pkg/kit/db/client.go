package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	// 如果 logger 为 nil，使用默认 logger
	if l == nil {
		l = slog.Default()
	}
	
	// 检查配置是否有效
	if cfg.Driver == "" {
		return nil, fmt.Errorf("database driver is empty")
	}
	if cfg.DSN == "" {
		return nil, fmt.Errorf("database DSN is empty")
	}
	
	// 使用默认值填充配置
	if cfg.MaxIdleConns == 0 {
		cfg.MaxIdleConns = 10
	}
	if cfg.MaxOpenConns == 0 {
		cfg.MaxOpenConns = 100
	}
	if cfg.ConnMaxLifetime == 0 {
		cfg.ConnMaxLifetime = time.Hour
	}
	if cfg.SlowThreshold == 0 {
		cfg.SlowThreshold = 200 * time.Millisecond
	}
	if cfg.LogMode == "" {
		cfg.LogMode = "error"
	}
	
	gormLogger := NewSlogAdapter(l, parseLogLevel(cfg.LogMode), cfg.SlowThreshold)
	
	// 确保 gormLogger 不为 nil
	if gormLogger == nil {
		return nil, fmt.Errorf("failed to create gorm logger")
	}

	gormConfig := &gorm.Config{
		Logger:                 gormLogger,
		SkipDefaultTransaction: true,
		PrepareStmt:            true,
	}

	var dialector gorm.Dialector
	switch cfg.Driver {
	case "mysql":
		dialector = mysql.Open(cfg.DSN)
	case "sqlite":
		// 确保 SQLite 数据库目录存在
		if err := ensureSQLiteDir(cfg.DSN); err != nil {
			return nil, fmt.Errorf("ensure sqlite directory: %w", err)
		}
		dialector = sqlite.Open(cfg.DSN)
	default:
		return nil, fmt.Errorf("unsupported driver: %s", cfg.Driver)
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	
	// 确保 db 不为 nil
	if db == nil {
		return nil, fmt.Errorf("gorm db is nil after open")
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
		return nil, fmt.Errorf("get sql db: %w", err)
	}
	
	if sqlDB == nil {
		return nil, fmt.Errorf("sql db is nil")
	}
	
	// 设置连接池参数（使用默认值如果配置为0）
	maxIdleConns := cfg.MaxIdleConns
	if maxIdleConns == 0 {
		maxIdleConns = 10
	}
	maxOpenConns := cfg.MaxOpenConns
	if maxOpenConns == 0 {
		maxOpenConns = 100
	}
	connMaxLifetime := cfg.ConnMaxLifetime
	if connMaxLifetime == 0 {
		connMaxLifetime = time.Hour
	}
	
	sqlDB.SetMaxIdleConns(maxIdleConns)
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetConnMaxLifetime(connMaxLifetime)

	return &Client{db: db}, nil
}

func (c *Client) GetDB(ctx context.Context) *gorm.DB {
	if c == nil || c.db == nil {
		panic("db client is nil")
	}
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

// ensureSQLiteDir 确保 SQLite 数据库文件所在的目录存在
func ensureSQLiteDir(dsn string) error {
	// SQLite DSN 格式: "file:path/to/db.db?cache=shared&..."
	// 或: "path/to/db.db"
	
	var dbPath string
	if strings.HasPrefix(dsn, "file:") {
		// 提取文件路径（去掉 "file:" 前缀和查询参数）
		parts := strings.Split(dsn, "?")
		dbPath = strings.TrimPrefix(parts[0], "file:")
	} else {
		// 直接是文件路径
		parts := strings.Split(dsn, "?")
		dbPath = parts[0]
	}

	// 获取目录路径
	dir := filepath.Dir(dbPath)
	if dir == "." || dir == "" {
		// 当前目录，不需要创建
		return nil
	}

	// 创建目录（如果不存在）
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}

	return nil
}

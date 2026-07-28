package db

import "time"

type Config struct {
	Driver          string        `mapstructure:"driver"`
	DSN             string        `mapstructure:"dsn"`
	Replicas        []string      `mapstructure:"replicas"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
	LogMode         string        `mapstructure:"log_mode"`
	SlowThreshold   time.Duration `mapstructure:"slow_threshold"`
	// AutoMigrate 启动时自动迁移表结构（仅限开发/简单部署，生产建议用迁移工具）
	AutoMigrate bool `mapstructure:"auto_migrate"`
	// MigrationsEnabled 启动时执行 migrations/ 内嵌的版本化迁移（生产推荐；
	// 与 AutoMigrate 同开时优先生效，AutoMigrate 不再执行）
	MigrationsEnabled bool `mapstructure:"migrations_enabled"`
}

func DefaultConfig() Config {
	return Config{
		Driver:          "mysql",
		MaxIdleConns:    10,
		MaxOpenConns:    100,
		ConnMaxLifetime: time.Hour,
		LogMode:         "error",
		SlowThreshold:   200 * time.Millisecond,
	}
}

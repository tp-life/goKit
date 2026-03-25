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
}

func DefaultConfig() Config {
	return Config{
		Driver:          "sqlite",
		DSN:             "data/arbitrage.db",
		MaxIdleConns:    2,
		MaxOpenConns:    4,
		ConnMaxLifetime: time.Hour,
		LogMode:         "warn",
		SlowThreshold:   200 * time.Millisecond,
	}
}

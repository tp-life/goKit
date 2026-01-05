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
		Driver:          "mysql",
		MaxIdleConns:    10,
		MaxOpenConns:    100,
		ConnMaxLifetime: time.Hour,
		LogMode:         "error",
		SlowThreshold:   200 * time.Millisecond,
	}
}

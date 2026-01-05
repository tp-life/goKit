package rpc

import "time"

type Config struct {
	Port              string        `mapstructure:"port"`
	MaxConnectionIdle time.Duration `mapstructure:"max_connection_idle"`
	Timeout           time.Duration `mapstructure:"timeout"`
}

func DefaultConfig() Config {
	return Config{
		Port:              ":9090",
		MaxConnectionIdle: 300 * time.Second,
		Timeout:           5 * time.Second,
	}
}

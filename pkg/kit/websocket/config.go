package websocket

import "time"

type Config struct {
	URL               string        `mapstructure:"url"`
	ReconnectInterval time.Duration `mapstructure:"reconnect_interval"`
	MaxReconnectAttempts int        `mapstructure:"max_reconnect_attempts"`
	ReadTimeout       time.Duration `mapstructure:"read_timeout"`
	WriteTimeout      time.Duration `mapstructure:"write_timeout"`
	ConnectTimeout    time.Duration `mapstructure:"connect_timeout"` // 连接超时（单独设置）
	PingInterval      time.Duration `mapstructure:"ping_interval"`
	ProxyURL          string        `mapstructure:"proxy_url"`
}

func DefaultConfig() Config {
	return Config{
		ReconnectInterval:   5 * time.Second,
		MaxReconnectAttempts: 10,
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       10 * time.Second,
		ConnectTimeout:     30 * time.Second, // 连接超时默认 30 秒
		PingInterval:        30 * time.Second,
	}
}

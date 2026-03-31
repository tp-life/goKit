package tui

import (
	"strings"
	"time"
)

// Config 定义 TUI 展示层的运行配置。
type Config struct {
	Mode              string `mapstructure:"mode"`
	RefreshIntervalMS int    `mapstructure:"refresh_interval_ms"`
}

// Normalize 规范化模式与刷新间隔，避免错误输入导致界面异常。
func (c *Config) Normalize() {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	switch mode {
	case "tui", "both", "web":
		c.Mode = mode
	default:
		c.Mode = "web"
	}

	// TUI 刷新不需要太激进，避免远程终端高频重绘。
	if c.RefreshIntervalMS <= 0 {
		c.RefreshIntervalMS = 1000
	}
}

// TUIEnabled 判断当前模式是否需要启用终端界面。
func (c Config) TUIEnabled() bool {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	return mode == "tui" || mode == "both"
}

// WebEnabled 判断当前模式是否仍需启用 HTTP dashboard。
func (c Config) WebEnabled() bool {
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	return mode == "web" || mode == "both"
}

// RefreshInterval 返回 Bubble Tea 使用的刷新周期。
func (c Config) RefreshInterval() time.Duration {
	if c.RefreshIntervalMS <= 0 {
		return time.Second
	}
	return time.Duration(c.RefreshIntervalMS) * time.Millisecond
}

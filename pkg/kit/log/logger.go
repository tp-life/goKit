// pkg/kit/log/logger.go
package log

import (
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	globalLogger *slog.Logger
	once         sync.Once
)

// NewLogger 创建 slog 实例
func NewLogger(cfg Config) *slog.Logger {
	var level slog.Level
	switch strings.ToLower(cfg.Level) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		AddSource: cfg.Source,
		Level:     level,
	}

	// 根据配置选择日志输出目标；纯 TUI 模式下可切到 discard，避免刷花终端界面。
	output := io.Writer(os.Stdout)
	switch strings.ToLower(strings.TrimSpace(cfg.Output)) {
	case "stderr":
		output = os.Stderr
	case "discard":
		output = io.Discard
	}

	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		handler = slog.NewTextHandler(output, opts)
	} else {
		handler = slog.NewJSONHandler(output, opts)
	}

	// 包装 TraceHandler
	logger := slog.New(&TraceHandler{Handler: handler})

	// 设置为全局默认，方便非依赖注入场景使用 slog.Info()
	slog.SetDefault(logger)

	// 保存单例
	once.Do(func() {
		globalLogger = logger
	})

	return logger
}

// L 获取全局 Logger (可选)
func L() *slog.Logger {
	if globalLogger == nil {
		return slog.Default()
	}
	return globalLogger
}

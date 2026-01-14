// pkg/kit/log/logger.go
package log

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"sync"
)

var (
	globalLogger        *slog.Logger
	globalWrappedLogger *Logger
	once                sync.Once
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

	var handler slog.Handler
	if strings.ToLower(cfg.Format) == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// 确保 handler 不为 nil
	if handler == nil {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	// 包装 TraceHandler
	logger := slog.New(&TraceHandler{Handler: handler})

	// 设置为全局默认，方便非依赖注入场景使用 slog.Info()
	slog.SetDefault(logger)

	// 保存单例
	once.Do(func() {
		globalLogger = logger
		globalWrappedLogger = &Logger{Logger: logger}
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

// Fields 字段类型，用于简化日志记录
type Fields map[string]interface{}

// ToArgs 将 Fields 转换为 slog 参数（键值对形式）
func (f Fields) ToArgs() []any {
	if len(f) == 0 {
		return nil
	}
	args := make([]any, 0, len(f)*2) // 每个字段需要 key 和 value
	for k, v := range f {
		args = append(args, k, v)
	}
	return args
}

// Logger 封装 slog.Logger，提供更友好的 API
type Logger struct {
	*slog.Logger
}

// Wrap 包装 slog.Logger，提供更友好的 API（简化版，可以直接调用）
func Wrap(logger *slog.Logger) *Logger {
	if logger == nil {
		return Default()
	}
	return &Logger{Logger: logger}
}

// Default 获取全局默认的 Logger 包装器
func Default() *Logger {
	if globalWrappedLogger == nil {
		// 如果还没有初始化，使用默认的 slog.Logger
		return &Logger{Logger: slog.Default()}
	}
	return globalWrappedLogger
}

// With 创建带字段的 Logger
func (l *Logger) With(fields Fields) *Logger {
	args := fields.ToArgs()
	if len(args) == 0 {
		return l
	}
	return &Logger{Logger: l.Logger.With(args...)}
}

// Debug 记录 Debug 级别日志（使用 Fields）
func (l *Logger) Debug(msg string, fields Fields) {
	if len(fields) == 0 {
		l.Logger.Debug(msg)
		return
	}
	l.Logger.Debug(msg, fields.ToArgs()...)
}

// Info 记录 Info 级别日志（使用 Fields）
func (l *Logger) Info(msg string, fields Fields) {
	if len(fields) == 0 {
		l.Logger.Info(msg)
		return
	}
	l.Logger.Info(msg, fields.ToArgs()...)
}

// Warn 记录 Warn 级别日志（使用 Fields）
func (l *Logger) Warn(msg string, fields Fields) {
	if len(fields) == 0 {
		l.Logger.Warn(msg)
		return
	}
	l.Logger.Warn(msg, fields.ToArgs()...)
}

// Error 记录 Error 级别日志（使用 Fields）
func (l *Logger) Error(msg string, fields Fields) {
	if len(fields) == 0 {
		l.Logger.Error(msg)
		return
	}
	l.Logger.Error(msg, fields.ToArgs()...)
}

// DebugContext 记录 Debug 级别日志（带 Context）
func (l *Logger) DebugContext(ctx context.Context, msg string, fields Fields) {
	if len(fields) == 0 {
		l.Logger.DebugContext(ctx, msg)
		return
	}
	l.Logger.DebugContext(ctx, msg, fields.ToArgs()...)
}

// InfoContext 记录 Info 级别日志（带 Context）
func (l *Logger) InfoContext(ctx context.Context, msg string, fields Fields) {
	if len(fields) == 0 {
		l.Logger.InfoContext(ctx, msg)
		return
	}
	l.Logger.InfoContext(ctx, msg, fields.ToArgs()...)
}

// WarnContext 记录 Warn 级别日志（带 Context）
func (l *Logger) WarnContext(ctx context.Context, msg string, fields Fields) {
	if len(fields) == 0 {
		l.Logger.WarnContext(ctx, msg)
		return
	}
	l.Logger.WarnContext(ctx, msg, fields.ToArgs()...)
}

// ErrorContext 记录 Error 级别日志（带 Context）
func (l *Logger) ErrorContext(ctx context.Context, msg string, fields Fields) {
	if len(fields) == 0 {
		l.Logger.ErrorContext(ctx, msg)
		return
	}
	l.Logger.ErrorContext(ctx, msg, fields.ToArgs()...)
}

// 包级别函数，直接使用默认 logger

// Debug 记录 Debug 级别日志
func Debug(msg string, fields Fields) {
	Default().Debug(msg, fields)
}

// Info 记录 Info 级别日志
func Info(msg string, fields Fields) {
	Default().Info(msg, fields)
}

// Warn 记录 Warn 级别日志
func Warn(msg string, fields Fields) {
	Default().Warn(msg, fields)
}

// Error 记录 Error 级别日志
func Error(msg string, fields Fields) {
	Default().Error(msg, fields)
}

// DebugContext 记录 Debug 级别日志（带 Context）
func DebugContext(ctx context.Context, msg string, fields Fields) {
	Default().DebugContext(ctx, msg, fields)
}

// InfoContext 记录 Info 级别日志（带 Context）
func InfoContext(ctx context.Context, msg string, fields Fields) {
	Default().InfoContext(ctx, msg, fields)
}

// WarnContext 记录 Warn 级别日志（带 Context）
func WarnContext(ctx context.Context, msg string, fields Fields) {
	Default().WarnContext(ctx, msg, fields)
}

// ErrorContext 记录 Error 级别日志（带 Context）
func ErrorContext(ctx context.Context, msg string, fields Fields) {
	Default().ErrorContext(ctx, msg, fields)
}

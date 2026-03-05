package db

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm/logger"
)

type SlogAdapter struct {
	l             *slog.Logger
	LogLevel      logger.LogLevel
	SlowThreshold time.Duration
}

func NewSlogAdapter(l *slog.Logger, level logger.LogLevel, slow time.Duration) *SlogAdapter {
	return &SlogAdapter{l: l, LogLevel: level, SlowThreshold: slow}
}

func (s *SlogAdapter) LogMode(level logger.LogLevel) logger.Interface {
	newS := *s
	newS.LogLevel = level
	return &newS
}

func (s *SlogAdapter) Info(ctx context.Context, str string, args ...any) {
	if s.LogLevel >= logger.Info {
		s.l.InfoContext(ctx, fmt.Sprintf(str, args...))
	}
}

func (s *SlogAdapter) Warn(ctx context.Context, str string, args ...any) {
	if s.LogLevel >= logger.Warn {
		s.l.WarnContext(ctx, fmt.Sprintf(str, args...))
	}
}

func (s *SlogAdapter) Error(ctx context.Context, str string, args ...any) {
	if s.LogLevel >= logger.Error {
		s.l.ErrorContext(ctx, fmt.Sprintf(str, args...))
	}
}

func (s *SlogAdapter) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if s.LogLevel <= logger.Silent {
		return
	}
	elapsed := time.Since(begin)
	sql, rows := fc()

	fields := []any{
		slog.String("sql", sql),
		slog.Int64("rows", rows),
		slog.Duration("lat", elapsed),
		// 【修改点】：不再使用 Gorm 的 utils.FileWithLineNum()，改用我们自定义的追踪函数
		slog.String("loc", fileWithLineNum()),
	}

	if err != nil && s.LogLevel >= logger.Error {
		s.l.ErrorContext(ctx, "sql_err", append(fields, slog.Any("err", err))...)
		return
	}
	if s.SlowThreshold != 0 && elapsed > s.SlowThreshold && s.LogLevel >= logger.Warn {
		s.l.WarnContext(ctx, "sql_slow", fields...)
		return
	}
	if s.LogLevel == logger.Info {
		s.l.InfoContext(ctx, "sql_exec", fields...)
	}
}

// fileWithLineNum 核心修复：自定义的调用栈寻址函数
func fileWithLineNum() string {
	// 从第 2 层调用栈开始往上找，最多找 15 层
	for i := 2; i < 15; i++ {
		_, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}

		// 跳过 Gorm 框架的内部文件，同时【跳过我们自己的 DB 封装层】
		if strings.Contains(file, "gorm.io") || strings.Contains(file, "pkg/kit/db") {
			continue
		}

		// 找到了真实的业务代码路径
		return file + ":" + strconv.Itoa(line)
	}
	return ""
}

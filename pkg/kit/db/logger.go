package db

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
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
		slog.String("loc", utils.FileWithLineNum()),
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

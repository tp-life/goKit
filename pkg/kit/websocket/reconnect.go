package websocket

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type ReconnectManager struct {
	interval      time.Duration
	maxAttempts   int
	logger        *slog.Logger
	attempts      int
	mu            sync.Mutex
	reconnectChan chan struct{}
}

func NewReconnectManager(interval time.Duration, maxAttempts int, logger *slog.Logger) *ReconnectManager {
	return &ReconnectManager{
		interval:      interval,
		maxAttempts:   maxAttempts,
		logger:        logger,
		reconnectChan: make(chan struct{}, 1),
	}
}

func (r *ReconnectManager) Do(ctx context.Context, fn func() error) error {
	r.mu.Lock()
	r.attempts = 0
	r.mu.Unlock()

	for {
		err := fn()
		if err == nil {
			r.mu.Lock()
			r.attempts = 0
			r.mu.Unlock()
			return nil
		}

		r.mu.Lock()
		r.attempts++
		attempts := r.attempts
		r.mu.Unlock()

		if attempts >= r.maxAttempts {
			return err
		}

		// 指数退避
		backoff := r.interval * time.Duration(1<<uint(attempts-1))
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}

		r.logger.Warn("websocket_reconnect_attempt",
			slog.Int("attempt", attempts),
			slog.Duration("backoff", backoff),
			slog.Any("err", err),
		)

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			// 继续重连
		case <-r.reconnectChan:
			// 立即重连
		}
	}
}

func (r *ReconnectManager) Trigger() {
	select {
	case r.reconnectChan <- struct{}{}:
	default:
	}
}

package exchange

import (
	"context"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// dynamicPublicStreamConfig 把“动态 public websocket 长连接”所需的框架行为收敛成一组 hook。
//
// 这层 manager 的目标很克制，只处理重复最多的公共骨架：
// 1. 建连 / 重连 / backoff；
// 2. 定期刷新当前订阅或本地 watch；
// 3. 可选 keepalive；
// 4. 统一 read loop 与 ctx 退出。
//
// 而各交易所 family 自己保留的内容包括：
// - 如何生成当前 snapshot；
// - 初次建连后要订阅什么；
// - refresh 时如何同步 topic；
// - 收到消息后如何解析并写入 sink。
//
// 这样既能减少 Binance-like / Bybit 两边的重复样板，
// 也不会把交易所私有语义过早挤进一个过度抽象的“大一统 manager”里。
type dynamicPublicStreamConfig[T any] struct {
	Endpoint        string
	Dial            func(context.Context, string) (*websocket.Conn, error)
	ResolveSnapshot func() (T, bool)
	OnConnect       func(*websocket.Conn, T) error
	OnRefresh       func(*websocket.Conn, T) error
	OnMessage       func([]byte, T)
	Keepalive       func(context.Context, *websocket.Conn, <-chan struct{})
	OnConnected     func()
	OnDisconnected  func(error)
	RefreshInterval time.Duration
	ReadTimeout     time.Duration
	IdleWait        time.Duration
	MaxBackoff      time.Duration
}

func runDynamicPublicStream[T any](ctx context.Context, cfg dynamicPublicStreamConfig[T]) {
	refreshInterval := cfg.RefreshInterval
	if refreshInterval <= 0 {
		refreshInterval = 2 * time.Second
	}
	readTimeout := cfg.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 30 * time.Second
	}
	idleWait := cfg.IdleWait
	if idleWait <= 0 {
		idleWait = 2 * time.Second
	}
	maxBackoff := cfg.MaxBackoff
	if maxBackoff <= 0 {
		maxBackoff = 15 * time.Second
	}

	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		snapshot, ok := cfg.ResolveSnapshot()
		if !ok {
			if !sleepContext(ctx, idleWait) {
				return
			}
			continue
		}

		conn, err := cfg.Dial(ctx, cfg.Endpoint)
		if err != nil {
			if cfg.OnDisconnected != nil {
				cfg.OnDisconnected(err)
			}
			if !sleepContext(ctx, backoff) {
				return
			}
			if backoff < maxBackoff {
				backoff *= 2
			}
			continue
		}

		if cfg.OnConnect != nil {
			if err := cfg.OnConnect(conn, snapshot); err != nil {
				_ = conn.Close()
				if cfg.OnDisconnected != nil {
					cfg.OnDisconnected(err)
				}
				if !sleepContext(ctx, backoff) {
					return
				}
				if backoff < maxBackoff {
					backoff *= 2
				}
				continue
			}
		}
		configureWebSocketReadDeadline(conn, readTimeout)

		backoff = time.Second
		if cfg.OnConnected != nil {
			cfg.OnConnected()
		}

		var (
			mu      sync.RWMutex
			current = snapshot
		)

		stopAux := make(chan struct{})
		go func() {
			select {
			case <-ctx.Done():
				_ = conn.Close()
			case <-stopAux:
			}
		}()
		if cfg.Keepalive != nil {
			go cfg.Keepalive(ctx, conn, stopAux)
		}
		go func() {
			ticker := time.NewTicker(refreshInterval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-stopAux:
					return
				case <-ticker.C:
					next, ok := cfg.ResolveSnapshot()
					if !ok {
						continue
					}
					if cfg.OnRefresh != nil {
						if err := cfg.OnRefresh(conn, next); err != nil {
							_ = conn.Close()
							return
						}
					}
					mu.Lock()
					current = next
					mu.Unlock()
				}
			}
		}()

		for {
			_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
			_, msg, err := conn.ReadMessage()
			if err != nil {
				close(stopAux)
				_ = conn.Close()
				if cfg.OnDisconnected != nil {
					cfg.OnDisconnected(err)
				}
				break
			}
			mu.RLock()
			currentSnapshot := current
			mu.RUnlock()
			if cfg.OnMessage != nil {
				cfg.OnMessage(msg, currentSnapshot)
			}
		}
	}
}

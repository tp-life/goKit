package websocket

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Client struct {
	config      Config
	logger      *slog.Logger
	conn        *websocket.Conn
	connMutex   sync.RWMutex
	msgChan     chan []byte
	errChan     chan error
	closeChan   chan struct{}
	reconnect   *ReconnectManager
	proxyDialer *ProxyDialer
}

func NewClient(cfg Config, logger *slog.Logger) *Client {
	client := &Client{
		config:    cfg,
		logger:    logger,
		msgChan:   make(chan []byte, 100),
		errChan:   make(chan error, 10),
		closeChan: make(chan struct{}),
		reconnect: NewReconnectManager(cfg.ReconnectInterval, cfg.MaxReconnectAttempts, logger),
	}

	if cfg.ProxyURL != "" {
		client.proxyDialer = NewProxyDialer(cfg.ProxyURL, logger)
	}

	return client
}

func (c *Client) Connect(ctx context.Context) error {
	return c.reconnect.Do(ctx, func() error {
		return c.connect(ctx)
	})
}

func (c *Client) connect(ctx context.Context) error {
	// 使用连接超时（如果配置了），否则使用 WriteTimeout，最小 30 秒
	connectTimeout := c.config.ConnectTimeout
	if connectTimeout == 0 {
		connectTimeout = c.config.WriteTimeout
	}
	if connectTimeout < 30*time.Second {
		connectTimeout = 30 * time.Second // 至少 30 秒
	}
	
	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()

	dialOptions := websocket.DialOptions{
		HTTPClient: &http.Client{
			Timeout: connectTimeout,
		},
	}

	if c.proxyDialer != nil {
		dialOptions.HTTPClient.Transport = c.proxyDialer.Transport()
	}

	conn, _, err := websocket.Dial(connectCtx, c.config.URL, &dialOptions)
	if err != nil {
		return fmt.Errorf("failed to WebSocket dial: %w", err)
	}

	// 设置读取限制，支持更大的消息（默认 32KB，增加到 1MB）
	conn.SetReadLimit(1024 * 1024) // 1MB

	c.connMutex.Lock()
	c.conn = conn
	c.connMutex.Unlock()

	c.logger.Info("websocket_connected", slog.String("url", c.config.URL))

	// 启动读写协程
	go c.readLoop()
	go c.pingLoop()

	return nil
}

func (c *Client) readLoop() {
	for {
		select {
		case <-c.closeChan:
			return
		default:
			c.connMutex.RLock()
			conn := c.conn
			c.connMutex.RUnlock()

			if conn == nil {
				time.Sleep(100 * time.Millisecond)
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), c.config.ReadTimeout)
			_, msg, err := conn.Read(ctx)
			cancel()

			if err != nil {
				c.errChan <- err
				c.reconnect.Trigger()
				return
			}

			select {
			case c.msgChan <- msg:
			default:
				c.logger.Warn("websocket_message_channel_full")
			}
		}
	}
}

func (c *Client) pingLoop() {
	ticker := time.NewTicker(c.config.PingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.closeChan:
			return
		case <-ticker.C:
			c.connMutex.RLock()
			conn := c.conn
			c.connMutex.RUnlock()

			if conn == nil {
				continue
			}

			ctx, cancel := context.WithTimeout(context.Background(), c.config.WriteTimeout)
			err := conn.Ping(ctx)
			cancel()

			if err != nil {
				c.logger.Warn("websocket_ping_failed", slog.Any("err", err))
			}
		}
	}
}

func (c *Client) Write(ctx context.Context, message []byte) error {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()

	if c.conn == nil {
		return ErrNotConnected
	}

	writeCtx, cancel := context.WithTimeout(ctx, c.config.WriteTimeout)
	defer cancel()

	return c.conn.Write(writeCtx, websocket.MessageText, message)
}

func (c *Client) Read() <-chan []byte {
	return c.msgChan
}

func (c *Client) Errors() <-chan error {
	return c.errChan
}

func (c *Client) IsConnected() bool {
	c.connMutex.RLock()
	defer c.connMutex.RUnlock()
	return c.conn != nil
}

func (c *Client) Close() error {
	close(c.closeChan)

	c.connMutex.Lock()
	defer c.connMutex.Unlock()

	if c.conn != nil {
		err := c.conn.Close(websocket.StatusNormalClosure, "normal closure")
		c.conn = nil
		return err
	}

	return nil
}

var ErrNotConnected = errors.New("websocket not connected")

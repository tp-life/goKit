package polymarket

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/gorilla/websocket"
	xproxy "golang.org/x/net/proxy"
)

type contextDialerFunc func(ctx context.Context, network, address string) (net.Conn, error)

// DialContext 让函数类型也能直接作为带上下文的拨号器使用。
func (fn contextDialerFunc) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return fn(ctx, network, address)
}

// NewProxyHTTPClient 根据配置创建一个支持代理的 HTTP 客户端。
func NewProxyHTTPClient(cfg Config, timeout time.Duration) (*http.Client, error) {
	transport, err := newProxyHTTPTransport(cfg)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
	}, nil
}

// NewProxyWebsocketDialer 根据配置创建一个支持代理的 websocket 拨号器。
func NewProxyWebsocketDialer(cfg Config, handshakeTimeout time.Duration) (*websocket.Dialer, error) {
	dialer := &websocket.Dialer{
		HandshakeTimeout: handshakeTimeout,
	}

	proxyURL, err := parseConfiguredProxyURL(cfg.ProxyURL)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		dialer.Proxy = http.ProxyFromEnvironment
		return dialer, nil
	}

	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		dialer.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		proxyDialer, err := newSOCKSContextDialer(proxyURL)
		if err != nil {
			return nil, err
		}
		dialer.NetDialContext = proxyDialer.DialContext
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
	return dialer, nil
}

// DialProxyEthClient 根据配置为 Polygon RPC 创建一个支持代理的链上客户端。
func DialProxyEthClient(ctx context.Context, cfg Config) (*ethclient.Client, error) {
	rpcURL := strings.TrimSpace(cfg.PolygonRPCURL)
	if rpcURL == "" {
		return nil, fmt.Errorf("polygon rpc url is empty")
	}

	parsedURL, err := url.Parse(rpcURL)
	if err != nil {
		return nil, err
	}

	options := make([]rpc.ClientOption, 0, 1)
	switch strings.ToLower(parsedURL.Scheme) {
	case "ws", "wss":
		dialer, err := NewProxyWebsocketDialer(cfg, 10*time.Second)
		if err != nil {
			return nil, err
		}
		options = append(options, rpc.WithWebsocketDialer(*dialer))
	default:
		httpClient, err := NewProxyHTTPClient(cfg, 12*time.Second)
		if err != nil {
			return nil, err
		}
		options = append(options, rpc.WithHTTPClient(httpClient))
	}

	rpcClient, err := rpc.DialOptions(ctx, rpcURL, options...)
	if err != nil {
		return nil, err
	}
	return ethclient.NewClient(rpcClient), nil
}

// newProxyHTTPTransport 根据代理协议构造 HTTP transport。
func newProxyHTTPTransport(cfg Config) (*http.Transport, error) {
	transport := &http.Transport{}

	proxyURL, err := parseConfiguredProxyURL(cfg.ProxyURL)
	if err != nil {
		return nil, err
	}
	if proxyURL == nil {
		transport.Proxy = http.ProxyFromEnvironment
		return transport, nil
	}

	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		proxyDialer, err := newSOCKSContextDialer(proxyURL)
		if err != nil {
			return nil, err
		}
		transport.DialContext = proxyDialer.DialContext
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", proxyURL.Scheme)
	}
	return transport, nil
}

// parseConfiguredProxyURL 把配置中的代理地址解析成 URL；为空时返回 nil。
func parseConfiguredProxyURL(raw string) (*url.URL, error) {
	normalized := strings.TrimSpace(raw)
	if normalized == "" {
		return nil, nil
	}

	parsedURL, err := url.Parse(normalized)
	if err != nil {
		return nil, err
	}
	if parsedURL.Scheme == "" {
		return nil, fmt.Errorf("proxy url missing scheme")
	}
	if parsedURL.Host == "" {
		return nil, fmt.Errorf("proxy url missing host")
	}
	return parsedURL, nil
}

// newSOCKSContextDialer 创建一个支持上下文取消的 SOCKS 拨号器。
func newSOCKSContextDialer(proxyURL *url.URL) (contextDialerFunc, error) {
	baseDialer, err := xproxy.FromURL(proxyURL, &net.Dialer{
		Timeout:   12 * time.Second,
		KeepAlive: 30 * time.Second,
	})
	if err != nil {
		return nil, err
	}
	if contextDialer, ok := baseDialer.(xproxy.ContextDialer); ok {
		return contextDialer.DialContext, nil
	}

	// 兜底兼容未实现 ContextDialer 的代理拨号器，避免因为库差异直接失效。
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		type dialResult struct {
			conn net.Conn
			err  error
		}
		done := make(chan dialResult, 1)
		go func() {
			conn, err := baseDialer.Dial(network, address)
			done <- dialResult{conn: conn, err: err}
		}()

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case result := <-done:
			if result.conn != nil && ctx.Err() != nil {
				result.conn.Close()
				return nil, ctx.Err()
			}
			return result.conn, result.err
		}
	}, nil
}

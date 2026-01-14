package websocket

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/url"

	"golang.org/x/net/proxy"
)

type ProxyDialer struct {
	proxyURL string
	logger   *slog.Logger
	transport *http.Transport
}

func NewProxyDialer(proxyURL string, logger *slog.Logger) *ProxyDialer {
	pd := &ProxyDialer{
		proxyURL: proxyURL,
		logger:   logger,
	}

	// 创建代理 transport
	proxyURLParsed, err := url.Parse(proxyURL)
	if err == nil {
		dialer, err := proxy.SOCKS5("tcp", proxyURLParsed.Host, nil, proxy.Direct)
		if err == nil {
			pd.transport = &http.Transport{
				DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
					return dialer.Dial(network, addr)
				},
			}
		}
	}

	return pd
}

func (p *ProxyDialer) Transport() *http.Transport {
	return p.transport
}

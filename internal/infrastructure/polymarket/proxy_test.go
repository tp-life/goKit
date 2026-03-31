package polymarket

import (
	"net/http"
	"testing"
	"time"
)

// TestNewProxyHTTPClientWithHTTPProxy 验证 HTTP 代理地址会正确挂到 transport 上。
func TestNewProxyHTTPClientWithHTTPProxy(t *testing.T) {
	client, err := NewProxyHTTPClient(Config{ProxyURL: "http://127.0.0.1:7890"}, time.Second)
	if err != nil {
		t.Fatalf("expected http proxy client, got error %v", err)
	}

	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected http.Transport, got %T", client.Transport)
	}
	if transport.Proxy == nil {
		t.Fatalf("expected proxy function to be configured")
	}

	req, err := http.NewRequest(http.MethodGet, "https://example.com", nil)
	if err != nil {
		t.Fatalf("unexpected request build error: %v", err)
	}
	proxyURL, err := transport.Proxy(req)
	if err != nil {
		t.Fatalf("unexpected proxy resolution error: %v", err)
	}
	if proxyURL == nil || proxyURL.String() != "http://127.0.0.1:7890" {
		t.Fatalf("unexpected proxy url: %#v", proxyURL)
	}
}

// TestNewProxyWebsocketDialerWithSOCKS5 验证 SOCKS5 代理会把 websocket 切到自定义拨号器。
func TestNewProxyWebsocketDialerWithSOCKS5(t *testing.T) {
	dialer, err := NewProxyWebsocketDialer(Config{ProxyURL: "socks5://127.0.0.1:7891"}, time.Second)
	if err != nil {
		t.Fatalf("expected socks5 websocket dialer, got error %v", err)
	}
	if dialer.NetDialContext == nil {
		t.Fatalf("expected NetDialContext to be configured for socks5 proxy")
	}
	if dialer.Proxy != nil {
		t.Fatalf("expected HTTP proxy function to stay nil when using socks5")
	}
}

// TestParseConfiguredProxyURLRejectsMissingScheme 验证缺少协议头的代理地址会被明确拒绝。
func TestParseConfiguredProxyURLRejectsMissingScheme(t *testing.T) {
	if _, err := parseConfiguredProxyURL("127.0.0.1:7890"); err == nil {
		t.Fatalf("expected invalid proxy url to be rejected")
	}
}

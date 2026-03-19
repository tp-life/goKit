package exchange

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/spf13/viper"
	xproxy "golang.org/x/net/proxy"
)

func loadAppConfig() AppConfig {
	return AppConfig{Debug: viper.GetBool("app.debug")}
}

func effectiveTimeout(base time.Duration, appCfg AppConfig) time.Duration {
	if base <= 0 {
		base = 10 * time.Second
	}
	if appCfg.Debug && base < 3600*time.Second {
		return 3600 * time.Second
	}
	return base
}

func newHTTPClient(cfg ExchangeConfig, appCfg AppConfig, logger *slog.Logger, exchangeName string) *http.Client {
	timeout := effectiveTimeout(cfg.RequestTimeout, appCfg)
	transport := &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ExpectContinueTimeout: 30 * time.Second,
	}
	applyProxyToHTTPTransport(transport, cfg.Proxy, logger, exchangeName)
	return &http.Client{Timeout: timeout, Transport: transport}
}

func newWebSocketDialer(cfg ExchangeConfig, appCfg AppConfig, logger *slog.Logger, exchangeName string) *websocket.Dialer {
	timeout := effectiveTimeout(cfg.RequestTimeout, appCfg)
	dialer := &websocket.Dialer{Proxy: nil, HandshakeTimeout: timeout, EnableCompression: true}
	applyProxyToWSDialer(dialer, cfg.Proxy, logger, exchangeName)
	return dialer
}

// resolvePrivateWSBaseURL 统一挑选“私有订单/用户流”应该连接到哪个 websocket 地址。
//
// 这里故意把私有流地址单独抽出来，而不是继续复用 `public_ws_base_url`，原因有两点：
// 1. 有些交易所公有 market ws 和私有 order ws 根本不是同一个入口，例如 Bybit V5；
// 2. 就算当前某个交易所二者恰好相同，把私有流地址显式配置出来，后续扩族时也更不容易误连。
//
// 选择优先级：
// 1. `private_ws_base_url`
// 2. 调用方传入的 family-level fallback
// 3. `public_ws_base_url`
func resolvePrivateWSBaseURL(cfg ExchangeConfig, fallback string) string {
	if value := strings.TrimSpace(cfg.PrivateWSBaseURL); value != "" {
		return value
	}
	if value := strings.TrimSpace(fallback); value != "" {
		return value
	}
	return strings.TrimSpace(cfg.PublicWSBaseURL)
}

func applyProxyToHTTPTransport(transport *http.Transport, proxyCfg ProxyConfig, logger *slog.Logger, exchangeName string) {
	if !proxyCfg.Enabled || strings.TrimSpace(proxyCfg.URL) == "" {
		return
	}
	proxyURL, err := url.Parse(proxyCfg.URL)
	if err != nil {
		logger.Warn("exchange_proxy_invalid", slog.String("exchange", exchangeName), slog.Any("err", err))
		return
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		dialer, err := xproxy.FromURL(proxyURL, xproxy.Direct)
		if err != nil {
			logger.Warn("exchange_proxy_invalid", slog.String("exchange", exchangeName), slog.Any("err", err))
			return
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.Dial(network, addr)
		}
	default:
		logger.Warn("exchange_proxy_scheme_unsupported", slog.String("exchange", exchangeName), slog.String("scheme", proxyURL.Scheme))
	}
}

func applyProxyToWSDialer(dialer *websocket.Dialer, proxyCfg ProxyConfig, logger *slog.Logger, exchangeName string) {
	if !proxyCfg.Enabled || strings.TrimSpace(proxyCfg.URL) == "" {
		return
	}
	proxyURL, err := url.Parse(proxyCfg.URL)
	if err != nil {
		logger.Warn("exchange_proxy_invalid", slog.String("exchange", exchangeName), slog.Any("err", err))
		return
	}
	switch strings.ToLower(proxyURL.Scheme) {
	case "http", "https":
		dialer.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		proxyDialer, err := xproxy.FromURL(proxyURL, xproxy.Direct)
		if err != nil {
			logger.Warn("exchange_proxy_invalid", slog.String("exchange", exchangeName), slog.Any("err", err))
			return
		}
		dialer.NetDialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return proxyDialer.Dial(network, addr)
		}
	default:
		logger.Warn("exchange_proxy_scheme_unsupported", slog.String("exchange", exchangeName), slog.String("scheme", proxyURL.Scheme))
	}
}

func mustFloat(v string) float64 {
	if v == "" {
		return 0
	}
	f, _ := strconv.ParseFloat(v, 64)
	return f
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func signedQuery(params url.Values, secret string) string {
	payload := params.Encode()
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return payload + "&signature=" + hex.EncodeToString(mac.Sum(nil))
}

func normalizeAllowed(allowed map[string]struct{}, keys ...string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, key := range keys {
		for _, normalized := range allowedLookupKeys(key) {
			if _, ok := allowed[normalized]; ok {
				return true
			}
		}
	}
	return false
}

// normalizeSymbolAlias 把 symbol/base asset 归一到策略内部使用的 canonical asset key。
//
// 规则：
// 1. 统一 trim + upper；
// 2. 命中 alias 表时映射到主 canonical 名称；
// 3. 未命中则原样返回。
//
// 这层归一化的目的，是让：
// - canonicalFrom() 在构建全市场 watchlist 时不会把 BTC / XBT 错分成两个币；
// - normalizeAllowed() 在处理 AllowedSymbols 白名单时，也能接受用户写 BTC 或 XBT。
func normalizeSymbolAlias(symbol string) string {
	return defaultSymbolRegistry.NormalizeAsset(symbol)
}

// allowedLookupKeys 返回一个输入 symbol 在 allowlist 匹配时应该尝试的全部 key。
//
// 例如：
// - 输入 BTC => [BTC]
// - 输入 XBT => [XBT, BTC]
//
// 这样用户配置 `allowed_symbols: [BTC]` 时，交易所若返回 `XBTUSDT` 也能匹配上；
// 同时用户若明确写 `XBT`，也依然兼容。
func allowedLookupKeys(symbol string) []string {
	return defaultSymbolRegistry.AllowedLookupKeys(symbol)
}

func canonicalFrom(raw, base string) string {
	return defaultSymbolRegistry.CanonicalAsset(raw, base)
}

func stepFromDecimals(decimals int) string {
	if decimals <= 0 {
		return "1"
	}
	return "0." + strings.Repeat("0", decimals-1) + "1"
}

func parseNullableFloat(v any) float64 {
	switch val := v.(type) {
	case string:
		return mustFloat(val)
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

func readJSONBody(resp *http.Response, out any) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("status=%d body=%s", resp.StatusCode, string(body))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func newJSONRequest(ctx context.Context, method, endpoint string, payload any) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

type baseStatusHolder struct {
	statusMu sync.RWMutex
	status   ConnectorStatus
}

func (h *baseStatusHolder) currentStatus() ConnectorStatus {
	h.statusMu.RLock()
	defer h.statusMu.RUnlock()
	return h.status
}

func (h *baseStatusHolder) updateStatus(fn func(*ConnectorStatus)) {
	h.statusMu.Lock()
	defer h.statusMu.Unlock()
	fn(&h.status)
}

func sortSymbols(items []string) []string {
	cp := append([]string(nil), items...)
	sort.Strings(cp)
	return cp
}

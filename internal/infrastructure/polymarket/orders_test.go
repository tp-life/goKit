package polymarket

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestTickSizeResponseAcceptsNumber 确认 tick-size 元数据在服务端返回 number 时也能正常解析。
func TestTickSizeResponseAcceptsNumber(t *testing.T) {
	var resp TickSizeResponse
	if err := json.Unmarshal([]byte(`{"minimum_tick_size":0.01}`), &resp); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if got := resp.MinimumTickSize.String(); got != "0.01" {
		t.Fatalf("unexpected tick size %q", got)
	}
}

// TestTickSizeResponseAcceptsString 确认 tick-size 元数据在服务端返回 string 时也能正常解析。
func TestTickSizeResponseAcceptsString(t *testing.T) {
	var resp TickSizeResponse
	if err := json.Unmarshal([]byte(`{"minimum_tick_size":"0.001"}`), &resp); err != nil {
		t.Fatalf("unexpected unmarshal error: %v", err)
	}
	if got := resp.MinimumTickSize.String(); got != "0.001" {
		t.Fatalf("unexpected tick size %q", got)
	}
}

// TestFeeRateResponseAcceptsStringOrNumber 确认 fee-rate 对 string 和 number 都具备兼容性。
func TestFeeRateResponseAcceptsStringOrNumber(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{name: "number", raw: `{"base_fee":25}`, want: 25},
		{name: "string", raw: `{"base_fee":"30"}`, want: 30},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var resp FeeRateResponse
			if err := json.Unmarshal([]byte(tc.raw), &resp); err != nil {
				t.Fatalf("unexpected unmarshal error: %v", err)
			}
			if got := resp.BaseFee.OrZero(); got != tc.want {
				t.Fatalf("unexpected base fee %d", got)
			}
		})
	}
}

// TestRandomUint64StaysWithinJSSafeIntegerRange 确认订单 salt 不会超出 JS 安全整数范围。
func TestRandomUint64StaysWithinJSSafeIntegerRange(t *testing.T) {
	for idx := 0; idx < 32; idx++ {
		salt, err := randomUint64()
		if err != nil {
			t.Fatalf("randomUint64 returned error: %v", err)
		}
		if salt == 0 {
			t.Fatalf("expected salt to be positive")
		}
		if salt > maxJSSafeInteger {
			t.Fatalf("salt %d exceeds max JS safe integer", salt)
		}
	}
}

// TestPlaceLimitOrderRefreshesInvalidConfiguredAPIKey 确认配置中的旧 API key 失效时会自动刷新并重试。
func TestPlaceLimitOrderRefreshesInvalidConfiguredAPIKey(t *testing.T) {
	const staleKey = "stale-api-key"
	const freshKey = "fresh-api-key"
	const freshSecret = "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

	orderCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/tick-size":
			_, _ = io.WriteString(w, `{"minimum_tick_size":0.01}`)
		case r.URL.Path == "/neg-risk":
			_, _ = io.WriteString(w, `{"neg_risk":false}`)
		case r.URL.Path == "/fee-rate":
			_, _ = io.WriteString(w, `{"base_fee":"0"}`)
		case r.URL.Path == "/auth/api-key":
			_, _ = io.WriteString(w, `{"apiKey":"`+freshKey+`","secret":"`+freshSecret+`","passphrase":"fresh-passphrase"}`)
		case r.URL.Path == "/order":
			orderCalls++
			if r.Header.Get("POLY_API_KEY") != freshKey {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = io.WriteString(w, `{"error":"Unauthorized/Invalid api key"}`)
				return
			}
			_, _ = io.WriteString(w, `{"orderID":"order-1"}`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client, err := NewClient(Config{
		Host:          server.URL,
		ChainID:       137,
		PrivateKey:    "4c0883a69102937d6231471b5dbb6204fe512961708279273d4f4f2b2d0f7e5f",
		APIKey:        staleKey,
		APISecret:     freshSecret,
		APIPassphrase: "stale-passphrase",
		FunderAddress: "0x90F8bf6A479f320ead074411a4B0e7944Ea8c9C1",
	}, logger)
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}

	orderID, normalizedSize, err := client.PlaceLimitOrder(
		t.Context(),
		"100",
		"BUY",
		0.81,
		10,
	)
	if err != nil {
		t.Fatalf("PlaceLimitOrder returned error: %v", err)
	}
	if orderID != "order-1" {
		t.Fatalf("unexpected order id %q", orderID)
	}
	if normalizedSize <= 0 {
		t.Fatalf("expected normalized size to be positive, got %.4f", normalizedSize)
	}
	if orderCalls != 2 {
		t.Fatalf("expected order endpoint to be called twice, got %d", orderCalls)
	}
}

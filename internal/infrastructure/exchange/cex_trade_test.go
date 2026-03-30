package exchange

import (
	"context"
	stdcrypto "crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestAsterLegacyHMACTradePlaceOrder_UsesLegacySignedEndpoint(t *testing.T) {
	t.Setenv("ASTER_API_KEY", "aster-key")
	t.Setenv("ASTER_API_SECRET", "aster-secret")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-MBX-APIKEY"); got != "aster-key" {
			t.Fatalf("unexpected api key header %q", got)
		}
		assertLegacyAsterSignature(t, r, "aster-secret")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/fapi/v2/positionRisk":
			_, _ = io.WriteString(w, `[{
				"symbol":"BTCUSDT",
				"positionSide":"BOTH"
			}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/fapi/v1/order":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body error = %v", err)
			}
			if len(body) != 0 {
				t.Fatalf("expected legacy signed order to send query params only, got body %q", string(body))
			}
			_, _ = io.WriteString(w, `{
				"orderId":"order-1",
				"clientOrderId":"cli-1",
				"status":"FILLED",
				"executedQty":"0.01",
				"avgPrice":"60000"
			}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewAsterTradeClient(ConfigSet{
		Aster: ExchangeConfig{
			Enabled:     true,
			RestBaseURL: server.URL,
			Auth: AuthConfig{
				APIKeyEnv:    "ASTER_API_KEY",
				APISecretEnv: "ASTER_API_SECRET",
			},
		},
	}, logger).(*CEXTradeClient)

	if client.authMode != binanceLikeTradeAuthLegacyHMAC {
		t.Fatalf("expected legacy auth mode, got %q", client.authMode)
	}
	result, err := client.PlaceOrder(context.Background(), TradeOrderRequest{
		CanonicalSymbol: "BTC",
		VenueSymbol:     "BTCUSDT",
		Side:            "BUY",
		OrderType:       "MARKET",
		Quantity:        0.01,
		ClientOrderID:   "cli-1",
	})
	if err != nil {
		t.Fatalf("PlaceOrder error = %v", err)
	}
	if result.VenueOrderID != "order-1" || result.ClientOrderID != "cli-1" || result.Status != "FILLED" {
		t.Fatalf("unexpected order result %#v", result)
	}
}

func TestAsterLegacyHMACTradeUsesLegacyAccountAndPositionEndpoints(t *testing.T) {
	t.Setenv("ASTER_API_KEY", "aster-key")
	t.Setenv("ASTER_API_SECRET", "aster-secret")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertLegacyAsterSignature(t, r, "aster-secret")
		switch r.URL.Path {
		case "/fapi/v2/positionRisk":
			_, _ = io.WriteString(w, `[{
				"symbol":"BTCUSDT",
				"positionAmt":"0.02",
				"entryPrice":"60100",
				"markPrice":"60050",
				"unRealizedProfit":"1.25"
			}]`)
		case "/fapi/v4/account":
			_, _ = io.WriteString(w, `{
				"totalMarginBalance":"1005",
				"availableBalance":"700",
				"totalInitialMargin":"300"
			}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewAsterTradeClient(ConfigSet{
		Aster: ExchangeConfig{
			Enabled:     true,
			RestBaseURL: server.URL,
			Auth: AuthConfig{
				APIKeyEnv:    "ASTER_API_KEY",
				APISecretEnv: "ASTER_API_SECRET",
			},
		},
	}, logger).(*CEXTradeClient)

	position, err := client.GetPosition(context.Background(), "BTC", "BTCUSDT", "")
	if err != nil {
		t.Fatalf("GetPosition error = %v", err)
	}
	if position.Quantity != 0.02 || position.EntryPrice != 60100 || position.MarkPrice != 60050 {
		t.Fatalf("unexpected position %#v", position)
	}
	account, err := client.GetAccountSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetAccountSnapshot error = %v", err)
	}
	if account.Equity != 1005 || account.AvailableBalance != 700 || account.MarginUsed != 300 {
		t.Fatalf("unexpected account snapshot %#v", account)
	}
}

func TestAsterV3TradePlaceOrder_UsesSignerAuth(t *testing.T) {
	const (
		accountAddress = "0x63DD5aCC6b1aa0f563956C0e534DD30B6dcF7C4e"
		privateKey     = "0x4fd0a42218f3eae43a6ce26d22544e986139a01e5b34a62db53757ffca81bae1"
	)

	t.Setenv("ASTER_ACCOUNT_ADDRESS", accountAddress)
	t.Setenv("ASTER_SIGNER_PRIVATE_KEY", privateKey)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var client *CEXTradeClient
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/fapi/v3/positionRisk":
			if got := r.Header.Get("X-MBX-APIKEY"); got != "" {
				t.Fatalf("expected no MBX api key header for v3 signer auth, got %q", got)
			}
			values := r.URL.Query()
			if got := values.Get("user"); got != accountAddress {
				t.Fatalf("unexpected user %q", got)
			}
			_, _ = io.WriteString(w, `[{
				"symbol":"BTCUSDT",
				"positionSide":"BOTH"
			}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/fapi/v3/order":
			if got := r.Header.Get("X-MBX-APIKEY"); got != "" {
				t.Fatalf("expected no MBX api key header for v3 signer auth, got %q", got)
			}
			if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/x-www-form-urlencoded") {
				t.Fatalf("unexpected content type %q", got)
			}

			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body error = %v", err)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatalf("parse body error = %v", err)
			}
			if got := values.Get("user"); got != accountAddress {
				t.Fatalf("unexpected user %q", got)
			}
			if values.Get("signer") == "" {
				t.Fatal("expected signer address in request body")
			}
			if values.Get("nonce") == "" {
				t.Fatal("expected nonce in request body")
			}

			signature := values.Get("signature")
			if signature == "" {
				t.Fatal("expected signature in request body")
			}
			values.Del("signature")
			expectedSignature, err := client.signAsterV3Payload(values.Encode())
			if err != nil {
				t.Fatalf("signAsterV3Payload error = %v", err)
			}
			if signature != expectedSignature {
				t.Fatalf("unexpected signature %q, expected %q", signature, expectedSignature)
			}

			_, _ = io.WriteString(w, `{
				"orderId":"order-v3",
				"clientOrderId":"cli-v3",
				"status":"NEW"
			}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client = NewAsterTradeClient(ConfigSet{
		Aster: ExchangeConfig{
			Enabled:     true,
			RestBaseURL: server.URL,
			Auth: AuthConfig{
				AccountAddressEnv: "ASTER_ACCOUNT_ADDRESS",
				PrivateKeyEnv:     "ASTER_SIGNER_PRIVATE_KEY",
			},
			AdapterOptions: map[string]string{
				"trade_auth_mode": asterTradeAuthV3Signer,
			},
		},
	}, logger).(*CEXTradeClient)

	if client.authMode != asterTradeAuthV3Signer {
		t.Fatalf("expected v3 signer auth mode, got %q", client.authMode)
	}
	result, err := client.PlaceOrder(context.Background(), TradeOrderRequest{
		CanonicalSymbol: "BTC",
		VenueSymbol:     "BTCUSDT",
		Side:            "BUY",
		OrderType:       "LIMIT",
		TimeInForce:     "GTC",
		Quantity:        0.01,
		Price:           60000,
		ClientOrderID:   "cli-v3",
	})
	if err != nil {
		t.Fatalf("PlaceOrder error = %v", err)
	}
	if result.VenueOrderID != "order-v3" || result.ClientOrderID != "cli-v3" || result.Status != "NEW" {
		t.Fatalf("unexpected order result %#v", result)
	}
}

func TestBinanceRSATradePlaceOrder_UsesRSASignedEndpoint(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey error = %v", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey error = %v", err)
	}
	privateKeyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})

	t.Setenv("BINANCE_API_KEY", "binance-rsa-key")
	t.Setenv("BINANCE_RSA_PRIVATE_KEY", string(privateKeyPEM))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-MBX-APIKEY"); got != "binance-rsa-key" {
			t.Fatalf("unexpected api key header %q", got)
		}
		assertRSASignature(t, r, &privateKey.PublicKey)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/fapi/v3/positionRisk":
			_, _ = io.WriteString(w, `[{
				"symbol":"BTCUSDT",
				"positionSide":"BOTH"
			}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/fapi/v1/order":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatalf("read body error = %v", err)
			}
			if len(body) != 0 {
				t.Fatalf("expected rsa signed order to send query params only, got body %q", string(body))
			}
			_, _ = io.WriteString(w, `{
				"orderId":"order-rsa",
				"clientOrderId":"cli-rsa",
				"status":"FILLED",
				"executedQty":"0.01",
				"avgPrice":"60000"
			}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBinanceTradeClient(ConfigSet{
		Binance: ExchangeConfig{
			Enabled:     true,
			RestBaseURL: server.URL,
			Auth: AuthConfig{
				APIKeyEnv:     "BINANCE_API_KEY",
				PrivateKeyEnv: "BINANCE_RSA_PRIVATE_KEY",
			},
			AdapterOptions: map[string]string{
				"trade_auth_mode": binanceLikeTradeAuthRSA,
			},
		},
	}, logger).(*CEXTradeClient)

	if client.authMode != binanceLikeTradeAuthRSA {
		t.Fatalf("expected rsa auth mode, got %q", client.authMode)
	}
	result, err := client.PlaceOrder(context.Background(), TradeOrderRequest{
		CanonicalSymbol: "BTC",
		VenueSymbol:     "BTCUSDT",
		Side:            "BUY",
		OrderType:       "MARKET",
		Quantity:        0.01,
		ClientOrderID:   "cli-rsa",
	})
	if err != nil {
		t.Fatalf("PlaceOrder error = %v", err)
	}
	if result.VenueOrderID != "order-rsa" || result.ClientOrderID != "cli-rsa" || result.Status != "FILLED" {
		t.Fatalf("unexpected order result %#v", result)
	}
}

func TestBinanceLikeTradePlaceOrder_503ReturnsUnknownExecutionOutcome(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "binance-key")
	t.Setenv("BINANCE_API_SECRET", "binance-secret")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/fapi/v3/positionRisk":
			_, _ = io.WriteString(w, `[{
				"symbol":"BTCUSDT",
				"positionSide":"BOTH"
			}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/fapi/v1/order":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"code":-1007,"msg":"execution status unknown"}`)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBinanceTradeClient(ConfigSet{
		Binance: ExchangeConfig{
			Enabled:     true,
			RestBaseURL: server.URL,
			Auth: AuthConfig{
				APIKeyEnv:    "BINANCE_API_KEY",
				APISecretEnv: "BINANCE_API_SECRET",
			},
		},
	}, logger).(*CEXTradeClient)

	_, err := client.PlaceOrder(context.Background(), TradeOrderRequest{
		CanonicalSymbol: "BTC",
		VenueSymbol:     "BTCUSDT",
		Side:            "BUY",
		OrderType:       "MARKET",
		Quantity:        0.01,
		ClientOrderID:   "cli-503",
	})
	if err == nil {
		t.Fatal("expected unknown execution outcome error")
	}
	var unknownErr *UnknownExecutionOutcomeError
	if !errors.As(err, &unknownErr) {
		t.Fatalf("expected UnknownExecutionOutcomeError, got %T: %v", err, err)
	}
	if unknownErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %#v", unknownErr)
	}
}

func TestBinanceTradePlaceOrder_RejectsHedgeModeAccounts(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "binance-key")
	t.Setenv("BINANCE_API_SECRET", "binance-secret")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	orderCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-MBX-APIKEY"); got != "binance-key" {
			t.Fatalf("unexpected api key header %q", got)
		}
		assertLegacyAsterSignature(t, r, "binance-secret")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/fapi/v3/positionRisk":
			// 使用 LONG/SHORT 双记录模拟官方文档里的 hedge mode 返回形状。
			_, _ = io.WriteString(w, `[{
				"symbol":"BTCUSDT",
				"positionSide":"LONG",
				"positionAmt":"0"
			},{
				"symbol":"BTCUSDT",
				"positionSide":"SHORT",
				"positionAmt":"0"
			}]`)
		case r.Method == http.MethodPost && r.URL.Path == "/fapi/v1/order":
			orderCalled = true
			t.Fatalf("order endpoint should not be called after hedge mode is detected")
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBinanceTradeClient(ConfigSet{
		Binance: ExchangeConfig{
			Enabled:     true,
			RestBaseURL: server.URL,
			Auth: AuthConfig{
				APIKeyEnv:    "BINANCE_API_KEY",
				APISecretEnv: "BINANCE_API_SECRET",
			},
		},
	}, logger).(*CEXTradeClient)

	_, err := client.PlaceOrder(context.Background(), TradeOrderRequest{
		CanonicalSymbol: "BTC",
		VenueSymbol:     "BTCUSDT",
		Side:            "BUY",
		OrderType:       "MARKET",
		Quantity:        0.01,
		ClientOrderID:   "cli-hedge",
	})
	if err == nil {
		t.Fatal("expected hedge mode guard to reject the order")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "hedge mode") {
		t.Fatalf("expected hedge mode error, got %v", err)
	}
	if orderCalled {
		t.Fatal("expected order endpoint to remain untouched")
	}
}

func TestBinanceTradeUsesV3AccountAndPositionEndpoints(t *testing.T) {
	t.Setenv("BINANCE_API_KEY", "binance-key")
	t.Setenv("BINANCE_API_SECRET", "binance-secret")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-MBX-APIKEY"); got != "binance-key" {
			t.Fatalf("unexpected api key header %q", got)
		}
		assertLegacyAsterSignature(t, r, "binance-secret")
		switch r.URL.Path {
		case "/fapi/v3/positionRisk":
			_, _ = io.WriteString(w, `[{
				"symbol":"BTCUSDT",
				"positionAmt":"0.02",
				"entryPrice":"60100",
				"markPrice":"60050",
				"unRealizedProfit":"1.25"
			}]`)
		case "/fapi/v3/account":
			_, _ = io.WriteString(w, `{
				"totalMarginBalance":"1005",
				"availableBalance":"700",
				"totalInitialMargin":"300"
			}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBinanceTradeClient(ConfigSet{
		Binance: ExchangeConfig{
			Enabled:     true,
			RestBaseURL: server.URL,
			Auth: AuthConfig{
				APIKeyEnv:    "BINANCE_API_KEY",
				APISecretEnv: "BINANCE_API_SECRET",
			},
		},
	}, logger).(*CEXTradeClient)

	position, err := client.GetPosition(context.Background(), "BTC", "BTCUSDT", "")
	if err != nil {
		t.Fatalf("GetPosition error = %v", err)
	}
	if position.Quantity != 0.02 || position.EntryPrice != 60100 || position.MarkPrice != 60050 {
		t.Fatalf("unexpected position %#v", position)
	}
	account, err := client.GetAccountSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetAccountSnapshot error = %v", err)
	}
	if account.Equity != 1005 || account.AvailableBalance != 700 || account.MarginUsed != 300 {
		t.Fatalf("unexpected account snapshot %#v", account)
	}
}

func assertLegacyAsterSignature(t *testing.T, r *http.Request, secret string) {
	t.Helper()
	if r.URL.RawQuery == "" {
		t.Fatal("expected signed query string")
	}
	index := strings.LastIndex(r.URL.RawQuery, "&signature=")
	if index < 0 {
		t.Fatalf("expected signature in query string, got %q", r.URL.RawQuery)
	}
	payload := r.URL.RawQuery[:index]
	signature := r.URL.RawQuery[index+len("&signature="):]
	if got := hmacHex(secret, payload); got != signature {
		t.Fatalf("unexpected signature %q, expected %q", signature, got)
	}
}

func assertRSASignature(t *testing.T, r *http.Request, publicKey *rsa.PublicKey) {
	t.Helper()
	if r.URL.RawQuery == "" {
		t.Fatal("expected signed query string")
	}
	index := strings.LastIndex(r.URL.RawQuery, "&signature=")
	if index < 0 {
		t.Fatalf("expected signature in query string, got %q", r.URL.RawQuery)
	}
	payload := r.URL.RawQuery[:index]
	signature := r.URL.Query().Get("signature")
	if signature == "" {
		t.Fatal("expected decoded signature value in query string")
	}
	signatureBytes, err := base64.StdEncoding.DecodeString(signature)
	if err != nil {
		t.Fatalf("DecodeString error = %v", err)
	}
	digest := sha256.Sum256([]byte(payload))
	if err := rsa.VerifyPKCS1v15(publicKey, stdcrypto.SHA256, digest[:], signatureBytes); err != nil {
		t.Fatalf("VerifyPKCS1v15 error = %v", err)
	}
}

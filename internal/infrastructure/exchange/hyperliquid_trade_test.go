package exchange

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	testHyperliquidAccountAddress = "0x63dd5acc6b1aa0f563956c0e534dd30b6dcf7c4e"
	testHyperliquidPrivateKey     = "0x4fd0a42218f3eae43a6ce26d22544e986139a01e5b34a62db53757ffca81bae1"
	testHyperliquidVaultAddress   = "0x1111111111111111111111111111111111111111"
	testHyperliquidCloid          = "0x1234567890abcdef1234567890abcdef"
)

func TestHyperliquidTradeGetOrderStatus_UsesOIDForCloidAndVaultAddress(t *testing.T) {
	t.Setenv("HL_ACCOUNT_ADDRESS", testHyperliquidAccountAddress)
	t.Setenv("HL_PRIVATE_KEY", testHyperliquidPrivateKey)
	t.Setenv("HL_VAULT_ADDRESS", testHyperliquidVaultAddress)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/info" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body error = %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body error = %v", err)
		}
		if payload["type"] != "orderStatus" {
			t.Fatalf("expected orderStatus request, got %#v", payload["type"])
		}
		if payload["user"] != testHyperliquidVaultAddress {
			t.Fatalf("expected vault address for info query, got %#v", payload["user"])
		}
		if payload["oid"] != testHyperliquidCloid {
			t.Fatalf("expected client cloid to be sent via oid field, got %#v", payload["oid"])
		}
		_, _ = io.WriteString(w, `{
			"status":"filled",
			"oid":"12345",
			"order":{
				"status":"filled",
				"filled":"0.01",
				"avgPx":"60000"
			}
		}`)
	}))
	defer server.Close()

	client := NewHyperliquidTradeAdapter("hyperliquid", ExchangeConfig{
		Enabled:     true,
		RestBaseURL: server.URL,
		Auth: AuthConfig{
			AccountAddressEnv: "HL_ACCOUNT_ADDRESS",
			PrivateKeyEnv:     "HL_PRIVATE_KEY",
			VaultAddressEnv:   "HL_VAULT_ADDRESS",
		},
	}, logger).(*HyperliquidTradeClient)

	status, err := client.GetOrderStatus(context.Background(), OrderLookupRequest{
		ClientOrderID: testHyperliquidCloid,
	})
	if err != nil {
		t.Fatalf("GetOrderStatus error = %v", err)
	}
	if status.Status != "FILLED" || status.ExecutedQty != 0.01 || status.AveragePrice != 60000 {
		t.Fatalf("unexpected order status %#v", status)
	}
	if status.VenueOrderID != "12345" || !status.Terminal {
		t.Fatalf("expected terminal filled venue order id, got %#v", status)
	}
}

func TestHyperliquidTradePlaceOrder_RejectsBusinessErrorResponse(t *testing.T) {
	t.Setenv("HL_ACCOUNT_ADDRESS", testHyperliquidAccountAddress)
	t.Setenv("HL_PRIVATE_KEY", testHyperliquidPrivateKey)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exchange" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body error = %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body error = %v", err)
		}
		action, _ := payload["action"].(map[string]any)
		orders, _ := action["orders"].([]any)
		firstOrder, _ := orders[0].(map[string]any)
		if firstOrder["c"] != testHyperliquidCloid {
			t.Fatalf("expected cloid to be forwarded, got %#v", firstOrder["c"])
		}
		_, _ = io.WriteString(w, `{
			"status":"ok",
			"response":{
				"type":"order",
				"data":{
					"statuses":[{"error":"bad cloid"}]
				}
			}
		}`)
	}))
	defer server.Close()

	client := NewHyperliquidTradeAdapter("hyperliquid", ExchangeConfig{
		Enabled:     true,
		RestBaseURL: server.URL,
		Auth: AuthConfig{
			AccountAddressEnv: "HL_ACCOUNT_ADDRESS",
			PrivateKeyEnv:     "HL_PRIVATE_KEY",
		},
	}, logger).(*HyperliquidTradeClient)

	_, err := client.PlaceOrder(context.Background(), TradeOrderRequest{
		CanonicalSymbol: "BTC",
		VenueSymbol:     "BTC",
		AssetID:         "5",
		Side:            "BUY",
		OrderType:       "LIMIT",
		TimeInForce:     "IOC",
		Quantity:        0.01,
		Price:           60000,
		ClientOrderID:   testHyperliquidCloid,
	})
	if err == nil {
		t.Fatal("expected business error response to fail place order")
	}
}

func TestHyperliquidTradeCancelOrder_ByCloid(t *testing.T) {
	t.Setenv("HL_ACCOUNT_ADDRESS", testHyperliquidAccountAddress)
	t.Setenv("HL_PRIVATE_KEY", testHyperliquidPrivateKey)
	t.Setenv("HL_VAULT_ADDRESS", testHyperliquidVaultAddress)

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exchange" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body error = %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body error = %v", err)
		}
		if payload["vaultAddress"] != testHyperliquidVaultAddress {
			t.Fatalf("expected vaultAddress in cancel payload, got %#v", payload["vaultAddress"])
		}
		action, _ := payload["action"].(map[string]any)
		if action["type"] != "cancelByCloid" {
			t.Fatalf("expected cancelByCloid action, got %#v", action["type"])
		}
		cancels, _ := action["cancels"].([]any)
		firstCancel, _ := cancels[0].(map[string]any)
		if firstCancel["asset"] != float64(5) {
			t.Fatalf("expected asset=5, got %#v", firstCancel["asset"])
		}
		if firstCancel["cloid"] != testHyperliquidCloid {
			t.Fatalf("expected cloid in cancel payload, got %#v", firstCancel["cloid"])
		}
		_, _ = io.WriteString(w, `{"status":"ok","response":{"type":"cancel"}}`)
	}))
	defer server.Close()

	client := NewHyperliquidTradeAdapter("hyperliquid", ExchangeConfig{
		Enabled:     true,
		RestBaseURL: server.URL,
		Auth: AuthConfig{
			AccountAddressEnv: "HL_ACCOUNT_ADDRESS",
			PrivateKeyEnv:     "HL_PRIVATE_KEY",
			VaultAddressEnv:   "HL_VAULT_ADDRESS",
		},
	}, logger).(*HyperliquidTradeClient)

	if err := client.CancelOrder(context.Background(), OrderLookupRequest{
		AssetID:       "5",
		ClientOrderID: testHyperliquidCloid,
	}); err != nil {
		t.Fatalf("CancelOrder error = %v", err)
	}
}

package polymarket

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
)

// TestRedeemConditionUsesProxyRelayer 确认代理钱包兑奖会改走 relayer，而不是继续要求 signer=funder。
func TestRedeemConditionUsesProxyRelayer(t *testing.T) {
	privateKeyHex := "59c6995e998f97a5a0044976f5d1b0f5d5a9b6715f7e5f1f1b72e38fcb0a6b15"
	privateKey, err := crypto.HexToECDSA(privateKeyHex)
	if err != nil {
		t.Fatalf("failed to parse private key: %v", err)
	}
	signer := crypto.PubkeyToAddress(privateKey.PublicKey)
	proxyWallet := deriveProxyWalletAddress(signer)

	var capturedRequest relayerTransactionRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/relay-payload":
			_ = json.NewEncoder(w).Encode(relayerPayloadResponse{
				Address: "0x4da9395388791c22684e03779c3de10934eb9cfb",
				Nonce:   "31",
			})
		case r.Method == http.MethodPost && r.URL.Path == "/submit":
			if got := r.Header.Get("RELAYER_API_KEY"); got != "relay-key-1" {
				t.Fatalf("unexpected relayer api key header %q", got)
			}
			if got := strings.ToLower(r.Header.Get("RELAYER_API_KEY_ADDRESS")); got != strings.ToLower(signer.Hex()) {
				t.Fatalf("unexpected relayer api key address %q", got)
			}
			if err := json.NewDecoder(r.Body).Decode(&capturedRequest); err != nil {
				t.Fatalf("failed to decode submit body: %v", err)
			}
			_ = json.NewEncoder(w).Encode(relayerSubmitResponse{
				TransactionID:   "txn-1",
				TransactionHash: "",
				State:           "STATE_NEW",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/transaction":
			_ = json.NewEncoder(w).Encode([]relayerTransaction{{
				TransactionID:   "txn-1",
				TransactionHash: "0xabc123",
				ProxyAddress:    proxyWallet.Hex(),
				State:           relayerStateConfirmed,
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := Config{
		RelayerURL:     server.URL,
		PrivateKey:     "0x" + privateKeyHex,
		FunderAddress:  proxyWallet.Hex(),
		SignatureType:  1,
		RelayerAPIKey:  "relay-key-1",
		ChainID:        137,
		CTFContract:    "0x4d97dcd97ec945f40cf65f87097ace5ea0476045",
		USDCCollateral: "0x2791bca1f2de4661ed88a30c99a7a9449aa84174",
	}

	client, err := NewClient(cfg, nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	result, err := client.RedeemCondition(context.Background(), "0x1111111111111111111111111111111111111111111111111111111111111111")
	if err != nil {
		t.Fatalf("expected proxy relayer redeem to succeed, got %v", err)
	}
	if result.TransactionID != "txn-1" || result.TxHash != "0xabc123" {
		t.Fatalf("unexpected redeem result: %+v", result)
	}
	if capturedRequest.Type != relayerTxTypeProxy {
		t.Fatalf("expected proxy relayer request, got %+v", capturedRequest)
	}
	if strings.ToLower(capturedRequest.ProxyWallet) != strings.ToLower(proxyWallet.Hex()) {
		t.Fatalf("unexpected proxy wallet %q", capturedRequest.ProxyWallet)
	}
	if capturedRequest.SignatureParams.RelayHub != proxyRelayHubAddress {
		t.Fatalf("unexpected relay hub %q", capturedRequest.SignatureParams.RelayHub)
	}
	if capturedRequest.Nonce != "31" {
		t.Fatalf("unexpected nonce %q", capturedRequest.Nonce)
	}
}

// TestValidateExpectedProxyWalletRejectsMismatch 确认 signer 与配置的 proxy 地址不一致时会给出明确错误。
func TestValidateExpectedProxyWalletRejectsMismatch(t *testing.T) {
	privateKey, err := crypto.HexToECDSA("8b3a350cf5c34c9194ca3a545d7c87f3d6a37df4b2275f1c6c3d4dcd3f3e3c20")
	if err != nil {
		t.Fatalf("failed to parse private key: %v", err)
	}
	client := &Client{
		cfg: Config{
			FunderAddress: "0x1771ce21dD09805cCD43f7851f7089f0b085974d",
		},
		privateKey: privateKey,
		address:    crypto.PubkeyToAddress(privateKey.PublicKey),
	}

	if err := client.validateExpectedProxyWallet(deriveProxyWalletAddress(client.address)); err == nil {
		t.Fatalf("expected mismatched proxy wallet to be rejected")
	}
}

// TestGetRedeemableConditionsSkipsDustAndMergeable 确认自动兑奖扫描不会把尘埃仓位和 merge-only 仓位混进去。
func TestGetRedeemableConditionsSkipsDustAndMergeable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"conditionId": "0x1111111111111111111111111111111111111111111111111111111111111111",
				"size":        0.50,
				"redeemable":  true,
			},
			{
				"conditionId": "0x2222222222222222222222222222222222222222222222222222222222222222",
				"size":        0.005,
				"redeemable":  true,
			},
			{
				"conditionId": "0x3333333333333333333333333333333333333333333333333333333333333333",
				"size":        1.25,
				"mergeable":   true,
			},
		})
	}))
	defer server.Close()

	client, err := NewClient(Config{
		DataAPI:           server.URL,
		AutoRedeemMinSize: 0.01,
		SignatureType:     1,
		RelayerAPIKey:     "relay-key-1",
		PrivateKey:        "0x59c6995e998f97a5a0044976f5d1b0f5d5a9b6715f7e5f1f1b72e38fcb0a6b15",
		FunderAddress:     "0x1771ce21dD09805cCD43f7851f7089f0b085974d",
		CTFContract:       "0x4d97dcd97ec945f40cf65f87097ace5ea0476045",
		USDCCollateral:    "0x2791bca1f2de4661ed88a30c99a7a9449aa84174",
		RelayerURL:        "https://relayer-v2.polymarket.com",
	}, nil)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	claimable, pendingCount, err := client.GetRedeemableConditions(context.Background(), "0x1771ce21dD09805cCD43f7851f7089f0b085974d")
	if err != nil {
		t.Fatalf("expected scan to succeed, got %v", err)
	}
	if pendingCount != 1 {
		t.Fatalf("expected only one runnable redeem target, got %d", pendingCount)
	}
	if len(claimable) != 1 || claimable[0] != "0x1111111111111111111111111111111111111111111111111111111111111111" {
		t.Fatalf("unexpected claimable conditions: %+v", claimable)
	}
}

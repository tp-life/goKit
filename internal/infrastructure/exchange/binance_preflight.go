package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type BinanceSignedCheck struct {
	Name         string         `json:"name"`
	Method       string         `json:"method"`
	URL          string         `json:"url"`
	HTTPStatus   int            `json:"http_status"`
	OK           bool           `json:"ok"`
	Latency      string         `json:"latency"`
	ErrorCode    *int64         `json:"error_code,omitempty"`
	ErrorMessage string         `json:"error_message,omitempty"`
	ErrorBody    string         `json:"error_body,omitempty"`
	Summary      map[string]any `json:"summary,omitempty"`
}

type BinancePreflightResult struct {
	Exchange          string              `json:"exchange"`
	AuthMode          string              `json:"auth_mode"`
	AccountMode       string              `json:"account_mode"`
	KeyPresent        bool                `json:"key_present"`
	SecretPresent     bool                `json:"secret_present"`
	SecretShape       map[string]any      `json:"secret_shape,omitempty"`
	PrivateKeyPresent bool                `json:"private_key_present"`
	PrivateKeyShape   map[string]any      `json:"private_key_shape,omitempty"`
	EgressProbe       *NetworkProbe       `json:"egress_probe,omitempty"`
	EgressProbeErr    string              `json:"egress_probe_error,omitempty"`
	SpotAccount       BinanceSignedCheck  `json:"spot_account"`
	Restrictions      *BinanceSignedCheck `json:"restrictions,omitempty"`
	FuturesAccount    BinanceSignedCheck  `json:"futures_account"`
	LikelyCause       string              `json:"likely_cause"`
	Hints             []string            `json:"hints,omitempty"`
}

func RunBinancePreflight(ctx context.Context, exchangeName string, cfg ExchangeConfig, logger *slog.Logger) BinancePreflightResult {
	cfg = normalizeExchangeConfig(exchangeName, cfg)
	authMode := tradeAuthMode(exchangeName, cfg)
	accountMode := binanceLikeAccountMode(exchangeName, cfg)
	tradeBaseURL := resolveBinanceLikeTradeRESTBaseURL(exchangeName, cfg)
	routes := resolveBinanceLikePrivateRoutes(exchangeName, cfg, authMode)
	apiKey, apiSecret := readCredentialPair(cfg.Auth.APIKeyEnv, cfg.Auth.APISecretEnv)
	_, privateKey := readCredentialPair(cfg.Auth.APIKeyEnv, cfg.Auth.PrivateKeyEnv)
	result := BinancePreflightResult{
		Exchange:          normalizeExchangeName(exchangeName),
		AuthMode:          authMode,
		AccountMode:       accountMode,
		KeyPresent:        strings.TrimSpace(apiKey) != "",
		SecretPresent:     strings.TrimSpace(apiSecret) != "",
		SecretShape:       inspectSecretShape(apiSecret),
		PrivateKeyPresent: strings.TrimSpace(privateKey) != "",
		PrivateKeyShape:   inspectPrivateKeyShape(privateKey),
	}

	if probe, err := DetectTradeEgressIP(ctx, exchangeName, cfg, logger); err != nil {
		result.EgressProbe = &probe
		result.EgressProbeErr = err.Error()
	} else {
		result.EgressProbe = &probe
	}

	client := newHTTPClient(cfg, loadAppConfig(), logger, exchangeName+"-binance-preflight")
	result.SpotAccount = binanceSignedCheck(ctx, client, cfg, authMode, "spot_account", "https://api.binance.com", "/api/v3/account")
	if result.SpotAccount.OK {
		restrictions := binanceSignedCheck(ctx, client, cfg, authMode, "api_restrictions", "https://api.binance.com", "/sapi/v1/account/apiRestrictions")
		result.Restrictions = &restrictions
	}
	result.FuturesAccount = binanceSignedCheck(ctx, client, cfg, authMode, "futures_account", strings.TrimRight(tradeBaseURL, "/"), routes.accountPath)
	result.LikelyCause, result.Hints = classifyBinancePreflight(result)
	return result
}

func binanceSignedCheck(ctx context.Context, client *http.Client, cfg ExchangeConfig, authMode, name, baseURL, path string) BinanceSignedCheck {
	check := BinanceSignedCheck{
		Name:   name,
		Method: http.MethodGet,
		URL:    strings.TrimRight(baseURL, "/") + path,
	}
	apiKey, apiSecret := readCredentialPair(cfg.Auth.APIKeyEnv, cfg.Auth.APISecretEnv)
	if strings.TrimSpace(apiKey) == "" {
		check.ErrorMessage = "missing api key"
		return check
	}

	params := url.Values{}
	params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	payload := params.Encode()
	var (
		signature string
		err       error
	)
	switch authMode {
	case binanceLikeTradeAuthRSA:
		privateKey := loadRSASigner(cfg.Auth)
		if privateKey == nil {
			check.ErrorMessage = "missing or invalid rsa private key"
			return check
		}
		signature, err = signRSAPayload(privateKey, payload)
		if err == nil {
			signature = url.QueryEscape(signature)
		}
	default:
		if strings.TrimSpace(apiSecret) == "" {
			check.ErrorMessage = "missing api secret"
			return check
		}
		signature = signLegacyHMACPayload(apiSecret, payload)
	}
	if err != nil {
		check.ErrorMessage = err.Error()
		return check
	}

	reqURL := check.URL + "?" + payload + "&signature=" + signature
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		check.ErrorMessage = err.Error()
		return check
	}
	req.Header.Set("X-MBX-APIKEY", apiKey)

	begin := time.Now()
	resp, err := client.Do(req)
	check.Latency = time.Since(begin).String()
	if err != nil {
		check.ErrorMessage = err.Error()
		return check
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		check.ErrorMessage = readErr.Error()
		return check
	}
	check.HTTPStatus = resp.StatusCode

	if resp.StatusCode >= 300 {
		check.ErrorBody = string(body)
		var envelope map[string]any
		if err := json.Unmarshal(body, &envelope); err == nil {
			if code, ok := numericCode(envelope["code"]); ok {
				check.ErrorCode = &code
			}
			check.ErrorMessage = asString(envelope["msg"])
		}
		if check.ErrorMessage == "" {
			check.ErrorMessage = strings.TrimSpace(string(body))
		}
		return check
	}

	check.OK = true
	if name == "api_restrictions" {
		var summary map[string]any
		if err := json.Unmarshal(body, &summary); err == nil {
			check.Summary = summary
		}
	} else {
		check.Summary = map[string]any{"body_present": len(body) > 0}
	}
	return check
}

func classifyBinancePreflight(result BinancePreflightResult) (string, []string) {
	tradingScope := "futures"
	expectedHost := "fapi.binance.com"
	if result.AccountMode == binanceLikeAccountModePortfolioMargin {
		tradingScope = "portfolio margin"
		expectedHost = "papi.binance.com"
	}

	if result.AuthMode == binanceLikeTradeAuthRSA {
		if !result.KeyPresent || !result.PrivateKeyPresent {
			return "missing Binance API key or RSA private key", []string{
				"confirm BINANCE_API_KEY and BINANCE_RSA_PRIVATE_KEY are loaded from .env",
				"if you intended to keep using HMAC, switch trade_auth_mode back to legacy_hmac and provide BINANCE_API_SECRET",
				"rerun `go run ./cmd/tradeprobe --action preflight --exchange binance` after fixing env values",
			}
		}
	} else if !result.KeyPresent || !result.SecretPresent {
		hints := []string{
			"confirm BINANCE_API_KEY and BINANCE_API_SECRET are loaded from .env",
			"rerun `go run ./cmd/tradeprobe --action preflight --exchange binance` after fixing env values",
		}
		if result.PrivateKeyPresent {
			hints = append(hints, "if this key was created as an RSA key, set trade_auth_mode to rsa so the client uses private_key_env instead of api_secret_env")
		}
		return "missing Binance API key or secret", hints
	}

	if !result.SpotAccount.OK {
		if result.SpotAccount.ErrorCode != nil {
			switch *result.SpotAccount.ErrorCode {
			case -1022:
				if result.AuthMode == binanceLikeTradeAuthRSA {
					return "Spot RSA signature is invalid. The current private key most likely does not match BINANCE_API_KEY.", []string{
						"confirm the Binance API key was created from the matching RSA public key",
						"confirm the private key is the correct PKCS#8 PEM content and was pasted without truncation",
						"if you actually meant to use HMAC, switch trade_auth_mode back to legacy_hmac and provide the matching BINANCE_API_SECRET",
					}
				}
				hints := []string{
					"regenerate a fresh HMAC key/secret pair in Binance API Management",
					"replace both BINANCE_API_KEY and BINANCE_API_SECRET together; do not mix an old secret with a new key",
				}
				if result.PrivateKeyPresent {
					hints = append(hints, "if this Binance key is RSA-based, set trade_auth_mode to rsa so the client uses private_key_env instead of api_secret_env")
				}
				return "Spot HMAC signature is invalid. The current BINANCE_API_SECRET most likely does not match BINANCE_API_KEY.", hints
			case -2015:
				return "Spot rejected the key before futures permission checks. This is usually an invalid key, wrong environment, or missing read permission.", []string{
					"confirm this is a Binance mainnet key, not a testnet key",
					"confirm the key still exists and has read access enabled",
					"confirm the whitelist includes the detected egress IP",
				}
			}
		}
		return "Spot signed request failed before futures diagnostics could pass.", []string{
			"fix the spot auth error first; futures cannot be trusted until spot signed requests succeed",
		}
	}

	if !result.FuturesAccount.OK {
		if result.FuturesAccount.ErrorCode != nil && *result.FuturesAccount.ErrorCode == -2015 {
			if result.Restrictions != nil && result.Restrictions.OK {
				if enabled, ok := boolField(result.Restrictions.Summary, "enableFutures"); ok && !enabled {
					return "Spot auth works, but the Binance key does not have Futures permission enabled.", []string{
						"open Binance API Management and enable Futures for this key",
						"after enabling Futures, wait a moment and rerun preflight",
					}
				}
			}
			return fmt.Sprintf("Spot auth works, but the Binance %s account endpoint still rejects this key.", tradingScope), []string{
				"confirm Futures permission is enabled for this key",
				"confirm the account itself is eligible for USD-M Futures in this region",
				"if this account uses Portfolio Margin / Unified Account, confirm Portfolio Margin is already enabled on the Binance account",
				fmt.Sprintf("confirm you are calling the correct environment: mainnet key with %s", expectedHost),
			}
		}
		return "Spot auth works, but futures preflight still failed with a non-standard error.", []string{
			"inspect futures_account.error_code and futures_account.error_message in the preflight output",
		}
	}

	return fmt.Sprintf("Binance spot and %s signed requests both succeeded.", tradingScope), []string{
		"the key, secret, egress IP, and futures permission all look healthy from this machine",
	}
}

func inspectSecretShape(secret string) map[string]any {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil
	}
	return map[string]any{
		"length":           len(secret),
		"contains_newline": strings.Contains(secret, "\n"),
		"looks_pem":        strings.HasPrefix(secret, "-----BEGIN"),
	}
}

func inspectPrivateKeyShape(privateKey string) map[string]any {
	privateKey = normalizePrivateKeyPEM(privateKey)
	if privateKey == "" {
		return nil
	}
	return map[string]any{
		"length":              len(privateKey),
		"contains_newline":    strings.Contains(privateKey, "\n"),
		"looks_pem":           strings.HasPrefix(privateKey, "-----BEGIN"),
		"looks_pkcs8_private": strings.Contains(privateKey, "BEGIN PRIVATE KEY"),
		"looks_pkcs1_rsa":     strings.Contains(privateKey, "BEGIN RSA PRIVATE KEY"),
	}
}

func numericCode(v any) (int64, bool) {
	switch value := v.(type) {
	case float64:
		return int64(value), true
	case int64:
		return value, true
	case int:
		return int64(value), true
	default:
		return 0, false
	}
}

func boolField(values map[string]any, key string) (bool, bool) {
	if len(values) == 0 {
		return false, false
	}
	raw, ok := values[key]
	if !ok {
		return false, false
	}
	value, ok := raw.(bool)
	return value, ok
}

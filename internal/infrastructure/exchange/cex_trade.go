package exchange

import (
	"context"
	stdcrypto "crypto"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/gorilla/websocket"
)

const (
	binanceLikeTradeAuthLegacyHMAC = "legacy_hmac"
	binanceLikeTradeAuthRSA        = "rsa"
	asterTradeAuthV3Signer         = "v3_signer"
)

// CEXTradeClient 是保留了历史命名的实现类型。
//
// 它的真实职责不是“适配任意中心化交易所”，而是：
// 适配 Binance / Aster 这类 Binance-like 永续合约私有交易协议。
//
// 这层纠偏非常重要，因为后续如果继续接入 OKX / Bybit / Bitget，
// 我们应该先判断它们是否真的属于同一协议族，而不是被 `CEX` 这个宽泛名字误导。
type CEXTradeClient struct {
	name          string
	cfg           ExchangeConfig
	logger        *slog.Logger
	httpClient    *http.Client
	wsDialer      *websocket.Dialer
	apiKey        string
	apiSecret     string
	rsaPrivateKey *rsa.PrivateKey
	privateKey    *ecdsa.PrivateKey
	accountAddr   string
	signerAddr    string
	authMode      string
	orderPath     string
	positionPath  string
	accountPath   string
	nonceMu       sync.Mutex
	lastNonce     int64
}

func NewBinanceTradeClient(cfg ConfigSet, logger *slog.Logger) TradeAdapter {
	return NewCEXTradeAdapter("binance", cfg.Binance, logger)
}

func NewAsterTradeClient(cfg ConfigSet, logger *slog.Logger) TradeAdapter {
	return NewCEXTradeAdapter("aster", cfg.Aster, logger)
}

// NewBinanceLikeTradeAdapter 是 registry 层应该优先使用的交易适配器构造入口。
//
// 它的命名刻意强调：这份实现只覆盖 Binance Futures 风格的私有交易协议，
// 例如 API Key + HMAC 签名、`/fapi/...` 路径、Binance 风格字段名等。
//
// 这能帮助后续接入新交易所时更快看清边界：
// - 如果新交易所真的是 Binance-like，可以复用这份实现；
// - 如果不是，就应该新增新的协议族适配器，而不是继续往这里堆特殊分支。
func NewBinanceLikeTradeAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) TradeAdapter {
	return NewCEXTradeAdapter(name, cfg, logger)
}

// NewCEXTradeAdapter 是保留给现有代码的兼容构造入口。
//
// 名字虽然还是 `CEX`，但不要把它理解成“所有 CEX 通用适配器”；
// 它现在只代表 `binance_like` 协议族。
func NewCEXTradeAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) TradeAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		switch name {
		case "aster":
			c.RestBaseURL = "https://fapi.asterdex.com"
		default:
			c.RestBaseURL = "https://fapi.binance.com"
		}
	}
	if c.PublicWSBaseURL == "" {
		switch name {
		case "aster":
			c.PublicWSBaseURL = "wss://fstream.asterdex.com"
		default:
			c.PublicWSBaseURL = "wss://fstream.binance.com"
		}
	}
	if c.PrivateWSBaseURL == "" {
		c.PrivateWSBaseURL = c.PublicWSBaseURL
	}
	appCfg := loadAppConfig()
	orderPath := "/fapi/v1/order"
	positionPath := "/fapi/v2/positionRisk"
	accountPath := "/fapi/v2/account"
	authMode := tradeAuthMode(name, c)
	if name == "binance" {
		positionPath = "/fapi/v3/positionRisk"
		accountPath = "/fapi/v3/account"
	}
	if name == "aster" && authMode == asterTradeAuthV3Signer {
		orderPath = "/fapi/v3/order"
		positionPath = "/fapi/v3/positionRisk"
		accountPath = "/fapi/v3/account"
	} else if name == "aster" {
		accountPath = "/fapi/v4/account"
	}
	pk, signerAddr := loadAsterSigner(c.Auth)
	rsaPK := loadRSASigner(c.Auth)
	apiKey, apiSecret := readCredentialPair(c.Auth.APIKeyEnv, c.Auth.APISecretEnv)
	accountAddr, _ := readCredentialPair(c.Auth.AccountAddressEnv, c.Auth.PrivateKeyEnv)
	client := &CEXTradeClient{
		name:          name,
		cfg:           c,
		logger:        logger,
		httpClient:    newHTTPClient(c, appCfg, logger, name+"-trade"),
		wsDialer:      newWebSocketDialer(c, appCfg, logger, name+"-trade"),
		apiKey:        apiKey,
		apiSecret:     apiSecret,
		rsaPrivateKey: rsaPK,
		privateKey:    pk,
		accountAddr:   strings.TrimSpace(accountAddr),
		signerAddr:    signerAddr,
		authMode:      authMode,
		orderPath:     orderPath,
		positionPath:  positionPath,
		accountPath:   accountPath,
	}
	return client
}

func (c *CEXTradeClient) Name() string { return c.name }
func (c *CEXTradeClient) Enabled() bool {
	if !c.cfg.Enabled {
		return false
	}
	switch c.authMode {
	case asterTradeAuthV3Signer:
		return c.privateKey != nil && strings.TrimSpace(c.accountAddr) != "" && strings.TrimSpace(c.signerAddr) != ""
	case binanceLikeTradeAuthRSA:
		return strings.TrimSpace(c.apiKey) != "" && c.rsaPrivateKey != nil
	}
	return c.apiKey != "" && c.apiSecret != ""
}
func (c *CEXTradeClient) Capabilities() TradeCapabilities {
	return TradeCapabilities{
		MakerLimitTIF:            "GTX",
		TakerOrderType:           "MARKET",
		TakerUsesAggressiveIOC:   false,
		SupportsOrderEventStream: c.supportsOrderEventStream(),
	}
}

func (c *CEXTradeClient) PlaceOrder(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	if !c.Enabled() {
		return TradeOrderResult{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("symbol", req.VenueSymbol)
	params.Set("side", strings.ToUpper(req.Side))
	params.Set("quantity", formatFloat(req.Quantity, 8))
	params.Set("newOrderRespType", "RESULT")
	if c.authMode != asterTradeAuthV3Signer {
		params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	}
	if req.ClientOrderID != "" {
		params.Set("newClientOrderId", req.ClientOrderID)
	}
	orderType := strings.ToUpper(req.OrderType)
	if orderType == "" {
		orderType = "MARKET"
	}
	params.Set("type", orderType)
	if orderType == "LIMIT" {
		params.Set("price", formatFloat(req.Price, 8))
		tif := strings.ToUpper(strings.TrimSpace(req.TimeInForce))
		if tif == "" {
			tif = "GTC"
		}
		params.Set("timeInForce", tif)
	}
	if req.ReduceOnly {
		params.Set("reduceOnly", "true")
	}
	var respPayload map[string]any
	raw, err := c.signedPOST(ctx, c.orderPath, params, &respPayload)
	if err != nil {
		return TradeOrderResult{}, err
	}
	return TradeOrderResult{
		Exchange:        c.name,
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		ClientOrderID:   asString(respPayload["clientOrderId"]),
		VenueOrderID:    asString(respPayload["orderId"]),
		Status:          asString(respPayload["status"]),
		ExecutedQty:     parseNullableFloat(respPayload["executedQty"]),
		AveragePrice:    parseNullableFloat(respPayload["avgPrice"]),
		RawResponse:     raw,
	}, nil
}

func (c *CEXTradeClient) ClosePosition(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	pos, err := c.GetPosition(ctx, req.CanonicalSymbol, req.VenueSymbol, req.AssetID)
	if err != nil {
		return TradeOrderResult{}, err
	}
	side, qty, ok := closeSideAndQuantity(pos.Quantity, req.Quantity)
	if !ok {
		return TradeOrderResult{Exchange: c.name, CanonicalSymbol: req.CanonicalSymbol, VenueSymbol: req.VenueSymbol, Status: "NO_POSITION"}, nil
	}
	return c.PlaceOrder(ctx, TradeOrderRequest{
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		Side:            side,
		OrderType:       firstNonEmpty(req.OrderType, "MARKET"),
		TimeInForce:     req.TimeInForce,
		Quantity:        qty,
		Price:           req.Price,
		ReduceOnly:      true,
		ClientOrderID:   req.ClientOrderID,
		Reason:          req.Reason,
	})
}

func (c *CEXTradeClient) GetOrderStatus(ctx context.Context, req OrderLookupRequest) (OrderStatus, error) {
	if !c.Enabled() {
		return OrderStatus{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("symbol", req.VenueSymbol)
	if c.authMode != asterTradeAuthV3Signer {
		params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	}
	if req.VenueOrderID != "" {
		params.Set("orderId", req.VenueOrderID)
	} else if req.ClientOrderID != "" {
		params.Set("origClientOrderId", req.ClientOrderID)
	} else {
		return OrderStatus{}, fmt.Errorf("missing order lookup id")
	}
	var payload map[string]any
	raw, err := c.signedGET(ctx, c.orderPath, params, &payload)
	if err != nil {
		return OrderStatus{}, err
	}
	status := strings.ToUpper(asString(payload["status"]))
	return OrderStatus{
		Exchange:      c.name,
		Status:        status,
		ExecutedQty:   parseNullableFloat(payload["executedQty"]),
		AveragePrice:  parseNullableFloat(payload["avgPrice"]),
		VenueOrderID:  firstNonEmpty(asString(payload["orderId"]), req.VenueOrderID),
		ClientOrderID: firstNonEmpty(asString(payload["clientOrderId"]), req.ClientOrderID),
		Terminal:      isTerminalOrderStatus(status),
		Canceled:      status == "CANCELED" || status == "EXPIRED",
		RawResponse:   raw,
	}, nil
}

func (c *CEXTradeClient) GetAccountSnapshot(ctx context.Context) (AccountSnapshot, error) {
	if !c.Enabled() {
		return AccountSnapshot{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	if c.authMode != asterTradeAuthV3Signer {
		params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	}
	var payload map[string]any
	raw, err := c.signedGET(ctx, c.accountPath, params, &payload)
	if err != nil {
		return AccountSnapshot{}, err
	}
	return AccountSnapshot{
		Exchange:         c.name,
		Equity:           firstPositive(parseNullableFloat(payload["totalMarginBalance"]), parseNullableFloat(payload["totalWalletBalance"])),
		AvailableBalance: parseNullableFloat(payload["availableBalance"]),
		MarginUsed:       parseNullableFloat(payload["totalInitialMargin"]),
		RawResponse:      raw,
	}, nil
}

func (c *CEXTradeClient) GetPosition(ctx context.Context, canonicalSymbol, venueSymbol, _ string) (Position, error) {
	if !c.Enabled() {
		return Position{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	if c.authMode != asterTradeAuthV3Signer {
		params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	}
	var payload []map[string]any
	_, err := c.signedGET(ctx, c.positionPath, params, &payload)
	if err != nil {
		return Position{}, err
	}
	for _, item := range payload {
		if !strings.EqualFold(asString(item["symbol"]), venueSymbol) {
			continue
		}
		return Position{
			Exchange:      c.name,
			Symbol:        canonicalSymbol,
			VenueSymbol:   venueSymbol,
			Quantity:      parseNullableFloat(item["positionAmt"]),
			EntryPrice:    parseNullableFloat(item["entryPrice"]),
			MarkPrice:     parseNullableFloat(item["markPrice"]),
			UnrealizedPnL: parseNullableFloat(item["unRealizedProfit"]),
		}, nil
	}
	return Position{Exchange: c.name, Symbol: canonicalSymbol, VenueSymbol: venueSymbol}, nil
}

func (c *CEXTradeClient) signedGET(ctx context.Context, path string, params url.Values, out any) (string, error) {
	return c.signedDo(ctx, http.MethodGet, path, params, out)
}

func (c *CEXTradeClient) signedDELETE(ctx context.Context, path string, params url.Values, out any) (string, error) {
	return c.signedDo(ctx, http.MethodDelete, path, params, out)
}

func (c *CEXTradeClient) signedPOST(ctx context.Context, path string, params url.Values, out any) (string, error) {
	return c.signedDo(ctx, http.MethodPost, path, params, out)
}

func (c *CEXTradeClient) CancelOrder(ctx context.Context, req OrderLookupRequest) error {
	if !c.Enabled() {
		return fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	params := url.Values{}
	params.Set("symbol", req.VenueSymbol)
	if c.authMode != asterTradeAuthV3Signer {
		params.Set("timestamp", fmt.Sprintf("%d", time.Now().UnixMilli()))
	}
	if strings.TrimSpace(req.VenueOrderID) != "" {
		params.Set("orderId", req.VenueOrderID)
	} else if strings.TrimSpace(req.ClientOrderID) != "" {
		params.Set("origClientOrderId", req.ClientOrderID)
	} else {
		return fmt.Errorf("missing order cancel id")
	}
	_, err := c.signedDELETE(ctx, c.orderPath, params, nil)
	return err
}

func (c *CEXTradeClient) signedDo(ctx context.Context, method, path string, params url.Values, out any) (string, error) {
	resp, body, err := c.performSignedRequest(ctx, method, path, params)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		if c.logger != nil {
			c.logger.Warn("cex_signed_request_failed",
				"exchange", c.name,
				"path", path,
				"method", method,
				"status_code", resp.StatusCode,
				"response_body", string(body),
			)
		}
		return string(body), fmt.Errorf("%s signed request failed status=%d body=%s", c.name, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return string(body), err
		}
	}
	return string(body), nil
}

func (c *CEXTradeClient) performSignedRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, []byte, error) {
	switch c.authMode {
	case asterTradeAuthV3Signer:
		return c.performAsterV3SignedRequest(ctx, method, path, params)
	case binanceLikeTradeAuthRSA:
		return c.performRSASignedRequest(ctx, method, path, params)
	}
	return c.performLegacyHMACSignedRequest(ctx, method, path, params)
}

func (c *CEXTradeClient) performLegacyHMACSignedRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, []byte, error) {
	payload := params.Encode()
	signature := signLegacyHMACPayload(c.apiSecret, payload)
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path + "?" + payload + "&signature=" + signature
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, nil, readErr
	}
	return resp, body, nil
}

func (c *CEXTradeClient) performRSASignedRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, []byte, error) {
	payload := params.Encode()
	signature, err := signRSAPayload(c.rsaPrivateKey, payload)
	if err != nil {
		return nil, nil, err
	}
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path + "?" + payload + "&signature=" + url.QueryEscape(signature)
	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("X-MBX-APIKEY", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, nil, readErr
	}
	return resp, body, nil
}

func (c *CEXTradeClient) performAsterV3SignedRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, []byte, error) {
	signedParams := cloneURLValues(params)
	signedParams.Set("user", c.accountAddr)
	signedParams.Set("signer", c.signerAddr)
	signedParams.Set("nonce", fmt.Sprintf("%d", c.nextNonce()))

	payload := signedParams.Encode()
	signature, err := c.signAsterV3Payload(payload)
	if err != nil {
		return nil, nil, err
	}
	signedParams.Set("signature", signature)

	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path
	var bodyReader io.Reader
	if strings.EqualFold(method, http.MethodGet) {
		endpoint += "?" + signedParams.Encode()
	} else {
		bodyReader = strings.NewReader(signedParams.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, bodyReader)
	if err != nil {
		return nil, nil, err
	}
	if bodyReader != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, nil, readErr
	}
	return resp, body, nil
}

func (c *CEXTradeClient) signAsterV3Payload(payload string) (string, error) {
	if c.privateKey == nil {
		return "", fmt.Errorf("%s aster v3 signer private key missing", c.name)
	}
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": {
				{Name: "name", Type: "string"},
				{Name: "version", Type: "string"},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			"Message": {
				{Name: "msg", Type: "string"},
			},
		},
		PrimaryType: "Message",
		Domain: apitypes.TypedDataDomain{
			Name:              "AsterSignTransaction",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(1666),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{
			"msg": payload,
		},
	}
	hash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return "", fmt.Errorf("%s aster v3 sign typed data failed: %w", c.name, err)
	}
	sig, err := crypto.Sign(hash, c.privateKey)
	if err != nil {
		return "", fmt.Errorf("%s aster v3 sign failed: %w", c.name, err)
	}
	if len(sig) != 65 {
		return "", fmt.Errorf("%s aster v3 invalid signature length %d", c.name, len(sig))
	}
	sig[64] += 27
	return hexutil.Encode(sig), nil
}

func (c *CEXTradeClient) nextNonce() int64 {
	c.nonceMu.Lock()
	defer c.nonceMu.Unlock()

	nonce := time.Now().UnixMicro()
	if nonce <= c.lastNonce {
		nonce = c.lastNonce + 1
	}
	c.lastNonce = nonce
	return nonce
}

func cloneURLValues(values url.Values) url.Values {
	if values == nil {
		return url.Values{}
	}
	cloned := make(url.Values, len(values))
	for key, items := range values {
		cloned[key] = append([]string(nil), items...)
	}
	return cloned
}

func tradeAuthMode(name string, cfg ExchangeConfig) string {
	switch strings.ToLower(strings.TrimSpace(cfg.AdapterOption("trade_auth_mode"))) {
	case asterTradeAuthV3Signer:
		return asterTradeAuthV3Signer
	case binanceLikeTradeAuthRSA, "rsa_pkcs8":
		return binanceLikeTradeAuthRSA
	case binanceLikeTradeAuthLegacyHMAC:
		return binanceLikeTradeAuthLegacyHMAC
	}
	if name == "aster" && hasAsterSignerCredentials(cfg.Auth) {
		return asterTradeAuthV3Signer
	}
	if name != "aster" && hasRSASignerCredentials(cfg.Auth) && !hasLegacyHMACCredentials(cfg.Auth) {
		return binanceLikeTradeAuthRSA
	}
	return binanceLikeTradeAuthLegacyHMAC
}

func hasAsterSignerCredentials(auth AuthConfig) bool {
	accountAddr, privateKey := readCredentialPair(auth.AccountAddressEnv, auth.PrivateKeyEnv)
	return strings.TrimSpace(accountAddr) != "" && strings.TrimSpace(privateKey) != ""
}

func hasLegacyHMACCredentials(auth AuthConfig) bool {
	apiKey, apiSecret := readCredentialPair(auth.APIKeyEnv, auth.APISecretEnv)
	return strings.TrimSpace(apiKey) != "" && strings.TrimSpace(apiSecret) != ""
}

func hasRSASignerCredentials(auth AuthConfig) bool {
	apiKey, privateKey := readCredentialPair(auth.APIKeyEnv, auth.PrivateKeyEnv)
	return strings.TrimSpace(apiKey) != "" && strings.TrimSpace(privateKey) != ""
}

func loadAsterSigner(auth AuthConfig) (*ecdsa.PrivateKey, string) {
	_, privateKey := readCredentialPair(auth.AccountAddressEnv, auth.PrivateKeyEnv)
	pkHex := strings.TrimPrefix(privateKey, "0x")
	if strings.TrimSpace(pkHex) == "" {
		return nil, ""
	}
	pk, err := crypto.HexToECDSA(pkHex)
	if err != nil {
		return nil, ""
	}
	extra := auth.ResolveExtraEnv()
	signerAddr := strings.TrimSpace(extra["signer"])
	if signerAddr == "" {
		signerAddr = crypto.PubkeyToAddress(pk.PublicKey).Hex()
	}
	return pk, signerAddr
}

func loadRSASigner(auth AuthConfig) *rsa.PrivateKey {
	_, resolvedPrivateKey := readCredentialPair(auth.APIKeyEnv, auth.PrivateKeyEnv)
	privateKey := normalizePrivateKeyPEM(resolvedPrivateKey)
	if privateKey == "" {
		return nil
	}
	block, _ := pem.Decode([]byte(privateKey))
	if block == nil {
		return nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		if rsaKey, ok := parsed.(*rsa.PrivateKey); ok {
			return rsaKey
		}
		return nil
	}
	rsaKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil
	}
	return rsaKey
}

func normalizePrivateKeyPEM(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	return strings.ReplaceAll(value, `\n`, "\n")
}

func signLegacyHMACPayload(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func signRSAPayload(privateKey *rsa.PrivateKey, payload string) (string, error) {
	if privateKey == nil {
		return "", fmt.Errorf("rsa private key missing")
	}
	digest := sha256.Sum256([]byte(payload))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, stdcrypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(signature), nil
}

func asString(v any) string {
	switch val := v.(type) {
	case nil:
		return ""
	case string:
		return val
	case float64:
		return formatFloat(val, 0)
	case json.Number:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func isTerminalOrderStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FILLED", "CANCELED", "REJECTED", "EXPIRED", "EXPIRED_IN_MATCH", "NO_POSITION":
		return true
	default:
		return false
	}
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func formatFloat(v float64, decimals int) string {
	format := fmt.Sprintf("%%.%df", decimals)
	text := fmt.Sprintf(format, v)
	text = strings.TrimRight(text, "0")
	text = strings.TrimRight(text, ".")
	if text == "" || text == "-0" {
		return "0"
	}
	return text
}

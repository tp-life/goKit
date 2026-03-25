package exchange

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	stdmath "math"
	stdbig "math/big"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethmath "github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
	"github.com/vmihailenco/msgpack/v5"
)

type HyperliquidTradeClient struct {
	name           string
	cfg            ExchangeConfig
	logger         *slog.Logger
	httpClient     *http.Client
	privateKey     *ecdsa.PrivateKey
	accountAddress string
	vaultAddress   string
}

func NewHyperliquidTradeClient(cfg ConfigSet, logger *slog.Logger) TradeAdapter {
	return NewHyperliquidTradeAdapter("hyperliquid", cfg.Hyperliquid, logger)
}

func NewHyperliquidTradeAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) TradeAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		c.RestBaseURL = "https://api.hyperliquid.xyz"
	}
	appCfg := loadAppConfig()
	accountAddress, privateKeyValue := readCredentialPair(c.Auth.AccountAddressEnv, c.Auth.PrivateKeyEnv)
	pkHex := strings.TrimPrefix(privateKeyValue, "0x")
	var pk *ecdsa.PrivateKey
	if pkHex != "" {
		if parsed, err := crypto.HexToECDSA(pkHex); err == nil {
			pk = parsed
		}
	}
	return &HyperliquidTradeClient{
		name:           name,
		cfg:            c,
		logger:         logger,
		httpClient:     newHTTPClient(c, appCfg, logger, name+"-trade"),
		privateKey:     pk,
		accountAddress: strings.ToLower(accountAddress),
		vaultAddress:   strings.ToLower(readEnvByName(c.Auth.VaultAddressEnv)),
	}
}

func (c *HyperliquidTradeClient) Name() string { return c.name }
func (c *HyperliquidTradeClient) Enabled() bool {
	return c.cfg.Enabled && c.privateKey != nil && c.accountAddress != ""
}
func (c *HyperliquidTradeClient) Capabilities() TradeCapabilities {
	return TradeCapabilities{
		MakerLimitTIF:            "ALO",
		TakerOrderType:           "LIMIT",
		TakerTimeInForce:         "IOC",
		TakerUsesAggressiveIOC:   true,
		SupportsOrderEventStream: false,
	}
}

func (c *HyperliquidTradeClient) PlaceOrder(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	if !c.Enabled() {
		return TradeOrderResult{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	asset, err := strconv.Atoi(req.AssetID)
	if err != nil {
		return TradeOrderResult{}, fmt.Errorf("invalid hyperliquid asset id: %w", err)
	}
	wire := hlOrderWire{
		A: asset,
		B: strings.EqualFold(req.Side, "BUY"),
		P: hlFloatToWire(req.Price),
		S: hlFloatToWire(req.Quantity),
		R: req.ReduceOnly,
		T: hlOrderTypeWire{Limit: &hlLimitOrderType{Tif: hlTIF(req.TimeInForce)}},
	}
	if req.ClientOrderID != "" {
		wire.C = req.ClientOrderID
	}
	action := hlOrderAction{Type: "order", Orders: []hlOrderWire{wire}, Grouping: "na"}
	nonce := time.Now().UnixMilli()
	sig, err := c.signL1Action(action, nonce)
	if err != nil {
		return TradeOrderResult{}, err
	}
	payload := map[string]any{
		"action":    action,
		"nonce":     nonce,
		"signature": sig,
	}
	if c.vaultAddress != "" {
		payload["vaultAddress"] = c.vaultAddress
	}
	var resp map[string]any
	raw, err := c.postExchange(ctx, payload, &resp)
	if err != nil {
		return TradeOrderResult{}, err
	}
	status := asString(resp["status"])
	venueOrderID := ""
	if data, ok := resp["response"].(map[string]any); ok {
		if inner, ok := data["data"].(map[string]any); ok {
			venueOrderID = firstNonEmpty(asString(inner["oid"]), asString(inner["orderId"]))
		} else {
			venueOrderID = firstNonEmpty(asString(data["oid"]), asString(data["orderId"]))
		}
	}
	return TradeOrderResult{
		Exchange:        c.name,
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		ClientOrderID:   req.ClientOrderID,
		VenueOrderID:    venueOrderID,
		Status:          firstNonEmpty(status, "submitted"),
		ExecutedQty:     0,
		AveragePrice:    req.Price,
		RawResponse:     raw,
	}, nil
}

func (c *HyperliquidTradeClient) ClosePosition(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error) {
	pos, err := c.GetPosition(ctx, req.CanonicalSymbol, req.VenueSymbol, req.AssetID)
	if err != nil {
		return TradeOrderResult{}, err
	}
	side, qty, ok := closeSideAndQuantity(pos.Quantity, req.Quantity)
	if !ok {
		return TradeOrderResult{Exchange: c.name, CanonicalSymbol: req.CanonicalSymbol, VenueSymbol: req.VenueSymbol, Status: "NO_POSITION"}, nil
	}
	price := req.Price
	if price <= 0 {
		ref := pos.MarkPrice
		if ref <= 0 {
			ref = pos.EntryPrice
		}
		if ref <= 0 {
			ref = 1
		}
		if side == "BUY" {
			price = ref * 1.02
		} else {
			price = ref * 0.98
		}
	}
	return c.PlaceOrder(ctx, TradeOrderRequest{
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		AssetID:         req.AssetID,
		Side:            side,
		OrderType:       "LIMIT",
		TimeInForce:     firstNonEmpty(req.TimeInForce, "IOC"),
		Quantity:        qty,
		Price:           price,
		ReduceOnly:      true,
		ClientOrderID:   req.ClientOrderID,
		Reason:          req.Reason,
	})
}

func (c *HyperliquidTradeClient) GetOrderStatus(ctx context.Context, req OrderLookupRequest) (OrderStatus, error) {
	if !c.Enabled() {
		return OrderStatus{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	payload := map[string]any{"type": "orderStatus", "user": c.accountAddress}
	if req.VenueOrderID != "" {
		payload["oid"] = req.VenueOrderID
	} else if req.ClientOrderID != "" {
		payload["cloid"] = req.ClientOrderID
	} else {
		return OrderStatus{}, fmt.Errorf("missing order lookup id")
	}
	var resp map[string]any
	raw, err := c.postInfo(ctx, payload, &resp)
	if err != nil {
		return OrderStatus{}, err
	}
	status := strings.ToUpper(firstNonEmpty(asString(resp["status"]), asString(resp["state"])))
	filled := parseNullableFloat(resp["filled"])
	avg := parseNullableFloat(resp["avgPx"])
	if data, ok := resp["order"].(map[string]any); ok {
		status = strings.ToUpper(firstNonEmpty(status, asString(data["status"]), asString(data["state"])))
		filled = firstPositive(filled, parseNullableFloat(data["filled"]), parseNullableFloat(data["sz"])-parseNullableFloat(data["remainingSz"]))
		avg = firstPositive(avg, parseNullableFloat(data["avgPx"]))
	}
	return OrderStatus{
		Exchange:      c.name,
		Status:        firstNonEmpty(status, "SUBMITTED"),
		ExecutedQty:   filled,
		AveragePrice:  avg,
		VenueOrderID:  firstNonEmpty(req.VenueOrderID, asString(resp["oid"])),
		ClientOrderID: req.ClientOrderID,
		Terminal:      isTerminalOrderStatus(status),
		Canceled:      status == "CANCELED" || status == "CANCELLED" || status == "EXPIRED",
		RawResponse:   raw,
	}, nil
}

func (c *HyperliquidTradeClient) GetAccountSnapshot(ctx context.Context) (AccountSnapshot, error) {
	if !c.Enabled() {
		return AccountSnapshot{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	payload := map[string]any{"type": "userState", "user": c.accountAddress}
	var resp map[string]any
	raw, err := c.postInfo(ctx, payload, &resp)
	if err != nil {
		return AccountSnapshot{}, err
	}
	margin := 0.0
	if assetPositions, ok := resp["assetPositions"].([]any); ok {
		for _, item := range assetPositions {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			pos, ok := m["position"].(map[string]any)
			if !ok {
				continue
			}
			margin += parseNullableFloat(pos["positionValue"])
		}
	}
	return AccountSnapshot{
		Exchange:         c.name,
		Equity:           hyperliquidEquity(resp),
		AvailableBalance: parseNullableFloat(resp["withdrawable"]),
		MarginUsed:       margin,
		RawResponse:      raw,
	}, nil
}

func (c *HyperliquidTradeClient) GetPosition(ctx context.Context, canonicalSymbol, venueSymbol, _ string) (Position, error) {
	if !c.Enabled() {
		return Position{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	payload := map[string]any{"type": "clearinghouseState", "user": c.accountAddress}
	var resp map[string]any
	_, err := c.postInfo(ctx, payload, &resp)
	if err != nil {
		return Position{}, err
	}
	positions, _ := resp["assetPositions"].([]any)
	for _, rawItem := range positions {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		posMap, ok := item["position"].(map[string]any)
		if !ok {
			continue
		}
		coin := strings.ToUpper(asString(posMap["coin"]))
		if coin != strings.ToUpper(venueSymbol) {
			continue
		}
		return Position{
			Exchange:      c.name,
			Symbol:        canonicalSymbol,
			VenueSymbol:   venueSymbol,
			Quantity:      parseNullableFloat(posMap["szi"]),
			EntryPrice:    parseNullableFloat(posMap["entryPx"]),
			MarkPrice:     parseNullableFloat(posMap["markPx"]),
			UnrealizedPnL: parseNullableFloat(posMap["unrealizedPnl"]),
		}, nil
	}
	return Position{Exchange: c.name, Symbol: canonicalSymbol, VenueSymbol: venueSymbol}, nil
}

type hlLimitOrderType struct {
	Tif string `json:"tif" msgpack:"tif"`
}

type hlOrderTypeWire struct {
	Limit *hlLimitOrderType `json:"limit,omitempty" msgpack:"limit,omitempty"`
}

type hlOrderWire struct {
	A int             `json:"a" msgpack:"a"`
	B bool            `json:"b" msgpack:"b"`
	P string          `json:"p" msgpack:"p"`
	S string          `json:"s" msgpack:"s"`
	R bool            `json:"r" msgpack:"r"`
	T hlOrderTypeWire `json:"t" msgpack:"t"`
	C string          `json:"c,omitempty" msgpack:"c,omitempty"`
}

type hlOrderAction struct {
	Type     string        `json:"type" msgpack:"type"`
	Orders   []hlOrderWire `json:"orders" msgpack:"orders"`
	Grouping string        `json:"grouping" msgpack:"grouping"`
}

type hlSignature struct {
	R string `json:"r"`
	S string `json:"s"`
	V int    `json:"v"`
}

func hyperliquidEquity(resp map[string]any) float64 {
	if summary, ok := resp["marginSummary"].(map[string]any); ok {
		return firstPositive(parseNullableFloat(summary["accountValue"]), parseNullableFloat(summary["marginUsed"]))
	}
	return firstPositive(parseNullableFloat(resp["accountValue"]), parseNullableFloat(resp["withdrawable"]))
}

func hlTIF(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "ALO", "GTX", "MAKER":
		return "Alo"
	case "IOC", "TAKER":
		return "Ioc"
	default:
		return "Gtc"
	}
}

func hlFloatToWire(x float64) string {
	rounded := stdmath.Round(x*1e8) / 1e8
	text := formatFloat(rounded, 8)
	if text == "" {
		return "0"
	}
	return text
}

func (c *HyperliquidTradeClient) signL1Action(action hlOrderAction, nonce int64) (hlSignature, error) {
	packed, err := msgpack.Marshal(action)
	if err != nil {
		return hlSignature{}, err
	}
	hashInput := make([]byte, 0, len(packed)+64)
	hashInput = append(hashInput, packed...)
	hashInput = append(hashInput, uint64ToBytes(uint64(nonce))...)
	if c.vaultAddress == "" {
		hashInput = append(hashInput, byte(0))
	} else {
		hashInput = append(hashInput, byte(1))
		hashInput = append(hashInput, common.HexToAddress(c.vaultAddress).Bytes()...)
	}
	actionHash := crypto.Keccak256(hashInput)
	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{{Name: "name", Type: "string"}, {Name: "version", Type: "string"}, {Name: "chainId", Type: "uint256"}, {Name: "verifyingContract", Type: "address"}},
			"Agent":        []apitypes.Type{{Name: "source", Type: "string"}, {Name: "connectionId", Type: "bytes32"}},
		},
		PrimaryType: "Agent",
		Domain: apitypes.TypedDataDomain{
			Name:              "Exchange",
			Version:           "1",
			ChainId:           (*ethmath.HexOrDecimal256)(stdbig.NewInt(1337)),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{
			"source":       "a",
			"connectionId": common.BytesToHash(actionHash).Hex(),
		},
	}
	digest, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return hlSignature{}, err
	}
	sig, err := crypto.Sign(digest, c.privateKey)
	if err != nil {
		return hlSignature{}, err
	}
	return hlSignature{R: "0x" + hex.EncodeToString(sig[:32]), S: "0x" + hex.EncodeToString(sig[32:64]), V: int(sig[64]) + 27}, nil
}

func uint64ToBytes(v uint64) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint64(b, v)
	return b
}

func (c *HyperliquidTradeClient) postExchange(ctx context.Context, payload any, out any) (string, error) {
	return c.post(ctx, "/exchange", payload, out)
}

func (c *HyperliquidTradeClient) postInfo(ctx context.Context, payload any, out any) (string, error) {
	return c.post(ctx, "/info", payload, out)
}

func (c *HyperliquidTradeClient) post(ctx context.Context, path string, payload any, out any) (string, error) {
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path
	req, err := newJSONRequest(ctx, http.MethodPost, endpoint, payload)
	if err != nil {
		return "", err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return string(body), fmt.Errorf("%s %s failed status=%d body=%s", c.name, path, resp.StatusCode, string(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return string(body), err
		}
	}
	return string(body), nil
}

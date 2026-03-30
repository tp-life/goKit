package exchange

import (
	"context"
	"crypto/ecdsa"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	if strings.TrimSpace(req.ClientOrderID) != "" && !isHyperliquidCloid(req.ClientOrderID) {
		return TradeOrderResult{}, fmt.Errorf("%s invalid cloid %q: expected 0x-prefixed 16-byte hex string", c.name, req.ClientOrderID)
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
	outcome, err := parseHyperliquidPlaceOrderResponse(resp)
	if err != nil {
		return TradeOrderResult{}, fmt.Errorf("%s order rejected: %w", c.name, err)
	}
	return TradeOrderResult{
		Exchange:        c.name,
		CanonicalSymbol: req.CanonicalSymbol,
		VenueSymbol:     req.VenueSymbol,
		ClientOrderID:   req.ClientOrderID,
		VenueOrderID:    outcome.VenueOrderID,
		Status:          outcome.Status,
		ExecutedQty:     outcome.ExecutedQty,
		AveragePrice:    firstPositive(outcome.AveragePrice, req.Price),
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
	payload := map[string]any{"type": "orderStatus", "user": c.infoUserAddress()}
	if req.VenueOrderID != "" {
		payload["oid"] = req.VenueOrderID
	} else if req.ClientOrderID != "" {
		// Hyperliquid 的 orderStatus 查询把真实 oid 和 client oid 都统一放在 `oid` 字段里。
		// 文档说明这里既可以传数值 oid，也可以传 16-byte hex client order id。
		payload["oid"] = req.ClientOrderID
	} else {
		return OrderStatus{}, fmt.Errorf("missing order lookup id")
	}
	var resp map[string]any
	raw, err := c.postInfo(ctx, payload, &resp)
	if err != nil {
		return OrderStatus{}, err
	}
	status, canceled, terminal := normalizeHyperliquidOrderStatus(firstNonEmpty(asString(resp["status"]), asString(resp["state"])))
	filled := parseNullableFloat(resp["filled"])
	avg := parseNullableFloat(resp["avgPx"])
	if data, ok := resp["order"].(map[string]any); ok {
		normalized, dataCanceled, dataTerminal := normalizeHyperliquidOrderStatus(firstNonEmpty(asString(data["status"]), asString(data["state"])))
		status = firstNonEmpty(normalized, status)
		canceled = canceled || dataCanceled
		terminal = terminal || dataTerminal
		filled = firstPositive(filled, parseNullableFloat(data["filled"]), parseNullableFloat(data["sz"])-parseNullableFloat(data["remainingSz"]))
		avg = firstPositive(avg, parseNullableFloat(data["avgPx"]))
	}
	return OrderStatus{
		Exchange:      c.name,
		Status:        firstNonEmpty(status, "SUBMITTED"),
		ExecutedQty:   filled,
		AveragePrice:  avg,
		VenueOrderID:  firstNonEmpty(req.VenueOrderID, asString(resp["oid"]), hyperliquidResponseVenueOrderID(resp)),
		ClientOrderID: req.ClientOrderID,
		Terminal:      terminal,
		Canceled:      canceled,
		RawResponse:   raw,
	}, nil
}

func (c *HyperliquidTradeClient) GetAccountSnapshot(ctx context.Context) (AccountSnapshot, error) {
	if !c.Enabled() {
		return AccountSnapshot{}, fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	payload := map[string]any{"type": "userState", "user": c.infoUserAddress()}
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
	payload := map[string]any{"type": "clearinghouseState", "user": c.infoUserAddress()}
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

func (c *HyperliquidTradeClient) CancelOrder(ctx context.Context, req OrderLookupRequest) error {
	if !c.Enabled() {
		return fmt.Errorf("%s trade client disabled or missing credentials", c.name)
	}
	asset, err := strconv.Atoi(req.AssetID)
	if err != nil {
		return fmt.Errorf("invalid hyperliquid asset id: %w", err)
	}

	var action any
	switch {
	case strings.TrimSpace(req.VenueOrderID) != "":
		oid, err := strconv.ParseUint(strings.TrimSpace(req.VenueOrderID), 10, 64)
		if err != nil {
			return fmt.Errorf("invalid hyperliquid venue order id %q: %w", req.VenueOrderID, err)
		}
		action = hlCancelAction{
			Type: "cancel",
			Cancels: []hlCancelWire{{
				A: asset,
				O: oid,
			}},
		}
	case strings.TrimSpace(req.ClientOrderID) != "":
		if !isHyperliquidCloid(req.ClientOrderID) {
			return fmt.Errorf("invalid hyperliquid cloid %q", req.ClientOrderID)
		}
		action = hlCancelByCloidAction{
			Type: "cancelByCloid",
			Cancels: []hlCancelByCloidWire{{
				Asset: asset,
				Cloid: req.ClientOrderID,
			}},
		}
	default:
		return fmt.Errorf("missing order cancel id")
	}

	nonce := time.Now().UnixMilli()
	sig, err := c.signL1Action(action, nonce)
	if err != nil {
		return err
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
		return err
	}
	if errText := hyperliquidResponseError(resp); errText != "" {
		return fmt.Errorf("%s cancel rejected: %s body=%s", c.name, errText, raw)
	}
	return nil
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

type hlCancelWire struct {
	A int    `json:"a" msgpack:"a"`
	O uint64 `json:"o" msgpack:"o"`
}

type hlCancelAction struct {
	Type    string         `json:"type" msgpack:"type"`
	Cancels []hlCancelWire `json:"cancels" msgpack:"cancels"`
}

type hlCancelByCloidWire struct {
	Asset int    `json:"asset" msgpack:"asset"`
	Cloid string `json:"cloid" msgpack:"cloid"`
}

type hlCancelByCloidAction struct {
	Type    string                `json:"type" msgpack:"type"`
	Cancels []hlCancelByCloidWire `json:"cancels" msgpack:"cancels"`
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

type hyperliquidPlaceOrderOutcome struct {
	VenueOrderID string
	Status       string
	ExecutedQty  float64
	AveragePrice float64
}

func parseHyperliquidPlaceOrderResponse(resp map[string]any) (hyperliquidPlaceOrderOutcome, error) {
	if errText := hyperliquidResponseError(resp); errText != "" {
		return hyperliquidPlaceOrderOutcome{}, errors.New(errText)
	}
	response, _ := resp["response"].(map[string]any)
	data := response
	if inner, ok := response["data"].(map[string]any); ok {
		data = inner
	}
	if statuses, ok := data["statuses"].([]any); ok && len(statuses) > 0 {
		item, _ := statuses[0].(map[string]any)
		if errText := strings.TrimSpace(asString(item["error"])); errText != "" {
			return hyperliquidPlaceOrderOutcome{}, errors.New(errText)
		}
		if resting, ok := item["resting"].(map[string]any); ok {
			return hyperliquidPlaceOrderOutcome{
				VenueOrderID: firstNonEmpty(asString(resting["oid"]), hyperliquidResponseVenueOrderID(resp)),
				Status:       "NEW",
			}, nil
		}
		if filled, ok := item["filled"].(map[string]any); ok {
			return hyperliquidPlaceOrderOutcome{
				VenueOrderID: firstNonEmpty(asString(filled["oid"]), hyperliquidResponseVenueOrderID(resp)),
				Status:       "FILLED",
				ExecutedQty:  firstPositive(parseNullableFloat(filled["totalSz"]), parseNullableFloat(filled["sz"])),
				AveragePrice: parseNullableFloat(filled["avgPx"]),
			}, nil
		}
	}
	return hyperliquidPlaceOrderOutcome{
		VenueOrderID: hyperliquidResponseVenueOrderID(resp),
		Status:       "SUBMITTED",
	}, nil
}

func hyperliquidResponseVenueOrderID(resp map[string]any) string {
	if response, ok := resp["response"].(map[string]any); ok {
		if inner, ok := response["data"].(map[string]any); ok {
			if oid := firstNonEmpty(asString(inner["oid"]), asString(inner["orderId"])); oid != "" {
				return oid
			}
		}
		if oid := firstNonEmpty(asString(response["oid"]), asString(response["orderId"])); oid != "" {
			return oid
		}
	}
	return firstNonEmpty(asString(resp["oid"]), asString(resp["orderId"]))
}

func hyperliquidResponseError(resp map[string]any) string {
	status := strings.ToLower(strings.TrimSpace(asString(resp["status"])))
	if status != "" && status != "ok" {
		return firstNonEmpty(strings.TrimSpace(asString(resp["error"])), strings.TrimSpace(asString(resp["message"])), status)
	}
	if errText := strings.TrimSpace(asString(resp["error"])); errText != "" {
		return errText
	}
	response, _ := resp["response"].(map[string]any)
	if errText := strings.TrimSpace(asString(response["error"])); errText != "" {
		return errText
	}
	data := response
	if inner, ok := response["data"].(map[string]any); ok {
		data = inner
	}
	if errText := strings.TrimSpace(asString(data["error"])); errText != "" {
		return errText
	}
	if statuses, ok := data["statuses"].([]any); ok {
		for _, rawItem := range statuses {
			item, ok := rawItem.(map[string]any)
			if !ok {
				continue
			}
			if errText := strings.TrimSpace(asString(item["error"])); errText != "" {
				return errText
			}
		}
	}
	return ""
}

func normalizeHyperliquidOrderStatus(status string) (normalized string, canceled bool, terminal bool) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "":
		return "", false, false
	case "open", "triggered":
		return "NEW", false, false
	case "filled":
		return "FILLED", false, true
	case "canceled", "margincanceled", "vaultwithdrawalcanceled", "openinterestcapcanceled", "selftradecanceled", "reduceonlycanceled", "siblingfilledcanceled", "delistedcanceled", "liquidatedcanceled", "scheduledcancel":
		return "CANCELED", true, true
	case "rejected", "tickrejected", "mintradentlrejected", "perpmarginrejected", "reduceonlyrejected", "badalopxrejected", "ioccancelrejected", "badtriggerpxrejected":
		return "REJECTED", false, true
	default:
		normalized = strings.ToUpper(strings.TrimSpace(status))
		return normalized, isTerminalOrderStatus(normalized) && (normalized == "CANCELED" || normalized == "CANCELLED"), isTerminalOrderStatus(normalized)
	}
}

func isHyperliquidCloid(value string) bool {
	value = strings.TrimSpace(value)
	if len(value) != 34 || !strings.HasPrefix(value, "0x") {
		return false
	}
	for _, r := range value[2:] {
		if (r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') {
			continue
		}
		return false
	}
	return true
}

func (c *HyperliquidTradeClient) infoUserAddress() string {
	// Hyperliquid 文档特别提醒：查询账户状态时要传“真实账户地址”，
	// 如果在 vault / subaccount 模式下误传 agent wallet 地址，会得到空结果。
	//
	// 在本项目的配置语义里：
	// - `accountAddress` 是主账户签名身份；
	// - `vaultAddress` 表示我们正在代其交易的真实 vault/subaccount。
	//
	// 因此读取 account / position / orderStatus 时，优先使用 vaultAddress。
	if strings.TrimSpace(c.vaultAddress) != "" {
		return c.vaultAddress
	}
	return c.accountAddress
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

func (c *HyperliquidTradeClient) signL1Action(action any, nonce int64) (hlSignature, error) {
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

package polymarket

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/shopspring/decimal"
)

const maxJSSafeInteger uint64 = 9007199254740991

// GetOrderStatus 查询订单的权威成交状态。
func (c *Client) GetOrderStatus(ctx context.Context, orderID string) (*OrderStatus, error) {
	return c.getOrderStatus(ctx, orderID, true)
}

// getOrderStatus 支持在 API key 失效时自动刷新一次凭证后重试。
func (c *Client) getOrderStatus(ctx context.Context, orderID string, allowRetry bool) (*OrderStatus, error) {
	// 订单状态属于需要 L2 鉴权的数据接口。
	creds, err := c.CreateOrDeriveAPIKey(ctx, 0)
	if err != nil {
		return nil, err
	}
	path := "/data/order/" + orderID
	headers, err := c.buildL2Headers(ctx, creds, http.MethodGet, path, "")
	if err != nil {
		return nil, err
	}

	var resp OrderStatusResponse
	if err := c.doJSON(ctx, http.MethodGet, c.cfg.Host+path, nil, headers, &resp); err != nil {
		if allowRetry && isInvalidAPIKeyError(err) && c.HasPrivateKey() {
			if _, refreshErr := c.RefreshAPIKey(ctx, 0); refreshErr == nil {
				return c.getOrderStatus(ctx, orderID, false)
			}
		}
		return nil, err
	}

	// 把底层字符串字段转换成服务层更容易消费的状态模型。
	originalSize, _ := strconv.ParseFloat(resp.OriginalSize, 64)
	sizeMatched, _ := strconv.ParseFloat(resp.SizeMatched, 64)
	return &OrderStatus{
		Status:       strings.ToUpper(resp.Status),
		OriginalSize: originalSize,
		SizeMatched:  sizeMatched,
		Filled:       originalSize > 0 && sizeMatched >= originalSize,
	}, nil
}

// CancelOrder 撤销一个仍挂在订单簿上的订单。
func (c *Client) CancelOrder(ctx context.Context, orderID string) error {
	return c.cancelOrder(ctx, orderID, true)
}

// cancelOrder 支持在 API key 失效时自动刷新一次凭证后重试。
func (c *Client) cancelOrder(ctx context.Context, orderID string, allowRetry bool) error {
	creds, err := c.CreateOrDeriveAPIKey(ctx, 0)
	if err != nil {
		return err
	}

	// 请求体会参与 HMAC 签名，因此必须先完成最终序列化。
	payload := map[string]string{"orderID": orderID}
	bodyBytes, err := marshalCompact(payload)
	if err != nil {
		return err
	}
	headers, err := c.buildL2Headers(ctx, creds, http.MethodDelete, "/order", string(bodyBytes))
	if err != nil {
		return err
	}
	if err := c.doJSON(ctx, http.MethodDelete, c.cfg.Host+"/order", bodyBytes, headers, nil); err != nil {
		if allowRetry && isInvalidAPIKeyError(err) && c.HasPrivateKey() {
			if _, refreshErr := c.RefreshAPIKey(ctx, 0); refreshErr == nil {
				return c.cancelOrder(ctx, orderID, false)
			}
		}
		return err
	}
	return nil
}

// PlaceLimitOrder 构造、签名并提交一个 GTC 限价单到 CLOB。
func (c *Client) PlaceLimitOrder(ctx context.Context, tokenID string, action string, price float64, sizeShares float64) (string, float64, error) {
	if !c.HasPrivateKey() {
		return "", 0, errors.New("private key is not configured")
	}

	// 基于市场元数据完成价格/数量归一化后再签名。
	order, normalizedSize, err := c.createSignedLimitOrder(ctx, tokenID, action, price, sizeShares)
	if err != nil {
		return "", 0, err
	}

	orderID, err := c.submitSignedOrder(ctx, order, true)
	if err != nil {
		return "", 0, err
	}
	return orderID, normalizedSize, nil
}

// submitSignedOrder 负责用当前 L2 凭证提交已签名订单，并在凭证失效时自动刷新后重试一次。
func (c *Client) submitSignedOrder(ctx context.Context, order *SignedOrder, allowRetry bool) (string, error) {
	// 出线 payload 必须与 L2 鉴权签名时使用的字节串完全一致。
	creds, err := c.CreateOrDeriveAPIKey(ctx, 0)
	if err != nil {
		return "", err
	}
	payload, err := c.toPostOrderPayload(order, creds.Key, "GTC", false, false)
	if err != nil {
		return "", err
	}
	bodyBytes, err := marshalCompact(payload)
	if err != nil {
		return "", err
	}
	headers, err := c.buildL2Headers(ctx, creds, http.MethodPost, "/order", string(bodyBytes))
	if err != nil {
		return "", err
	}

	var resp OrderCreateResponse
	if err := c.doJSON(ctx, http.MethodPost, c.cfg.Host+"/order", bodyBytes, headers, &resp); err != nil {
		if allowRetry && isInvalidAPIKeyError(err) && c.HasPrivateKey() {
			if _, refreshErr := c.RefreshAPIKey(ctx, 0); refreshErr == nil {
				return c.submitSignedOrder(ctx, order, false)
			}
		}
		return "", err
	}
	if resp.OrderID == "" {
		return "", errors.New("empty order id returned by Polymarket")
	}
	return resp.OrderID, nil
}

// createSignedLimitOrder 会先按市场 tick 规则归一化价格和数量，再生成 EIP-712 订单载荷。
func (c *Client) createSignedLimitOrder(ctx context.Context, tokenID string, action string, price float64, sizeShares float64) (*SignedOrder, float64, error) {
	// 先拉齐市场元数据，确保舍入规则和验签合约选择都是确定的。
	tickSize, err := c.GetTickSize(ctx, tokenID)
	if err != nil {
		return nil, 0, err
	}
	negRisk, err := c.GetNegRisk(ctx, tokenID)
	if err != nil {
		return nil, 0, err
	}
	feeRate, err := c.GetFeeRateBps(ctx, tokenID)
	if err != nil {
		return nil, 0, err
	}
	if !priceValid(price, tickSize) {
		return nil, 0, fmt.Errorf("invalid price %.4f for tick size %s", price, tickSize)
	}

	// 把外部传入的浮点数转换成市场允许的离散步长。
	cfg := getRoundConfig(tickSize)
	orderSide := strings.ToUpper(strings.TrimSpace(action))
	rawPrice := roundNormal(decimal.NewFromFloat(price), cfg.Price)
	rawSize := decimal.NewFromFloat(sizeShares)
	rawSize = roundDown(rawSize, cfg.Size)
	if rawSize.LessThanOrEqual(decimal.Zero) {
		return nil, 0, errors.New("order size must be greater than 0")
	}

	// BUY 用本金换份额，SELL 则用份额换回本金。
	var makerAmount decimal.Decimal
	var takerAmount decimal.Decimal
	var sideValue uint8
	switch orderSide {
	case "BUY":
		sideValue = 0
		takerAmount = rawSize
		makerAmount = roundAmount(rawSize.Mul(rawPrice), cfg.Amount)
	case "SELL":
		sideValue = 1
		makerAmount = rawSize
		takerAmount = roundAmount(rawSize.Mul(rawPrice), cfg.Amount)
	default:
		return nil, 0, fmt.Errorf("unsupported order side %q", action)
	}

	// 每笔订单都需要唯一 salt，避免不同下单之间签名碰撞。
	salt, err := randomUint64()
	if err != nil {
		return nil, 0, err
	}

	order := &SignedOrder{
		Salt:          new(big.Int).SetUint64(salt),
		Maker:         c.funder.Hex(),
		Signer:        c.address.Hex(),
		Taker:         zeroAddress,
		TokenID:       tokenID,
		MakerAmount:   decimalToScaledInt(makerAmount, 6),
		TakerAmount:   decimalToScaledInt(takerAmount, 6),
		Expiration:    "0",
		Nonce:         "0",
		FeeRateBps:    strconv.Itoa(feeRate),
		Side:          orderSide,
		SignatureType: c.cfg.SignatureType,
	}

	exchange := polygonExchange
	if negRisk {
		exchange = negRiskExchange
	}

	// 订单要针对最终负责链上校验的交易所合约进行签名。
	signature, err := c.signDigest(c.orderDigest(order, exchange, sideValue))
	if err != nil {
		return nil, 0, err
	}
	order.Signature = signature
	return order, rawSize.InexactFloat64(), nil
}

// orderDigest 生成订单 EIP-712 摘要，使得签名结果可直接提交给 CLOB。
// 这里必须绑定到正确的交易所合约，否则服务端和链上都不会认这个签名。
func (c *Client) orderDigest(order *SignedOrder, verifyingContract string, sideValue uint8) common.Hash {
	// ABI 编码前要先把十进制字符串字段还原成大整数。
	tokenID := parseBig(order.TokenID)
	makerAmount := parseBig(order.MakerAmount)
	takerAmount := parseBig(order.TakerAmount)
	expiration := parseBig(order.Expiration)
	nonce := parseBig(order.Nonce)
	feeRate := parseBig(order.FeeRateBps)
	signatureType := big.NewInt(int64(order.SignatureType))

	// 结构体哈希必须与交易所合约校验的 Order 元组顺序完全一致。
	structHash := crypto.Keccak256Hash(mustPack(
		[]abi.Argument{
			{Type: bytes32Type},
			{Type: uint256Type},
			{Type: addressType},
			{Type: addressType},
			{Type: addressType},
			{Type: uint256Type},
			{Type: uint256Type},
			{Type: uint256Type},
			{Type: uint256Type},
			{Type: uint256Type},
			{Type: uint256Type},
			{Type: uint8Type},
			{Type: uint8Type},
		},
		orderTypeHash,
		order.Salt,
		common.HexToAddress(order.Maker),
		common.HexToAddress(order.Signer),
		common.HexToAddress(order.Taker),
		tokenID,
		makerAmount,
		takerAmount,
		expiration,
		nonce,
		feeRate,
		sideValue,
		uint8(signatureType.Uint64()),
	))

	// 域分隔符把摘要明确绑定到具体的交易所合约地址。
	domainSeparator := crypto.Keccak256Hash(mustPack(
		[]abi.Argument{
			{Type: bytes32Type},
			{Type: bytes32Type},
			{Type: bytes32Type},
			{Type: uint256Type},
			{Type: addressType},
		},
		orderDomainTypeHash,
		orderDomainNameHash,
		orderVersionHash,
		big.NewInt(c.cfg.ChainID),
		common.HexToAddress(verifyingContract),
	))

	return crypto.Keccak256Hash([]byte{0x19, 0x01}, domainSeparator.Bytes(), structHash.Bytes())
}

// toPostOrderPayload 把已签名订单转换成 `/order` 接口要求的 JSON 结构。
func (c *Client) toPostOrderPayload(order *SignedOrder, owner, orderType string, deferExec, postOnly bool) (*signedOrderPayload, error) {
	if order == nil {
		return nil, errors.New("order is nil")
	}
	payload := &signedOrderPayload{
		Owner:     owner,
		OrderType: orderType,
		DeferExec: deferExec,
		PostOnly:  postOnly,
	}
	payload.Order.Salt = json.Number(order.Salt.String())
	payload.Order.Maker = order.Maker
	payload.Order.Signer = order.Signer
	payload.Order.Taker = order.Taker
	payload.Order.TokenID = order.TokenID
	payload.Order.MakerAmount = order.MakerAmount
	payload.Order.TakerAmount = order.TakerAmount
	payload.Order.Expiration = order.Expiration
	payload.Order.Nonce = order.Nonce
	payload.Order.FeeRateBps = order.FeeRateBps
	payload.Order.Side = order.Side
	payload.Order.SignatureType = order.SignatureType
	payload.Order.Signature = order.Signature
	return payload, nil
}

// GetTickSize 查询并缓存 token 对应市场的最小 tick size。
func (c *Client) GetTickSize(ctx context.Context, tokenID string) (string, error) {
	// 热缓存可以避免在重试循环里反复拉取几乎不会变化的静态元数据。
	c.cacheMu.RLock()
	if v, ok := c.tickSizes[tokenID]; ok {
		c.cacheMu.RUnlock()
		return v, nil
	}
	c.cacheMu.RUnlock()

	endpoint := c.cfg.Host + "/tick-size?token_id=" + url.QueryEscape(tokenID)
	var resp TickSizeResponse
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &resp); err != nil {
		return "", err
	}
	tickSize := resp.MinimumTickSize.String()
	if tickSize == "" {
		return "", errors.New("tick size missing from response")
	}

	c.cacheMu.Lock()
	c.tickSizes[tokenID] = tickSize
	c.cacheMu.Unlock()
	return tickSize, nil
}

// GetNegRisk 查询并缓存 token 是否属于 neg-risk 市场。
func (c *Client) GetNegRisk(ctx context.Context, tokenID string) (bool, error) {
	c.cacheMu.RLock()
	if v, ok := c.negRisk[tokenID]; ok {
		c.cacheMu.RUnlock()
		return v, nil
	}
	c.cacheMu.RUnlock()

	endpoint := c.cfg.Host + "/neg-risk?token_id=" + url.QueryEscape(tokenID)
	var resp NegRiskResponse
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &resp); err != nil {
		return false, err
	}

	c.cacheMu.Lock()
	c.negRisk[tokenID] = resp.NegRisk
	c.cacheMu.Unlock()
	return resp.NegRisk, nil
}

// GetFeeRateBps 查询并缓存 token 对应市场的手续费基点。
func (c *Client) GetFeeRateBps(ctx context.Context, tokenID string) (int, error) {
	c.cacheMu.RLock()
	if v, ok := c.feeRateBps[tokenID]; ok {
		c.cacheMu.RUnlock()
		return v, nil
	}
	c.cacheMu.RUnlock()

	endpoint := c.cfg.Host + "/fee-rate?token_id=" + url.QueryEscape(tokenID)
	var resp FeeRateResponse
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &resp); err != nil {
		return 0, err
	}
	baseFee := resp.BaseFee.OrZero()

	c.cacheMu.Lock()
	c.feeRateBps[tokenID] = baseFee
	c.cacheMu.Unlock()
	return baseFee, nil
}

type roundConfig struct {
	Price  int32
	Size   int32
	Amount int32
}

// getRoundConfig 根据 tick size 推导订单计算时需要的舍入精度。
func getRoundConfig(tickSize string) roundConfig {
	switch tickSize {
	case "0.1":
		return roundConfig{Price: 1, Size: 2, Amount: 3}
	case "0.01":
		return roundConfig{Price: 2, Size: 2, Amount: 4}
	case "0.001":
		return roundConfig{Price: 3, Size: 2, Amount: 5}
	case "0.0001":
		return roundConfig{Price: 4, Size: 2, Amount: 6}
	default:
		decimals := int32(strings.Index(strings.TrimRight(tickSize, "0"), "."))
		if decimals < 0 {
			decimals = 2
		}
		return roundConfig{Price: 2, Size: 2, Amount: decimals + 2}
	}
}

// roundDown 按给定小数位向零截断。
func roundDown(v decimal.Decimal, places int32) decimal.Decimal {
	return v.Truncate(places)
}

// roundUp 把存在余数的值抬到下一个合法离散步长。
func roundUp(v decimal.Decimal, places int32) decimal.Decimal {
	scaled := v.Shift(places)
	if scaled.Equal(scaled.Floor()) {
		return v
	}
	return scaled.Ceil().Shift(-places)
}

// roundNormal 使用四舍五入规则归一化价格。
func roundNormal(v decimal.Decimal, places int32) decimal.Decimal {
	scaled := v.Shift(places)
	scaled = scaled.Add(decimal.NewFromFloat(0.5))
	return scaled.Floor().Shift(-places)
}

// roundAmount 在 maker/taker 数量精度不一致时，尽量保守地处理本金精度。
func roundAmount(v decimal.Decimal, places int32) decimal.Decimal {
	if decimalPlaces(v) <= places {
		return v
	}
	v = roundUp(v, places+4)
	if decimalPlaces(v) <= places {
		return v
	}
	return roundDown(v, places)
}

// decimalPlaces 返回一个 decimal 当前包含的小数位数。
func decimalPlaces(v decimal.Decimal) int32 {
	exp := v.Exponent()
	if exp >= 0 {
		return 0
	}
	return int32(-exp)
}

// decimalToScaledInt 把 decimal 放大成适合 CLOB 载荷使用的整数字符串。
func decimalToScaledInt(v decimal.Decimal, scale int32) string {
	return v.Shift(scale).Truncate(0).BigInt().String()
}

// parseBig 把十进制字符串转成 big.Int，不额外做业务校验。
func parseBig(raw string) *big.Int {
	n := new(big.Int)
	n.SetString(raw, 10)
	return n
}

// randomUint64 生成一个位于 JS 安全整数范围内的正 salt。
// CLOB 的 JSON 载荷会把 salt 作为 number 发送，若超过 JS 安全整数范围，服务端按 number 解析时可能丢失精度并导致签名失效。
func randomUint64() (uint64, error) {
	n, err := rand.Int(rand.Reader, new(big.Int).SetUint64(maxJSSafeInteger))
	if err != nil {
		return 0, err
	}
	return n.Uint64() + 1, nil
}

// priceValid 校验价格是否满足 Polymarket 的 tick 和概率边界要求。
func priceValid(price float64, tickSize string) bool {
	tick, err := strconv.ParseFloat(tickSize, 64)
	if err != nil {
		return false
	}
	return price >= tick && price <= 1-tick
}

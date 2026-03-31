package polymarket

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/url"
)

// OrderBookLevel 表示订单簿摘要里的单个价位档。
type OrderBookLevel struct {
	Price string `json:"price"`
	Size  string `json:"size"`
}

// OrderBookSummary 保持字段顺序与官方客户端一致。
// 这样生成的 SHA-1 才能与 Polymarket 的标准订单簿 hash 对齐。
type OrderBookSummary struct {
	Market         *string          `json:"market,omitempty"`
	AssetID        *string          `json:"asset_id,omitempty"`
	Timestamp      *string          `json:"timestamp,omitempty"`
	Bids           []OrderBookLevel `json:"bids"`
	Asks           []OrderBookLevel `json:"asks"`
	MinOrderSize   *string          `json:"min_order_size,omitempty"`
	TickSize       *string          `json:"tick_size,omitempty"`
	NegRisk        *bool            `json:"neg_risk,omitempty"`
	LastTradePrice *string          `json:"last_trade_price,omitempty"`
	Hash           string           `json:"hash"`
}

// GetOrderBook 拉取指定 token 的当前订单簿快照。
func (c *Client) GetOrderBook(ctx context.Context, tokenID string) (*OrderBookSummary, error) {
	endpoint := c.cfg.Host + "/book?token_id=" + url.QueryEscape(tokenID)
	var book OrderBookSummary
	if err := c.doJSON(ctx, http.MethodGet, endpoint, nil, nil, &book); err != nil {
		return nil, err
	}

	// 把 nil 切片统一成空切片，保证后续 hash 计算是确定性的。
	if book.Bids == nil {
		book.Bids = []OrderBookLevel{}
	}
	if book.Asks == nil {
		book.Asks = []OrderBookLevel{}
	}
	return &book, nil
}

// GetOrderBookHash 计算订单簿的标准 hash，并回写到对象本身。
func (c *Client) GetOrderBookHash(orderbook *OrderBookSummary) (string, error) {
	return GenerateOrderBookSummaryHash(orderbook)
}

// ValidateOrderBookHash 比较对象内嵌 hash 与重新计算出的标准 hash 是否一致。
func (c *Client) ValidateOrderBookHash(orderbook *OrderBookSummary) (bool, string, error) {
	if orderbook == nil {
		return false, "", nil
	}

	// 先保存服务端返回的 hash，供调用方与重新计算结果对比。
	original := orderbook.Hash
	hash, err := GenerateOrderBookSummaryHash(orderbook)
	if err != nil {
		return false, "", err
	}
	return original == hash, hash, nil
}

// GenerateOrderBookSummaryHash 按官方客户端规则生成订单簿 hash。
// 具体过程是先清空 hash 字段，再按固定字段顺序编码 JSON，最后对字节串做 SHA-1。
func GenerateOrderBookSummaryHash(orderbook *OrderBookSummary) (string, error) {
	if orderbook == nil {
		return "", nil
	}

	canonical := *orderbook
	if canonical.Bids == nil {
		canonical.Bids = []OrderBookLevel{}
	}
	if canonical.Asks == nil {
		canonical.Asks = []OrderBookLevel{}
	}
	canonical.Hash = ""

	body, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha1.Sum(body)
	hash := hex.EncodeToString(sum[:])
	orderbook.Hash = hash
	return hash, nil
}

package polymarket

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"
)

const displaySpreadFallbackThreshold = 0.10

// GetActiveMarket 基于当前配置解析当前或下一轮目标市场。
func (c *Client) GetActiveMarket(ctx context.Context) (*ActiveMarketView, error) {
	// 优先尝试当前配置周期对应的 slug，尽量停留在正在交易的市场上。
	current := c.currentSlug(time.Now())
	if market, err := c.fetchMarketBySlug(ctx, current); err == nil && market != nil && market.Remaining > 0 {
		return market, nil
	}

	// 如果当前轮已经结束或尚未可用，则回退到下一轮，方便提前预热状态。
	next := c.nextSlug(time.Now())
	return c.fetchMarketBySlug(ctx, next)
}

// currentSlug 按配置的资产和周期生成当前轮标识。
func (c *Client) currentSlug(now time.Time) string {
	unix := now.Unix()
	intervalSec := int64(c.cfg.ResolvedMarketIntervalSec())
	currentWindow := (unix / intervalSec) * intervalSec
	return fmt.Sprintf("%s-%s-%d", c.cfg.ResolvedMarketSlugPrefix(), c.cfg.ResolvedMarketSlugInterval(), currentWindow)
}

// nextSlug 返回给定时间点之后的下一轮市场 slug。
func (c *Client) nextSlug(now time.Time) string {
	unix := now.Unix()
	intervalSec := int64(c.cfg.ResolvedMarketIntervalSec())
	nextWindow := ((unix / intervalSec) + 1) * intervalSec
	return fmt.Sprintf("%s-%s-%d", c.cfg.ResolvedMarketSlugPrefix(), c.cfg.ResolvedMarketSlugInterval(), nextWindow)
}

// fetchMarketBySlug 按 slug 拉取单个 Gamma 事件，并投影成 SDK 使用的市场视图。
func (c *Client) fetchMarketBySlug(ctx context.Context, slug string) (*ActiveMarketView, error) {
	endpoint, err := url.Parse(c.cfg.GammaAPI + "/events")
	if err != nil {
		return nil, err
	}

	// Gamma 通过查询参数而不是路径参数按 slug 过滤事件。
	q := endpoint.Query()
	q.Set("slug", slug)
	endpoint.RawQuery = q.Encode()

	var events []GammaEventPayload
	if err := c.doJSON(ctx, http.MethodGet, endpoint.String(), nil, nil, &events); err != nil {
		return nil, err
	}
	if len(events) == 0 {
		return nil, nil
	}
	event := events[0]
	if event.Closed || event.EndDate == "" || event.StartTime == "" || len(event.Markets) == 0 {
		return nil, nil
	}

	// 已经过期的事件直接忽略，避免调用方误交易已结束轮次。
	endAt, err := time.Parse(time.RFC3339, normalizeRFC3339(event.EndDate))
	if err != nil {
		return nil, err
	}
	remaining := int(time.Until(endAt).Seconds())
	if remaining <= 0 {
		return nil, nil
	}

	market := event.Markets[0]
	prices, _ := asStringSlice(market.OutcomePrices)
	tokens, _ := asStringSlice(market.ClobTokenIDs)

	result := &ActiveMarketView{
		Slug:      slug,
		Start:     event.StartTime,
		End:       event.EndDate,
		Remaining: remaining,
	}
	if len(prices) > 0 {
		if v, err := strconv.ParseFloat(prices[0], 64); err == nil {
			result.UpPrice = &v
		}
	}
	if len(prices) > 1 {
		if v, err := strconv.ParseFloat(prices[1], 64); err == nil {
			result.DownPrice = &v
		}
	}
	if len(tokens) > 0 {
		result.UpToken = tokens[0]
	}
	if len(tokens) > 1 {
		result.DownToken = tokens[1]
	}
	return result, nil
}

// SubscribeMarket 订阅指定 token 对的一档盘口和展示价更新。
func (c *Client) SubscribeMarket(ctx context.Context, upToken, downToken string, onUpdate func(assetID string, bid, ask, display float64)) error {
	return c.runWSLoop(ctx, c.cfg.ClobWSURL, func(conn *websocket.Conn) error {
		// 通过一条连接同时订阅两个 outcome token，保证成对状态尽量同步。
		// 同时开启 custom_feature，拿到 best_bid_ask 及 last_trade_price 所需的完整上下文。
		sub := map[string]any{
			"assets_ids":             []string{upToken, downToken},
			"type":                   "market",
			"custom_feature_enabled": true,
		}
		if err := conn.WriteJSON(sub); err != nil {
			return err
		}

		quotes := map[string]marketQuoteState{
			upToken:   {},
			downToken: {},
		}
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return err
			}

			// 单个 websocket 帧里可能包含多条市场事件。
			updates := extractMarketUpdates(msg)
			for _, update := range updates {
				state, ok := quotes[update.AssetID]
				if !ok {
					state = marketQuoteState{}
				}
				bid, ask, displayPrice, emit := state.apply(update)
				quotes[update.AssetID] = state
				if emit {
					onUpdate(update.AssetID, bid, ask, displayPrice)
				}
			}
		}
	})
}

// GetERC20Balance 通过 Polygon RPC 直接读取配置账户的 USDC 余额。
func (c *Client) GetERC20Balance(ctx context.Context, account string) (*float64, error) {
	if strings.TrimSpace(c.cfg.PolygonRPCURL) == "" || strings.TrimSpace(account) == "" {
		return nil, nil
	}

	// 这里只需要 balanceOf，因此直接内嵌一个最小 ABI 即可。
	abiJSON := `[{"constant":true,"inputs":[{"name":"account","type":"address"}],"name":"balanceOf","outputs":[{"name":"","type":"uint256"}],"stateMutability":"view","type":"function"}]`
	parsedABI, err := abi.JSON(strings.NewReader(abiJSON))
	if err != nil {
		return nil, err
	}
	data, err := parsedABI.Pack("balanceOf", common.HexToAddress(account))
	if err != nil {
		return nil, err
	}

	// `eth_call` 返回的是 token 最小单位下的整数余额。
	payload := map[string]any{
		"jsonrpc": "2.0",
		"method":  "eth_call",
		"params": []any{
			map[string]any{
				"to":   "0x2791Bca1f2de4661ED88A30C99A7a9449Aa84174",
				"data": "0x" + hex.EncodeToString(data),
			},
			"latest",
		},
		"id": 1,
	}
	body, err := marshalCompact(payload)
	if err != nil {
		return nil, err
	}
	var resp RPCBalanceResponse
	if err := c.doJSON(ctx, http.MethodPost, c.cfg.PolygonRPCURL, body, nil, &resp); err != nil {
		return nil, err
	}
	if resp.Result == "" {
		return nil, nil
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(resp.Result, "0x"), 16)
	v := decimal.NewFromBigInt(n, -6).InexactFloat64()
	return &v, nil
}

type marketUpdate struct {
	AssetID   string
	Bid       *float64
	Ask       *float64
	LastTrade *float64
}

// marketQuoteState 保存单个 outcome token 的盘口和展示价状态。
type marketQuoteState struct {
	bid        float64
	ask        float64
	lastTrade  float64
	display    float64
	hasBid     bool
	hasAsk     bool
	hasTrade   bool
	hasDisplay bool
}

// extractMarketUpdates 把原始 websocket 载荷转换成统一的一档盘口更新。
func extractMarketUpdates(message []byte) []marketUpdate {
	var raw any
	if err := json.Unmarshal(message, &raw); err != nil {
		return nil
	}
	var items []map[string]any
	switch v := raw.(type) {
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
	case map[string]any:
		items = append(items, v)
	}

	// `book`、`price_change`、`best_bid_ask` 和 `last_trade_price` 都会影响展示价。
	updates := make([]marketUpdate, 0, len(items))
	for _, item := range items {
		eventType, _ := item["event_type"].(string)
		switch eventType {
		case "book":
			assetID, _ := item["asset_id"].(string)
			bid, ask := extractBestBidAsk(item["bids"], item["asks"])
			if bid > 0 && ask > 0 {
				bidCopy := bid
				askCopy := ask
				updates = append(updates, marketUpdate{AssetID: assetID, Bid: &bidCopy, Ask: &askCopy})
			}
		case "price_change":
			priceChanges, _ := item["price_changes"].([]any)
			for _, rawChange := range priceChanges {
				change, _ := rawChange.(map[string]any)
				if change == nil {
					continue
				}
				assetID, _ := change["asset_id"].(string)
				bid := maybeFloat(change["best_bid"])
				ask := maybeFloat(change["best_ask"])
				if bid == nil || ask == nil || *bid <= 0 || *ask <= 0 {
					continue
				}
				bidCopy := *bid
				askCopy := *ask
				updates = append(updates, marketUpdate{AssetID: assetID, Bid: &bidCopy, Ask: &askCopy})
			}
		case "best_bid_ask":
			assetID, _ := item["asset_id"].(string)
			bid := maybeFloat(item["best_bid"])
			ask := maybeFloat(item["best_ask"])
			if bid == nil || ask == nil || *bid <= 0 || *ask <= 0 {
				continue
			}
			bidCopy := *bid
			askCopy := *ask
			updates = append(updates, marketUpdate{AssetID: assetID, Bid: &bidCopy, Ask: &askCopy})
		case "last_trade_price":
			assetID, _ := item["asset_id"].(string)
			price := maybeFloat(item["price"])
			if price == nil || *price <= 0 {
				continue
			}
			priceCopy := *price
			updates = append(updates, marketUpdate{AssetID: assetID, LastTrade: &priceCopy})
		}
	}
	return updates
}

// apply 合并单条 websocket 更新，并根据官方展示规则输出最新价格。
func (s *marketQuoteState) apply(update marketUpdate) (float64, float64, float64, bool) {
	if update.Bid != nil && *update.Bid > 0 {
		s.bid = *update.Bid
		s.hasBid = true
	}
	if update.Ask != nil && *update.Ask > 0 {
		s.ask = *update.Ask
		s.hasAsk = true
	}
	if update.LastTrade != nil && *update.LastTrade > 0 {
		s.lastTrade = *update.LastTrade
		s.hasTrade = true
	}

	if s.hasBid && s.hasAsk {
		spread := s.ask - s.bid
		switch {
		case spread <= displaySpreadFallbackThreshold:
			s.display = (s.bid + s.ask) / 2
			s.hasDisplay = true
		case s.hasTrade:
			// 官方前端在宽价差场景下显示最后成交价，而不是 midpoint。
			s.display = s.lastTrade
			s.hasDisplay = true
		}
	}
	if !s.hasDisplay && s.hasTrade {
		s.display = s.lastTrade
		s.hasDisplay = true
	}

	if !s.hasDisplay {
		return 0, 0, 0, false
	}
	return s.bid, s.ask, s.display, true
}

// extractBestBidAsk 从原始 bids/asks 数组中扫描出当前一档盘口。
func extractBestBidAsk(bidsRaw, asksRaw any) (float64, float64) {
	var bestBid float64
	var bestAsk float64
	if bids, ok := bidsRaw.([]any); ok {
		for _, bid := range bids {
			if m, ok := bid.(map[string]any); ok {
				price := floatValue(m["price"])
				if price > bestBid {
					bestBid = price
				}
			}
		}
	}
	if asks, ok := asksRaw.([]any); ok {
		for _, ask := range asks {
			if m, ok := ask.(map[string]any); ok {
				price := floatValue(m["price"])
				if price <= 0 {
					continue
				}
				if bestAsk == 0 || price < bestAsk {
					bestAsk = price
				}
			}
		}
	}
	return bestBid, bestAsk
}

// asStringSlice 兼容 Gamma 直接返回数组，或把数组编码成 JSON 字符串两种情况。
func asStringSlice(v any) ([]string, bool) {
	switch values := v.(type) {
	case []any:
		out := make([]string, 0, len(values))
		for _, item := range values {
			out = append(out, fmt.Sprint(item))
		}
		return out, true
	case string:
		var out []string
		if err := json.Unmarshal([]byte(values), &out); err == nil {
			return out, true
		}
	}
	return nil, false
}

// normalizeRFC3339 把 `Z` 结尾时间改写成显式偏移格式，方便 Go 稳定解析。
func normalizeRFC3339(v string) string {
	return strings.Replace(v, "Z", "+00:00", 1)
}

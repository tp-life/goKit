package exchange

import (
	"context"
	"strings"

	"goKit/internal/domain/entity"
)

type MarketSink interface {
	UpsertSymbol(symbol entity.Symbol)
	UpsertFunding(item entity.FundingSnapshot)
	UpsertBookTop(item entity.BookTopSnapshot)
	UpdateStatus(status ConnectorStatus)
}

// MarketSubscriptionProvider 暴露给交易所适配器两类订阅集合：
// 1. FundingSymbols: 低成本、适合全市场粗筛的数据（例如 funding / mark price）。
// 2. BookSymbols: 高频且更昂贵的数据（例如 best bid/ask），只对核心币 + 候选币深扫。
//
// 这样做的好处是，交易所适配器不需要知道“为什么选这些币”，
// 只需要在需要的时候拿到当前应该订阅的 symbol 列表即可。
type MarketSubscriptionProvider interface {
	FundingSymbols(exchangeName string) []entity.Symbol
	BookSymbols(exchangeName string) []entity.Symbol
}

type MarketAdapter interface {
	Name() string
	Enabled() bool
	Fees() FeeConfig
	Config() ExchangeConfig
	FetchTradableSymbols(ctx context.Context, quoteAsset string, allowed map[string]struct{}) ([]entity.Symbol, error)
	Start(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink)
}

type TradeOrderRequest struct {
	CanonicalSymbol string
	VenueSymbol     string
	AssetID         string
	Side            string
	OrderType       string
	TimeInForce     string
	Quantity        float64
	Price           float64
	ReduceOnly      bool
	ClientOrderID   string
	Reason          string
}

type TradeOrderResult struct {
	Exchange        string
	CanonicalSymbol string
	VenueSymbol     string
	ClientOrderID   string
	VenueOrderID    string
	Status          string
	ExecutedQty     float64
	AveragePrice    float64
	RawResponse     string
}

type OrderLookupRequest struct {
	CanonicalSymbol string
	VenueSymbol     string
	AssetID         string
	ClientOrderID   string
	VenueOrderID    string
}

type OrderStatus struct {
	Exchange      string
	Status        string
	ExecutedQty   float64
	AveragePrice  float64
	VenueOrderID  string
	ClientOrderID string
	Terminal      bool
	Canceled      bool
	RawResponse   string
}

type AccountSnapshot struct {
	Exchange         string
	Equity           float64
	AvailableBalance float64
	MarginUsed       float64
	RawResponse      string
}

type Position struct {
	Exchange      string
	Symbol        string
	VenueSymbol   string
	Quantity      float64
	EntryPrice    float64
	MarkPrice     float64
	UnrealizedPnL float64
}

type TradeAdapter interface {
	Name() string
	Enabled() bool
	PlaceOrder(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error)
	ClosePosition(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error)
	GetPosition(ctx context.Context, canonicalSymbol, venueSymbol, assetID string) (Position, error)
	GetOrderStatus(ctx context.Context, req OrderLookupRequest) (OrderStatus, error)
	GetAccountSnapshot(ctx context.Context) (AccountSnapshot, error)
}

func BuildMarketMap(items []MarketAdapter) map[string]MarketAdapter {
	out := make(map[string]MarketAdapter, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out[strings.ToLower(item.Name())] = item
	}
	return out
}

func BuildTradeMap(items []TradeAdapter) map[string]TradeAdapter {
	out := make(map[string]TradeAdapter, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		out[strings.ToLower(item.Name())] = item
	}
	return out
}

package exchange

import (
	"context"
	"strings"
	"time"

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

// FundingRateHistoryProvider 是市场适配器的可选能力接口。
//
// 它用于在策略启动和运行过程中，从交易所补齐“真实 funding 历史事件”。
// 当前策略会优先消费这份历史事件，再退回到本地分钟级 snapshot。
type FundingRateHistoryProvider interface {
	FetchFundingRateHistory(ctx context.Context, symbol entity.Symbol, startTime, endTime time.Time) ([]entity.FundingRateHistory, error)
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
	StopPrice       float64
	WorkingType     string
	PriceProtect    bool
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

// OrderEvent 表示一条从交易所私有流、用户流或恢复流程进入系统的订单事件。
//
// 它和同步下单后的 `TradeOrderResult` 不同：
// 1. `TradeOrderResult` 更像“本次请求立刻返回了什么”；
// 2. `OrderEvent` 更像“订单后来又发生了一个新事实”。
//
// 因此这里显式保留了 source / occurredAt / terminal / canceled 等字段，
// 方便上层把它接到真正的事件驱动状态机，而不只是做一次性的响应解析。
type OrderEvent struct {
	Source        string
	Exchange      string
	ClientOrderID string
	VenueOrderID  string
	Status        string
	ExecutedQty   float64
	AveragePrice  float64
	Terminal      bool
	Canceled      bool
	ErrorMessage  string
	RawPayload    string
	OccurredAtMs  int64
}

// OrderEventSink 是订单事件流向上游应用层的最小输出接口。
//
// 之所以不让交易所适配器直接依赖具体 service，是为了让：
// - 适配器层只负责“如何拿到事件”；
// - 应用层只负责“拿到事件后如何推进状态机”。
//
// 这使得未来无论是 websocket、轮询回放还是测试注入，都可以复用同一条事件管道。
type OrderEventSink interface {
	PublishOrderEvent(event OrderEvent)
}

type Position struct {
	Exchange         string
	Symbol           string
	VenueSymbol      string
	Quantity         float64
	EntryPrice       float64
	MarkPrice        float64
	LiquidationPrice float64
	UnrealizedPnL    float64
}

type TradeCapabilities struct {
	MakerLimitTIF            string
	TakerOrderType           string
	TakerTimeInForce         string
	TakerUsesAggressiveIOC   bool
	SupportsOrderEventStream bool
}

type TradeAdapter interface {
	Name() string
	Enabled() bool
	Capabilities() TradeCapabilities
	PlaceOrder(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error)
	ClosePosition(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error)
	GetPosition(ctx context.Context, canonicalSymbol, venueSymbol, assetID string) (Position, error)
	GetOrderStatus(ctx context.Context, req OrderLookupRequest) (OrderStatus, error)
	GetAccountSnapshot(ctx context.Context) (AccountSnapshot, error)
}

// TradeProtectiveStopPlacer 是“交易所原生保护单”可选能力接口。
//
// 当前主要给 Binance-like 永续合约使用：
// - 用原生 STOP_MARKET / reduce-only 把空头保护单挂到交易所；
// - 真正触发时，不必等应用层下一轮轮询才开始平仓。
//
// Spot 侧不强求实现这条接口，因为同所模式里更安全的做法是：
// - 永续腿先靠原生保护单止损；
// - 现货腿再由仓位监控做联动收口。
type TradeProtectiveStopPlacer interface {
	PlaceProtectiveStop(ctx context.Context, req TradeOrderRequest) (TradeOrderResult, error)
}

// TradeOrderCanceler 是一个可选能力接口。
//
// 它主要用于“下单后发现本轮执行已经不该继续挂着”这类场景，
// 例如 maker 开仓腿在 entry deadline 后仍未成交，就应该尽快撤单，
// 避免订单在 funding 之后被动成交。
type TradeOrderCanceler interface {
	CancelOrder(ctx context.Context, req OrderLookupRequest) error
}

// TradeOrderEventStreamer 是一个可选能力接口。
//
// 并不是所有交易所适配器在当前阶段都必须实现它：
// - 只做同步下单/轮询的适配器可以先不实现；
// - 已经具备用户流或私有订单回报的适配器，则可以实现它并把事件推给上层。
//
// 这样做的好处是，系统已经具备事件流接入口，但不会强迫所有交易所在同一时刻补齐。
type TradeOrderEventStreamer interface {
	StartOrderEventStream(ctx context.Context, sink OrderEventSink) error
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

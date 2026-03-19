package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"goKit/internal/domain/entity"

	"github.com/gorilla/websocket"
)

const bybitPublicPingInterval = 20 * time.Second

// BybitV5MarketClient 负责把 Bybit V5 的 public market data 接到统一的 MarketAdapter 上。
//
// 当前版本已经优先使用 Bybit public websocket ticker 流，而不是继续依赖 REST 轮询：
// 1. `tickers.{symbol}` 本身就同时带 funding / mark / best bid / ask，适合作为统一主通道；
// 2. websocket 能持续接收价格变化，而不是只看到若干秒一次的“抽样快照”；
// 3. 对套利系统来说，这能明显改善入场时机判断和盘口新鲜度。
//
// 同时这里仍然保留了 REST helper，原因是：
// - `FetchTradableSymbols` 依旧需要走 instruments-info；
// - 如果某些部署环境暂时不想开 public ws，也还能 fallback 到轮询。
type BybitV5MarketClient struct {
	baseStatusHolder
	name       string
	cfg        ExchangeConfig
	logger     *slog.Logger
	httpClient *http.Client
	wsDialer   *websocket.Dialer
}

type bybitInstrumentFilter struct {
	TickSize string `json:"tickSize"`
}

type bybitLotSizeFilter struct {
	QtyStep          string `json:"qtyStep"`
	MinOrderQty      string `json:"minOrderQty"`
	MinNotionalValue string `json:"minNotionalValue"`
}

type bybitInstrument struct {
	Symbol          string                `json:"symbol"`
	Status          string                `json:"status"`
	ContractType    string                `json:"contractType"`
	BaseCoin        string                `json:"baseCoin"`
	QuoteCoin       string                `json:"quoteCoin"`
	SettleCoin      string                `json:"settleCoin"`
	FundingInterval int                   `json:"fundingInterval"`
	PriceFilter     bybitInstrumentFilter `json:"priceFilter"`
	LotSizeFilter   bybitLotSizeFilter    `json:"lotSizeFilter"`
}

type bybitInstrumentsResult struct {
	Category       string            `json:"category"`
	NextPageCursor string            `json:"nextPageCursor"`
	List           []bybitInstrument `json:"list"`
}

type bybitTicker struct {
	Symbol          string `json:"symbol"`
	Bid1Price       string `json:"bid1Price"`
	Bid1Size        string `json:"bid1Size"`
	Ask1Price       string `json:"ask1Price"`
	Ask1Size        string `json:"ask1Size"`
	MarkPrice       string `json:"markPrice"`
	IndexPrice      string `json:"indexPrice"`
	FundingRate     string `json:"fundingRate"`
	NextFundingTime string `json:"nextFundingTime"`
}

type bybitTickersResult struct {
	Category string        `json:"category"`
	List     []bybitTicker `json:"list"`
}

type bybitTickerStreamMessage struct {
	Topic string      `json:"topic"`
	Type  string      `json:"type"`
	TS    int64       `json:"ts"`
	Data  bybitTicker `json:"data"`
}

type bybitPublicStreamSnapshot struct {
	FundingWatch            map[string]entity.Symbol
	BookWatch               map[string]entity.Symbol
	TickerBookFallbackWatch map[string]entity.Symbol
	TickerDesired           map[string]entity.Symbol
	OrderbookDesired        map[string]entity.Symbol
}

type bybitOrderbookStreamMessage struct {
	Topic string             `json:"topic"`
	Type  string             `json:"type"`
	TS    int64              `json:"ts"`
	Data  bybitOrderbookData `json:"data"`
	CTS   int64              `json:"cts"`
}

type bybitOrderbookData struct {
	Symbol string     `json:"s"`
	Bids   [][]string `json:"b"`
	Asks   [][]string `json:"a"`
}

func NewBybitV5MarketAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) MarketAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		c.RestBaseURL = "https://api.bybit.com"
	}
	if c.MarketWSBaseURL == "" {
		c.MarketWSBaseURL = bybitPublicMarketWSBaseURL(c)
	}
	appCfg := loadAppConfig()
	return &BybitV5MarketClient{
		name:             name,
		cfg:              c,
		logger:           logger,
		httpClient:       newHTTPClient(c, appCfg, logger, name),
		wsDialer:         newWebSocketDialer(c, appCfg, logger, name),
		baseStatusHolder: baseStatusHolder{status: ConnectorStatus{Exchange: name}},
	}
}

func (c *BybitV5MarketClient) Name() string           { return c.name }
func (c *BybitV5MarketClient) Enabled() bool          { return c.cfg.Enabled }
func (c *BybitV5MarketClient) Fees() FeeConfig        { return c.cfg.Fees }
func (c *BybitV5MarketClient) Config() ExchangeConfig { return c.cfg }

func (c *BybitV5MarketClient) FetchTradableSymbols(ctx context.Context, quoteAsset string, allowed map[string]struct{}) ([]entity.Symbol, error) {
	category := bybitCategory(c.cfg)
	cursor := ""

	// 和 Binance instruments 不同，Bybit linear/inverse 的 instruments-info 是分页接口。
	// 这里显式做 cursor 遍历，避免因为“默认只拿第一页”而静默漏掉大量可交易合约。
	selected := make(map[string]entity.Symbol)
	for {
		params := url.Values{}
		params.Set("category", category)
		params.Set("limit", "1000")
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		var payload bybitInstrumentsResult
		if _, err := c.publicGET(ctx, "/v5/market/instruments-info", params, &payload); err != nil {
			return nil, err
		}

		for _, item := range payload.List {
			if !c.isTradablePerpetual(item, quoteAsset) {
				continue
			}

			canonical := canonicalFrom(item.Symbol, item.BaseCoin)
			if !normalizeAllowed(allowed, item.Symbol, canonical, item.BaseCoin) {
				continue
			}

			symbol := entity.Symbol{
				Exchange:             c.name,
				Symbol:               canonical,
				VenueSymbol:          strings.ToUpper(item.Symbol),
				BaseAsset:            strings.ToUpper(item.BaseCoin),
				QuoteAsset:           strings.ToUpper(item.QuoteCoin),
				SettleAsset:          strings.ToUpper(firstNonEmpty(item.SettleCoin, c.cfg.SettleAsset)),
				Status:               strings.ToUpper(item.Status),
				ContractType:         "PERPETUAL",
				TickSize:             item.PriceFilter.TickSize,
				StepSize:             item.LotSizeFilter.QtyStep,
				MinQty:               firstNonEmpty(item.LotSizeFilter.MinOrderQty, item.LotSizeFilter.QtyStep),
				MinNotional:          item.LotSizeFilter.MinNotionalValue,
				FundingIntervalHours: bybitFundingIntervalHours(item.FundingInterval, c.cfg.DefaultFundingIntervalHours),
				VenueAssetID:         strings.ToUpper(item.Symbol),
				Enabled:              true,
				Watched:              true,
			}

			if prev, ok := selected[symbol.Symbol]; ok {
				if preferTradableSymbol(symbol, prev) {
					selected[symbol.Symbol] = symbol
				}
				continue
			}
			selected[symbol.Symbol] = symbol
		}

		cursor = strings.TrimSpace(payload.NextPageCursor)
		if cursor == "" {
			break
		}
	}

	out := make([]entity.Symbol, 0, len(selected))
	for _, item := range selected {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Symbol == out[j].Symbol {
			return out[i].VenueSymbol < out[j].VenueSymbol
		}
		return out[i].Symbol < out[j].Symbol
	})
	return out, nil
}

func (c *BybitV5MarketClient) Start(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	if !c.Enabled() {
		return
	}
	if strings.TrimSpace(c.cfg.MarketWSBaseURL) != "" {
		go c.runTickerStreamLoop(ctx, provider, sink)
		return
	}
	go c.runTickerPollingLoop(ctx, provider, sink)
}

func (c *BybitV5MarketClient) runTickerPollingLoop(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	interval := bybitPollInterval(c.cfg)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		c.pollTickers(ctx, provider, sink)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runTickerStreamLoop 维护 Bybit public market websocket。
//
// 这条流承担两层职责：
// 1. funding 粗筛层：mark/index/funding/nextFundingTime；
// 2. book top 层：`orderbook.1.{symbol}` 提供更细的 top-of-book，ticker 只在确实没有 orderbook 订阅时才兜底。
//
// 当前选择“同一连接同时挂 ticker + orderbook.1”，是为了平衡两件事：
// - funding 相关字段仍然由 ticker 主导，因为它天然包含 mark/index/funding；
// - book 相关字段则额外引入 L1 orderbook，让盘口更新更及时。
func (c *BybitV5MarketClient) runTickerStreamLoop(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	state := make(map[string]bybitTicker)
	currentTickerSubs := make(map[string]entity.Symbol)
	currentOrderbookSubs := make(map[string]entity.Symbol)

	runDynamicPublicStream(ctx, dynamicPublicStreamConfig[bybitPublicStreamSnapshot]{
		Endpoint: c.cfg.MarketWSBaseURL,
		Dial: func(ctx context.Context, endpoint string) (*websocket.Conn, error) {
			conn, _, err := c.wsDialer.DialContext(ctx, endpoint, nil)
			return conn, err
		},
		ResolveSnapshot: func() (bybitPublicStreamSnapshot, bool) {
			snapshot := c.resolveStreamWatches(provider)
			return snapshot, len(snapshot.TickerDesired) > 0 || len(snapshot.OrderbookDesired) > 0
		},
		OnConnect: func(conn *websocket.Conn, snapshot bybitPublicStreamSnapshot) error {
			clear(currentTickerSubs)
			clear(currentOrderbookSubs)
			if err := c.syncTickerSubscriptions(conn, currentTickerSubs, snapshot.TickerDesired); err != nil {
				return err
			}
			return c.syncOrderbookSubscriptions(conn, currentOrderbookSubs, snapshot.OrderbookDesired)
		},
		OnRefresh: func(conn *websocket.Conn, snapshot bybitPublicStreamSnapshot) error {
			if err := c.syncTickerSubscriptions(conn, currentTickerSubs, snapshot.TickerDesired); err != nil {
				return err
			}
			return c.syncOrderbookSubscriptions(conn, currentOrderbookSubs, snapshot.OrderbookDesired)
		},
		Keepalive: c.keepalivePublicMarketStream,
		OnConnected: func() {
			c.updateStatus(func(s *ConnectorStatus) { s.LastError = "" })
			sink.UpdateStatus(c.currentStatus())
		},
		OnDisconnected: func(err error) {
			if c.logger != nil {
				c.logger.Warn("dynamic_public_stream_stopped",
					slog.String("exchange", c.name),
					slog.String("kind", "market"),
					slog.Any("err", err),
				)
			}
			c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
			sink.UpdateStatus(c.currentStatus())
		},
		OnMessage: func(msg []byte, snapshot bybitPublicStreamSnapshot) {
			c.handleTickerStreamMessage(msg, snapshot.FundingWatch, snapshot.TickerBookFallbackWatch, state, sink)
			c.handleOrderbookStreamMessage(msg, snapshot.BookWatch, sink)
		},
		ReadTimeout: 30 * time.Second,
	})
}

// pollTickers 把 Bybit 当前整站 ticker 快照拆成两层输出：
// 1. funding 粗筛层需要的 mark/index/funding/nextFundingTime；
// 2. 深扫层需要的 bid1/ask1。
//
// 因为 Bybit 的同一条 REST 响应里已经包含这两类字段，
// 所以这里一次请求即可喂给两条上游链路，避免 duplicated polling。
func (c *BybitV5MarketClient) pollTickers(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	fundingWatch := makeSymbolWatch(provider.FundingSymbols(c.name))
	bookWatch := makeSymbolWatch(provider.BookSymbols(c.name))
	if len(fundingWatch) == 0 && len(bookWatch) == 0 {
		return
	}

	payload, eventTimeMs, err := c.fetchTickers(ctx)
	if err != nil {
		c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
		sink.UpdateStatus(c.currentStatus())
		return
	}

	now := time.Now().UTC()
	marketUpdated := false
	bookUpdated := false

	for _, item := range payload.List {
		venueSymbol := strings.ToUpper(item.Symbol)

		if meta, ok := fundingWatch[venueSymbol]; ok {
			sink.UpsertFunding(entity.FundingSnapshot{
				Exchange:             c.name,
				Symbol:               meta.Symbol,
				VenueSymbol:          meta.VenueSymbol,
				MarkPrice:            mustFloat(item.MarkPrice),
				IndexPrice:           mustFloat(item.IndexPrice),
				EstimatedSettlePrice: mustFloat(item.MarkPrice),
				FundingRate:          mustFloat(item.FundingRate),
				FundingTimeMs:        parseNullableInt64(item.NextFundingTime),
				FundingIntervalHours: meta.FundingIntervalHours,
				EventTimeMs:          eventTimeMs,
			})
			marketUpdated = true
		}

		if meta, ok := bookWatch[venueSymbol]; ok {
			sink.UpsertBookTop(entity.BookTopSnapshot{
				Exchange:    c.name,
				Symbol:      meta.Symbol,
				VenueSymbol: meta.VenueSymbol,
				BidPrice:    mustFloat(item.Bid1Price),
				BidQty:      mustFloat(item.Bid1Size),
				AskPrice:    mustFloat(item.Ask1Price),
				AskQty:      mustFloat(item.Ask1Size),
				EventTimeMs: eventTimeMs,
			})
			bookUpdated = true
		}
	}

	c.updateStatus(func(s *ConnectorStatus) {
		s.LastError = ""
		if marketUpdated {
			s.MarkPriceConnected = true
			s.LastMarketEventAt = now
		}
		if bookUpdated {
			s.BookTickerConnected = true
			s.LastBookEventAt = now
		}
	})
	sink.UpdateStatus(c.currentStatus())
}

func (c *BybitV5MarketClient) resolveStreamWatches(provider MarketSubscriptionProvider) bybitPublicStreamSnapshot {
	fundingWatch := makeSymbolWatch(provider.FundingSymbols(c.name))
	bookWatch := makeSymbolWatch(provider.BookSymbols(c.name))
	tickerDesired := make(map[string]entity.Symbol, len(fundingWatch)+len(bookWatch))
	orderbookDesired := make(map[string]entity.Symbol, len(bookWatch))
	for venueSymbol, meta := range fundingWatch {
		tickerDesired[venueSymbol] = meta
	}
	for venueSymbol, meta := range bookWatch {
		tickerDesired[venueSymbol] = meta
		orderbookDesired[venueSymbol] = meta
	}

	// 当前默认所有深扫 book symbol 都会上 L1 orderbook。
	// 但这里仍然显式保留 ticker book fallback 集合，而不是直接写死成空，
	// 是为了给未来一些“只想要 funding，不想额外挂 book stream”的 symbol 留扩展位。
	tickerBookFallback := make(map[string]entity.Symbol)
	for venueSymbol, meta := range bookWatch {
		if _, ok := orderbookDesired[venueSymbol]; ok {
			continue
		}
		tickerBookFallback[venueSymbol] = meta
	}
	return bybitPublicStreamSnapshot{
		FundingWatch:            fundingWatch,
		BookWatch:               bookWatch,
		TickerBookFallbackWatch: tickerBookFallback,
		TickerDesired:           tickerDesired,
		OrderbookDesired:        orderbookDesired,
	}
}

// syncTickerSubscriptions 把当前 websocket 上的 topic 集合收敛到目标集合。
//
// Bybit 这里采用“长期连接 + 增量 subscribe/unsubscribe”而不是“深扫池一变就整体重连”，
// 主要是为了降低：
// 1. 高频重连带来的握手开销；
// 2. 代理/网络抖动时的连接脆弱性；
// 3. 在候选池频繁进出时的无效链路震荡。
func (c *BybitV5MarketClient) syncTickerSubscriptions(conn *websocket.Conn, current, desired map[string]entity.Symbol) error {
	toUnsubscribe := make([]string, 0)
	for venueSymbol := range current {
		if _, ok := desired[venueSymbol]; ok {
			continue
		}
		toUnsubscribe = append(toUnsubscribe, "tickers."+venueSymbol)
		delete(current, venueSymbol)
	}
	if len(toUnsubscribe) > 0 {
		if err := conn.WriteJSON(map[string]any{"op": "unsubscribe", "args": toUnsubscribe}); err != nil {
			return err
		}
	}

	toSubscribe := make([]string, 0)
	for venueSymbol, meta := range desired {
		if _, ok := current[venueSymbol]; ok {
			continue
		}
		toSubscribe = append(toSubscribe, "tickers."+venueSymbol)
		current[venueSymbol] = meta
	}
	if len(toSubscribe) > 0 {
		if err := conn.WriteJSON(map[string]any{"op": "subscribe", "args": toSubscribe}); err != nil {
			return err
		}
	}
	return nil
}

// syncOrderbookSubscriptions 维护 `orderbook.1.{symbol}` 订阅集合。
//
// L1 orderbook 的目标非常聚焦：
// - 只承担更细的 top-of-book 更新；
// - 不试图在这里引入更深档位或完整本地 orderbook 合并。
//
// 这样后续如果我们真的需要 depth=50/200，再扩一个更重的本地簿逻辑即可，
// 不会把当前 funding arbitrage 主链一并拖复杂。
func (c *BybitV5MarketClient) syncOrderbookSubscriptions(conn *websocket.Conn, current, desired map[string]entity.Symbol) error {
	toUnsubscribe := make([]string, 0)
	for venueSymbol := range current {
		if _, ok := desired[venueSymbol]; ok {
			continue
		}
		toUnsubscribe = append(toUnsubscribe, "orderbook.1."+venueSymbol)
		delete(current, venueSymbol)
	}
	if len(toUnsubscribe) > 0 {
		if err := conn.WriteJSON(map[string]any{"op": "unsubscribe", "args": toUnsubscribe}); err != nil {
			return err
		}
	}

	toSubscribe := make([]string, 0)
	for venueSymbol, meta := range desired {
		if _, ok := current[venueSymbol]; ok {
			continue
		}
		toSubscribe = append(toSubscribe, "orderbook.1."+venueSymbol)
		current[venueSymbol] = meta
	}
	if len(toSubscribe) > 0 {
		if err := conn.WriteJSON(map[string]any{"op": "subscribe", "args": toSubscribe}); err != nil {
			return err
		}
	}
	return nil
}

func (c *BybitV5MarketClient) keepalivePublicMarketStream(ctx context.Context, conn *websocket.Conn, stop <-chan struct{}) {
	ticker := time.NewTicker(bybitPublicPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			if err := conn.WriteJSON(map[string]any{"op": "ping"}); err != nil {
				if c.logger != nil {
					c.logger.Warn("bybit_public_market_ping_failed", slog.String("exchange", c.name), slog.Any("err", err))
				}
				return
			}
		}
	}
}

// handleTickerStreamMessage 把 Bybit ticker websocket 消息翻译成 funding/book 快照。
//
// Bybit ticker 流既可能发 snapshot，也可能发 delta；而 delta 只带“变化了的字段”。
// 因此这里必须维护本地 symbol state，不能把每条 delta 都当成完整快照直接覆盖，
// 否则很容易把未出现的字段错误清空。
func (c *BybitV5MarketClient) handleTickerStreamMessage(
	msg []byte,
	fundingWatch map[string]entity.Symbol,
	bookWatch map[string]entity.Symbol,
	state map[string]bybitTicker,
	sink MarketSink,
) {
	var meta struct {
		Op    string `json:"op"`
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(msg, &meta); err != nil {
		if c.logger != nil {
			c.logger.Warn("bybit_ticker_unmarshal_failed", slog.String("exchange", c.name), slog.String("payload", string(msg)))
		}
		return
	}
	if strings.TrimSpace(meta.Op) != "" {
		return
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(meta.Topic)), "tickers.") {
		return
	}

	var packet bybitTickerStreamMessage
	if err := json.Unmarshal(msg, &packet); err != nil {
		if c.logger != nil {
			c.logger.Warn("bybit_ticker_unmarshal_failed", slog.String("exchange", c.name), slog.String("payload", string(msg)))
		}
		return
	}

	venueSymbol := strings.ToUpper(strings.TrimSpace(packet.Data.Symbol))
	if venueSymbol == "" {
		venueSymbol = strings.ToUpper(strings.TrimPrefix(packet.Topic, "tickers."))
	}
	if venueSymbol == "" {
		return
	}

	merged := mergeBybitTickerState(state[venueSymbol], packet.Data)
	merged.Symbol = venueSymbol
	state[venueSymbol] = merged

	now := time.Now().UTC()
	marketUpdated := false
	bookUpdated := false

	if meta, ok := fundingWatch[venueSymbol]; ok {
		sink.UpsertFunding(entity.FundingSnapshot{
			Exchange:             c.name,
			Symbol:               meta.Symbol,
			VenueSymbol:          meta.VenueSymbol,
			MarkPrice:            mustFloat(merged.MarkPrice),
			IndexPrice:           mustFloat(merged.IndexPrice),
			EstimatedSettlePrice: mustFloat(merged.MarkPrice),
			FundingRate:          mustFloat(merged.FundingRate),
			FundingTimeMs:        parseNullableInt64(merged.NextFundingTime),
			FundingIntervalHours: meta.FundingIntervalHours,
			EventTimeMs:          packet.TS,
		})
		marketUpdated = true
	}

	if meta, ok := bookWatch[venueSymbol]; ok {
		sink.UpsertBookTop(entity.BookTopSnapshot{
			Exchange:    c.name,
			Symbol:      meta.Symbol,
			VenueSymbol: meta.VenueSymbol,
			BidPrice:    mustFloat(merged.Bid1Price),
			BidQty:      mustFloat(merged.Bid1Size),
			AskPrice:    mustFloat(merged.Ask1Price),
			AskQty:      mustFloat(merged.Ask1Size),
			EventTimeMs: packet.TS,
		})
		bookUpdated = true
	}

	if !marketUpdated && !bookUpdated {
		return
	}
	c.updateStatus(func(s *ConnectorStatus) {
		s.LastError = ""
		if marketUpdated {
			s.MarkPriceConnected = true
			s.LastMarketEventAt = now
		}
		if bookUpdated {
			s.BookTickerConnected = true
			s.LastBookEventAt = now
		}
	})
	sink.UpdateStatus(c.currentStatus())
}

func mergeBybitTickerState(base, update bybitTicker) bybitTicker {
	if update.Symbol != "" {
		base.Symbol = update.Symbol
	}
	if update.Bid1Price != "" {
		base.Bid1Price = update.Bid1Price
	}
	if update.Bid1Size != "" {
		base.Bid1Size = update.Bid1Size
	}
	if update.Ask1Price != "" {
		base.Ask1Price = update.Ask1Price
	}
	if update.Ask1Size != "" {
		base.Ask1Size = update.Ask1Size
	}
	if update.MarkPrice != "" {
		base.MarkPrice = update.MarkPrice
	}
	if update.IndexPrice != "" {
		base.IndexPrice = update.IndexPrice
	}
	if update.FundingRate != "" {
		base.FundingRate = update.FundingRate
	}
	if update.NextFundingTime != "" {
		base.NextFundingTime = update.NextFundingTime
	}
	return base
}

// handleOrderbookStreamMessage 处理 Bybit `orderbook.1.{symbol}` 消息。
//
// 对 linear/inverse L1，Bybit 文档说明这条流以 snapshot 形式持续推送 top-of-book；
// 这里仍然兼容读取 bids/asks 数组的第一档，而不把逻辑写死在“只有 snapshot”这个假设上，
// 这样未来即便服务端细节变化，解析层也更稳一些。
func (c *BybitV5MarketClient) handleOrderbookStreamMessage(msg []byte, bookWatch map[string]entity.Symbol, sink MarketSink) {
	var meta struct {
		Op    string `json:"op"`
		Topic string `json:"topic"`
	}
	if err := json.Unmarshal(msg, &meta); err != nil {
		if c.logger != nil {
			c.logger.Warn("bybit_orderbook_unmarshal_failed", slog.String("exchange", c.name), slog.String("payload", string(msg)))
		}
		return
	}
	if strings.TrimSpace(meta.Op) != "" {
		return
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(meta.Topic)), "orderbook.1.") {
		return
	}

	var packet bybitOrderbookStreamMessage
	if err := json.Unmarshal(msg, &packet); err != nil {
		if c.logger != nil {
			c.logger.Warn("bybit_orderbook_unmarshal_failed", slog.String("exchange", c.name), slog.String("payload", string(msg)))
		}
		return
	}

	venueSymbol := strings.ToUpper(strings.TrimSpace(packet.Data.Symbol))
	if venueSymbol == "" {
		venueSymbol = bybitVenueSymbolFromTopic(packet.Topic, "orderbook.1.")
	}
	if venueSymbol == "" {
		return
	}

	metaSymbol, ok := bookWatch[venueSymbol]
	if !ok {
		return
	}

	bidPrice, bidQty, bidOK := firstBybitBookLevel(packet.Data.Bids)
	askPrice, askQty, askOK := firstBybitBookLevel(packet.Data.Asks)
	if !bidOK && !askOK {
		return
	}

	eventTimeMs := pickPositiveInt64(packet.CTS, packet.TS, time.Now().UnixMilli())
	sink.UpsertBookTop(entity.BookTopSnapshot{
		Exchange:    c.name,
		Symbol:      metaSymbol.Symbol,
		VenueSymbol: metaSymbol.VenueSymbol,
		BidPrice:    bidPrice,
		BidQty:      bidQty,
		AskPrice:    askPrice,
		AskQty:      askQty,
		EventTimeMs: eventTimeMs,
	})
	c.updateStatus(func(s *ConnectorStatus) {
		s.LastError = ""
		s.BookTickerConnected = true
		s.LastBookEventAt = time.Now().UTC()
	})
	sink.UpdateStatus(c.currentStatus())
}

func firstBybitBookLevel(levels [][]string) (float64, float64, bool) {
	if len(levels) == 0 || len(levels[0]) < 2 {
		return 0, 0, false
	}
	return mustFloat(levels[0][0]), mustFloat(levels[0][1]), true
}

func bybitVenueSymbolFromTopic(topic, prefix string) string {
	topic = strings.TrimSpace(topic)
	prefix = strings.TrimSpace(prefix)
	if !strings.HasPrefix(strings.ToLower(topic), strings.ToLower(prefix)) {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(topic[len(prefix):]))
}

func (c *BybitV5MarketClient) fetchTickers(ctx context.Context) (bybitTickersResult, int64, error) {
	params := url.Values{}
	params.Set("category", bybitCategory(c.cfg))

	var payload bybitTickersResult
	eventTimeMs, err := c.publicGET(ctx, "/v5/market/tickers", params, &payload)
	if err != nil {
		return bybitTickersResult{}, 0, err
	}
	if eventTimeMs <= 0 {
		eventTimeMs = time.Now().UnixMilli()
	}
	return payload, eventTimeMs, nil
}

func (c *BybitV5MarketClient) publicGET(ctx context.Context, path string, params url.Values, out any) (int64, error) {
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + path
	if encoded := params.Encode(); encoded != "" {
		endpoint += "?" + encoded
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode >= 300 {
		return 0, fmt.Errorf("%s bybit public request %s failed status=%d body=%s", c.name, path, resp.StatusCode, string(body))
	}

	envelope, err := decodeBybitEnvelope(body, out)
	if err != nil {
		return 0, fmt.Errorf("%s bybit public request %s failed: %w", c.name, path, err)
	}
	return envelope.Time, nil
}

func (c *BybitV5MarketClient) isTradablePerpetual(item bybitInstrument, quoteAsset string) bool {
	if !strings.EqualFold(item.Status, "Trading") {
		return false
	}
	if quoteAsset != "" && !strings.EqualFold(item.QuoteCoin, quoteAsset) {
		return false
	}
	contractType := strings.ToUpper(strings.TrimSpace(item.ContractType))
	return strings.Contains(contractType, "PERPETUAL")
}

func parseNullableInt64(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	out, _ := strconv.ParseInt(value, 10, 64)
	return out
}

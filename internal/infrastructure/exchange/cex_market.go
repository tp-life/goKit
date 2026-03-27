package exchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"goKit/internal/domain/entity"

	"github.com/gorilla/websocket"
)

// CEXMarketClient 是一个保留了历史命名的实现类型。
//
// 虽然名字里有 `CEX`，但它的真实语义并不是“任何中心化交易所都能复用”，
// 而是专门服务于 Binance / Aster 这类 Binance-like 永续合约协议族。
//
// 之所以暂时保留旧名字，是为了减少这轮架构纠偏对现有调用面的冲击；
// 但从 adapter_kind 语义上，它现在应该被理解成 `binance_like`，而不是通用 `cex`。
//
// 这个实现当前假设：
// - exchangeInfo / fundingInfo 走 REST
// - mark price 走全市场 websocket
// - best bid / ask 只对策略层指定的深扫池 symbol 建立 combined stream
//
// 这里故意把 funding 与 book 分成两条链路，是为了支持：
// “全市场 funding 粗筛 + 候选盘口深扫”的策略结构。
type CEXMarketClient struct {
	baseStatusHolder
	name       string
	cfg        ExchangeConfig
	logger     *slog.Logger
	httpClient *http.Client
	wsDialer   *websocket.Dialer
}

func NewBinanceMarketClient(cfg ConfigSet, logger *slog.Logger) MarketAdapter {
	return NewCEXMarketAdapter("binance", cfg.Binance, logger)
}

func NewAsterMarketClient(cfg ConfigSet, logger *slog.Logger) MarketAdapter {
	return NewCEXMarketAdapter("aster", cfg.Aster, logger)
}

// NewBinanceLikeMarketAdapter 是 registry 层应该优先使用的构造入口。
//
// 它明确表达“当前这份实现服务的是 Binance-like 协议族”，
// 而不是所有 CEX 交易所。
func NewBinanceLikeMarketAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) MarketAdapter {
	return NewCEXMarketAdapter(name, cfg, logger)
}

// NewCEXMarketAdapter 是保留给现有代码的兼容构造入口。
//
// 名称虽然沿用了旧叫法，但真实语义已经被收窄为 `binance_like` 协议族。
func NewCEXMarketAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) MarketAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		switch name {
		case "aster":
			c.RestBaseURL = "https://fapi.asterdex.com"
		default:
			c.RestBaseURL = "https://fapi.binance.com"
		}
	}
	if c.MarketWSBaseURL == "" {
		switch name {
		case "aster":
			c.MarketWSBaseURL = "wss://fstream.asterdex.com"
		default:
			c.MarketWSBaseURL = "wss://fstream.binance.com/market"
		}
	}
	if c.PublicWSBaseURL == "" {
		switch name {
		case "aster":
			// Aster 当前沿用 Binance-like 的 ws 基地址，没有 public / market 的强制分流。
			c.PublicWSBaseURL = "wss://fstream.asterdex.com"
		default:
			c.PublicWSBaseURL = "wss://fstream.binance.com/public"
		}
	}
	return newCEXMarketClient(name, c, logger)
}

func newCEXMarketClient(name string, cfg ExchangeConfig, logger *slog.Logger) *CEXMarketClient {
	appCfg := loadAppConfig()
	return &CEXMarketClient{
		name:             name,
		cfg:              cfg,
		logger:           logger,
		httpClient:       newHTTPClient(cfg, appCfg, logger, name),
		wsDialer:         newWebSocketDialer(cfg, appCfg, logger, name),
		baseStatusHolder: baseStatusHolder{status: ConnectorStatus{Exchange: name}},
	}
}

func (c *CEXMarketClient) Name() string           { return c.name }
func (c *CEXMarketClient) Enabled() bool          { return c.cfg.Enabled }
func (c *CEXMarketClient) Fees() FeeConfig        { return c.cfg.Fees }
func (c *CEXMarketClient) Config() ExchangeConfig { return c.cfg }

type exchangeInfoFilter struct {
	FilterType  string `json:"filterType"`
	TickSize    string `json:"tickSize"`
	StepSize    string `json:"stepSize"`
	MinQty      string `json:"minQty"`
	MaxQty      string `json:"maxQty"`
	Notional    string `json:"notional"`
	MinNotional string `json:"minNotional"`
}

type exchangeInfoSymbol struct {
	Symbol       string               `json:"symbol"`
	Status       string               `json:"status"`
	ContractType string               `json:"contractType"`
	BaseAsset    string               `json:"baseAsset"`
	QuoteAsset   string               `json:"quoteAsset"`
	Filters      []exchangeInfoFilter `json:"filters"`
}

type exchangeInfoResponse struct {
	Symbols []exchangeInfoSymbol `json:"symbols"`
}

func parseExchangeInfoFloat(raw string) float64 {
	value, _ := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return value
}

type fundingInfoItem struct {
	Symbol               string `json:"symbol"`
	FundingIntervalHours int    `json:"fundingIntervalHours"`
}

func (c *CEXMarketClient) FetchTradableSymbols(ctx context.Context, quoteAsset string, allowed map[string]struct{}) ([]entity.Symbol, error) {
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + "/fapi/v1/exchangeInfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s exchangeInfo status=%d body=%s", c.name, resp.StatusCode, string(body))
	}

	var payload exchangeInfoResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	intervals := c.fetchFundingIntervals(ctx)

	// 同一个 canonical symbol（例如 BTC）在单个交易所只保留一个 venue symbol。
	// 这样可以避免 BTCUSDT 被 BTCUSDT_20260626 这类 dated contract 覆盖。
	selected := make(map[string]entity.Symbol, len(payload.Symbols))

	for _, sym := range payload.Symbols {
		if quoteAsset != "" && !strings.EqualFold(sym.QuoteAsset, quoteAsset) {
			continue
		}
		if !strings.EqualFold(sym.Status, "TRADING") {
			continue
		}
		if !isFundingTradableContract(sym) {
			continue
		}

		canonical := canonicalFrom(sym.Symbol, sym.BaseAsset)
		if !normalizeAllowed(allowed, sym.Symbol, canonical, sym.BaseAsset) {
			continue
		}

		item := entity.Symbol{
			Exchange:             c.name,
			Symbol:               canonical,
			VenueSymbol:          strings.ToUpper(sym.Symbol),
			BaseAsset:            strings.ToUpper(sym.BaseAsset),
			QuoteAsset:           strings.ToUpper(sym.QuoteAsset),
			SettleAsset:          c.cfg.SettleAsset,
			Status:               sym.Status,
			ContractType:         sym.ContractType,
			FundingIntervalHours: c.cfg.DefaultFundingIntervalHours,
			Enabled:              true,
			Watched:              true,
		}
		execMeta := entity.SymbolExecutionMeta{}

		for _, f := range sym.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				item.TickSize = f.TickSize
			case "LOT_SIZE":
				if item.StepSize == "" {
					item.StepSize = f.StepSize
				}
				if item.MinQty == "" {
					item.MinQty = f.MinQty
				}
				if maxQty := parseExchangeInfoFloat(f.MaxQty); maxQty > 0 {
					execMeta.LimitMaxQty = maxQty
				}
			case "MARKET_LOT_SIZE":
				// Binance-like venue 经常同时返回 LOT_SIZE 和 MARKET_LOT_SIZE。
				// 对策略来说，两者都要保留：
				// - LOT_SIZE    决定 LIMIT/IOC 这类路径的单笔上限；
				// - MARKET_LOT_SIZE 决定 MARKET 单的单笔上限。
				//
				// 如果这里只有 MARKET_LOT_SIZE，也继续回填 step/min，
				// 避免执行层连最基本的数量取整信息都拿不到。
				if item.StepSize == "" {
					item.StepSize = f.StepSize
				}
				if item.MinQty == "" {
					item.MinQty = f.MinQty
				}
				if maxQty := parseExchangeInfoFloat(f.MaxQty); maxQty > 0 {
					execMeta.MarketMaxQty = maxQty
				}
			case "MIN_NOTIONAL":
				if f.MinNotional != "" {
					item.MinNotional = f.MinNotional
				} else if f.Notional != "" {
					item.MinNotional = f.Notional
				}
			}
		}
		if execMeta.MarketMaxQty <= 0 {
			execMeta.MarketMaxQty = execMeta.LimitMaxQty
		}
		if err := item.SetExecutionMeta(execMeta); err != nil {
			return nil, err
		}

		if interval, ok := intervals[item.VenueSymbol]; ok && interval > 0 {
			item.FundingIntervalHours = interval
		}

		if prev, ok := selected[item.Symbol]; ok {
			if preferTradableSymbol(item, prev) {
				selected[item.Symbol] = item
			}
			continue
		}
		selected[item.Symbol] = item
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

// isFundingTradableContract 用于过滤“适合资金费率套利”的合约。
// 核心原则：只保留永续，不把 dated contract 混进 funding 主流程。
func isFundingTradableContract(sym exchangeInfoSymbol) bool {
	contractType := strings.ToUpper(strings.TrimSpace(sym.ContractType))
	venueSymbol := strings.ToUpper(strings.TrimSpace(sym.Symbol))
	if contractType != "" {
		return contractType == "PERPETUAL"
	}
	if strings.Contains(venueSymbol, "_") {
		return false
	}
	return true
}

func preferTradableSymbol(candidate, current entity.Symbol) bool {
	candidatePerp := strings.EqualFold(candidate.ContractType, "PERPETUAL")
	currentPerp := strings.EqualFold(current.ContractType, "PERPETUAL")
	if candidatePerp != currentPerp {
		return candidatePerp
	}
	candidateUnderscore := strings.Contains(candidate.VenueSymbol, "_")
	currentUnderscore := strings.Contains(current.VenueSymbol, "_")
	if candidateUnderscore != currentUnderscore {
		return !candidateUnderscore
	}
	if len(candidate.VenueSymbol) != len(current.VenueSymbol) {
		return len(candidate.VenueSymbol) < len(current.VenueSymbol)
	}
	return candidate.VenueSymbol < current.VenueSymbol
}

func (c *CEXMarketClient) fetchFundingIntervals(ctx context.Context) map[string]int {
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + "/fapi/v1/fundingInfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return map[string]int{}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return map[string]int{}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return map[string]int{}
	}
	var payload []fundingInfoItem
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return map[string]int{}
	}
	out := make(map[string]int, len(payload))
	for _, item := range payload {
		if item.FundingIntervalHours > 0 {
			out[strings.ToUpper(item.Symbol)] = item.FundingIntervalHours
		}
	}
	return out
}

func (c *CEXMarketClient) Start(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	if !c.Enabled() {
		return
	}
	go c.runMarkPriceLoop(ctx, provider.FundingSymbols(c.name), sink)
	go c.runBookTickerLoop(ctx, provider, sink)
}

// runMarkPriceLoop 使用交易所提供的全市场 mark price 流。
// 这条流的成本相对较低，适合承担“全市场 funding 粗筛”职责。
func (c *CEXMarketClient) runMarkPriceLoop(ctx context.Context, symbols []entity.Symbol, sink MarketSink) {
	if len(symbols) == 0 {
		return
	}
	base := strings.TrimRight(c.cfg.MarketWSBaseURL, "/")
	if base == "" {
		base = strings.TrimRight(c.cfg.PublicWSBaseURL, "/")
	}
	endpoint := base + "/ws/!markPrice@arr@1s"
	watch := makeSymbolWatch(symbols)
	c.readLoop(ctx, endpoint, true, watch, nil, sink)
}

// runBookTickerLoop 维护一条长期存活的全市场 !bookTicker 连接。
// 设计目的：
// 1. 不再因为深扫池变化而断开/重建 websocket；
// 2. 通过全市场流降低代理握手失败和超长 URL 的风险；
// 3. 本地过滤集合（watch）按固定周期刷新，从而兼容动态深扫池变化；
// 4. 连接成本固定，是否写入 store 由当前 watch 决定。
func (c *CEXMarketClient) runBookTickerLoop(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	base := strings.TrimRight(c.cfg.PublicWSBaseURL, "/")
	endpoint := base + "/ws/!bookTicker"
	runDynamicPublicStream(ctx, dynamicPublicStreamConfig[map[string]entity.Symbol]{
		Endpoint: endpoint,
		Dial: func(ctx context.Context, endpoint string) (*websocket.Conn, error) {
			conn, _, err := c.wsDialer.DialContext(ctx, endpoint, nil)
			return conn, err
		},
		ResolveSnapshot: func() (map[string]entity.Symbol, bool) {
			watch := c.resolveBookWatch(provider)
			return watch, len(watch) > 0
		},
		OnConnected: func() {
			c.updateStatus(func(s *ConnectorStatus) { s.LastError = "" })
			sink.UpdateStatus(c.currentStatus())
		},
		OnDisconnected: func(err error) {
			c.logger.Warn("dynamic_public_stream_stopped",
				slog.String("exchange", c.name),
				slog.String("kind", "book"),
				slog.Any("err", err),
			)
			c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
			sink.UpdateStatus(c.currentStatus())
		},
		OnMessage: func(msg []byte, watch map[string]entity.Symbol) {
			c.handleBookTickerMessage(msg, watch, sink)
		},
		ReadTimeout: 30 * time.Second,
	})
}

// readLoop 是 CEX 公共的 websocket 读取循环。
// mark price 这类“订阅集合不会频繁变”的流，继续使用这条通用实现即可。
func (c *CEXMarketClient) readLoop(ctx context.Context, endpoint string, markPrice bool, watch map[string]entity.Symbol, reconnectCheck func() bool, sink MarketSink) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, _, err := c.wsDialer.DialContext(ctx, endpoint, nil)
		if err != nil {
			c.logger.Warn("ws_dial_failed", slog.String("exchange", c.name), slog.Any("err", err))
			c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
			sink.UpdateStatus(c.currentStatus())
			time.Sleep(backoff)
			if backoff < 15*time.Second {
				backoff *= 2
			}
			continue
		}
		backoff = time.Second
		configureWebSocketReadDeadline(conn, 10*time.Second)
		c.updateStatus(func(s *ConnectorStatus) { s.LastError = "" })
		sink.UpdateStatus(c.currentStatus())
		for {
			if reconnectCheck != nil && reconnectCheck() {
				_ = conn.Close()
				break
			}
			_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
			_, msg, err := conn.ReadMessage()
			if err != nil {
				_ = conn.Close()
				c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
				sink.UpdateStatus(c.currentStatus())
				break
			}
			if markPrice {
				c.handleMarkPriceMessage(msg, watch, sink)
			} else {
				c.handleBookTickerMessage(msg, watch, sink)
			}
		}
	}
}

type markPriceMessage struct {
	EventType            string `json:"e"`
	EventTime            int64  `json:"E"`
	Symbol               string `json:"s"`
	MarkPrice            string `json:"p"`
	IndexPrice           string `json:"i"`
	EstimatedSettlePrice string `json:"P"`
	FundingRate          string `json:"r"`
	FundingTime          int64  `json:"T"`
}

type streamEnvelope struct {
	Stream string          `json:"stream"`
	Data   json.RawMessage `json:"data"`
}

func (c *CEXMarketClient) handleMarkPriceMessage(msg []byte, watch map[string]entity.Symbol, sink MarketSink) {
	var env streamEnvelope
	payload := msg
	if err := json.Unmarshal(msg, &env); err == nil && len(env.Data) > 0 {
		payload = env.Data
	}

	// 全市场 markPrice 流可能返回数组，也可能返回单对象；两种都兼容。
	var arr []markPriceMessage
	if err := json.Unmarshal(payload, &arr); err == nil && len(arr) > 0 {
		for _, item := range arr {
			meta, ok := watch[strings.ToUpper(item.Symbol)]
			if !ok {
				continue
			}
			sink.UpsertFunding(entity.FundingSnapshot{
				Exchange:             c.name,
				Symbol:               meta.Symbol,
				VenueSymbol:          meta.VenueSymbol,
				MarkPrice:            mustFloat(item.MarkPrice),
				IndexPrice:           mustFloat(item.IndexPrice),
				EstimatedSettlePrice: mustFloat(item.EstimatedSettlePrice),
				FundingRate:          mustFloat(item.FundingRate),
				FundingTimeMs:        item.FundingTime,
				FundingIntervalHours: meta.FundingIntervalHours,
				EventTimeMs:          item.EventTime,
			})
		}
		return
	}

	var single markPriceMessage
	if err := json.Unmarshal(payload, &single); err == nil && single.Symbol != "" {
		meta, ok := watch[strings.ToUpper(single.Symbol)]
		if !ok {
			return
		}
		sink.UpsertFunding(entity.FundingSnapshot{
			Exchange:             c.name,
			Symbol:               meta.Symbol,
			VenueSymbol:          meta.VenueSymbol,
			MarkPrice:            mustFloat(single.MarkPrice),
			IndexPrice:           mustFloat(single.IndexPrice),
			EstimatedSettlePrice: mustFloat(single.EstimatedSettlePrice),
			FundingRate:          mustFloat(single.FundingRate),
			FundingTimeMs:        single.FundingTime,
			FundingIntervalHours: meta.FundingIntervalHours,
			EventTimeMs:          single.EventTime,
		})
		return
	}

	c.logger.Warn("mark_price_unmarshal_failed", slog.String("exchange", c.name), slog.String("payload", string(payload)))
}

type bookTickerMessage struct {
	EventType       string `json:"e"`
	UpdateID        int64  `json:"u"`
	EventTime       int64  `json:"E"`
	TransactionTime int64  `json:"T"`
	Symbol          string `json:"s"`
	BidPrice        string `json:"b"`
	BidQty          string `json:"B"`
	AskPrice        string `json:"a"`
	AskQty          string `json:"A"`
}

// handleBookTickerMessage 负责解析交易所返回的 best bid / ask 消息。
// 兼容两种输入：
// 1. raw websocket payload: {...}
// 2. combined stream payload: {"stream":"...","data":{...}}
//
// 注意：
// - 当前 CEX 已改成全市场 !bookTicker 连接，所以这里的 watch 只是“本地过滤集合”。
// - 只有命中 watch 的 symbol，才会写入 BookTopSnapshot。
// - 这样可以做到：连接成本固定，但 store 只保留当前策略需要的盘口。
func (c *CEXMarketClient) handleBookTickerMessage(msg []byte, watch map[string]entity.Symbol, sink MarketSink) {
	if len(watch) == 0 {
		return
	}

	var env streamEnvelope
	payload := msg
	if err := json.Unmarshal(msg, &env); err == nil && len(env.Data) > 0 {
		payload = env.Data
	}

	var item bookTickerMessage
	if err := json.Unmarshal(payload, &item); err != nil {
		c.logger.Warn("book_ticker_unmarshal_failed",
			slog.String("exchange", c.name),
			slog.String("payload", string(payload)),
			slog.String("error", err.Error()),
		)
		return
	}

	if item.Symbol == "" {
		c.logger.Warn("book_ticker_empty_symbol",
			slog.String("exchange", c.name),
			slog.String("payload", string(payload)),
		)
		return
	}

	meta, ok := watch[strings.ToUpper(item.Symbol)]
	if !ok {
		return
	}

	eventTimeMs := item.EventTime
	if eventTimeMs <= 0 {
		eventTimeMs = item.TransactionTime
	}

	sink.UpsertBookTop(entity.BookTopSnapshot{
		Exchange:    c.name,
		Symbol:      meta.Symbol,
		VenueSymbol: meta.VenueSymbol,
		BidPrice:    mustFloat(item.BidPrice),
		BidQty:      mustFloat(item.BidQty),
		AskPrice:    mustFloat(item.AskPrice),
		AskQty:      mustFloat(item.AskQty),
		EventTimeMs: eventTimeMs,
	})
}

// resolveBookWatch 返回“当前需要写入盘口快照的 symbol 集合”。
// 注意：
// 1. 它现在只负责本地过滤，不再参与 websocket 订阅签名比较；
// 2. 因为 CEX 已改成全市场 !bookTicker，连接本身不再随深扫池变化而重建；
// 3. key 使用 VenueSymbol（如 BTCUSDT），这样能直接匹配交易所 websocket 返回的原始 symbol。
func (c *CEXMarketClient) resolveBookWatch(provider MarketSubscriptionProvider) map[string]entity.Symbol {
	symbols := provider.BookSymbols(c.name)
	watch := make(map[string]entity.Symbol, len(symbols))
	for _, item := range symbols {
		watch[strings.ToUpper(item.VenueSymbol)] = item
	}
	return watch
}

func makeSymbolWatch(symbols []entity.Symbol) map[string]entity.Symbol {
	watch := make(map[string]entity.Symbol, len(symbols))
	for _, item := range symbols {
		watch[strings.ToUpper(item.VenueSymbol)] = item
	}
	return watch
}

func symbolSignature(symbols []entity.Symbol) string {
	keys := make([]string, 0, len(symbols))
	for _, item := range symbols {
		keys = append(keys, strings.ToUpper(item.VenueSymbol))
	}
	sort.Strings(keys)
	return strings.Join(keys, ",")
}

func isTimeoutErr(err error) bool {
	if err == nil {
		return false
	}
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

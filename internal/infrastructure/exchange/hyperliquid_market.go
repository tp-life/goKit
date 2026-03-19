package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"goKit/internal/domain/entity"

	"github.com/gorilla/websocket"
)

// HyperliquidMarketClient 采用和 Binance-like CEX 不同的分层方式：
// - metaAndAssetCtxs: 全市场 funding / mark / oracle / mid，适合粗筛
// - bbo: 按 coin 订阅的 best bid / ask，适合深扫
//
// 这正好匹配“全市场粗筛 + 候选深扫”的策略设计。
type HyperliquidMarketClient struct {
	baseStatusHolder
	name       string
	cfg        ExchangeConfig
	logger     *slog.Logger
	httpClient *http.Client
	wsDialer   *websocket.Dialer
}

func NewHyperliquidMarketClient(cfg ConfigSet, logger *slog.Logger) MarketAdapter {
	return NewHyperliquidMarketAdapter("hyperliquid", cfg.Hyperliquid, logger)
}

func NewHyperliquidMarketAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) MarketAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		c.RestBaseURL = "https://api.hyperliquid.xyz"
	}
	if c.PublicWSBaseURL == "" {
		c.PublicWSBaseURL = "wss://api.hyperliquid.xyz/ws"
	}
	appCfg := loadAppConfig()
	return &HyperliquidMarketClient{
		name:             name,
		cfg:              c,
		logger:           logger,
		httpClient:       newHTTPClient(c, appCfg, logger, name),
		wsDialer:         newWebSocketDialer(c, appCfg, logger, name),
		baseStatusHolder: baseStatusHolder{status: ConnectorStatus{Exchange: name}},
	}
}

func (c *HyperliquidMarketClient) Name() string           { return c.name }
func (c *HyperliquidMarketClient) Enabled() bool          { return c.cfg.Enabled }
func (c *HyperliquidMarketClient) Fees() FeeConfig        { return c.cfg.Fees }
func (c *HyperliquidMarketClient) Config() ExchangeConfig { return c.cfg }

func (c *HyperliquidMarketClient) FetchTradableSymbols(ctx context.Context, quoteAsset string, allowed map[string]struct{}) ([]entity.Symbol, error) {
	meta, _, err := c.fetchMetaAndAssetCtxs(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]entity.Symbol, 0, len(meta.Universe))
	for idx, item := range meta.Universe {
		canonical := strings.ToUpper(strings.TrimSpace(item.Name))
		if canonical == "" {
			continue
		}
		if !normalizeAllowed(allowed, canonical) {
			continue
		}
		out = append(out, entity.Symbol{
			Exchange:             c.name,
			Symbol:               canonical,
			VenueSymbol:          canonical,
			BaseAsset:            canonical,
			QuoteAsset:           strings.ToUpper(c.cfg.SettleAsset),
			SettleAsset:          strings.ToUpper(c.cfg.SettleAsset),
			Status:               "TRADING",
			ContractType:         "PERPETUAL",
			TickSize:             "0.0001",
			StepSize:             stepFromDecimals(item.SzDecimals),
			MinQty:               stepFromDecimals(item.SzDecimals),
			MinNotional:          "10",
			FundingIntervalHours: c.cfg.DefaultFundingIntervalHours,
			VenueAssetID:         fmt.Sprintf("%d", idx),
			Enabled:              true,
			Watched:              true,
		})
	}
	return out, nil
}

func (c *HyperliquidMarketClient) Start(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	if !c.Enabled() {
		return
	}
	go c.runAssetCtxPollingLoop(ctx, provider.FundingSymbols(c.Name()), sink)
	go c.runBBOLoop(ctx, provider, sink)
}

type hyperMetaUniverseItem struct {
	Name        string `json:"name"`
	SzDecimals  int    `json:"szDecimals"`
	MaxLeverage int    `json:"maxLeverage"`
}

type hyperMetaResponse struct {
	Universe []hyperMetaUniverseItem `json:"universe"`
}

type hyperAssetCtx struct {
	Funding  string `json:"funding"`
	MarkPx   string `json:"markPx"`
	OraclePx string `json:"oraclePx"`
	MidPx    string `json:"midPx"`
}

func (c *HyperliquidMarketClient) fetchMetaAndAssetCtxs(ctx context.Context) (hyperMetaResponse, []hyperAssetCtx, error) {
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + "/info"
	req, err := newJSONRequest(ctx, http.MethodPost, endpoint, map[string]any{"type": "metaAndAssetCtxs"})
	if err != nil {
		return hyperMetaResponse{}, nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return hyperMetaResponse{}, nil, err
	}
	defer resp.Body.Close()
	var raw []json.RawMessage
	if err := readJSONBody(resp, &raw); err != nil {
		return hyperMetaResponse{}, nil, err
	}
	if len(raw) < 2 {
		return hyperMetaResponse{}, nil, fmt.Errorf("unexpected metaAndAssetCtxs response")
	}
	var meta hyperMetaResponse
	if err := json.Unmarshal(raw[0], &meta); err != nil {
		return hyperMetaResponse{}, nil, err
	}
	var ctxs []hyperAssetCtx
	if err := json.Unmarshal(raw[1], &ctxs); err != nil {
		return hyperMetaResponse{}, nil, err
	}
	return meta, ctxs, nil
}

func nextFundingTimeMs(now time.Time) int64 {
	next := now.UTC().Truncate(time.Hour).Add(time.Hour)
	return next.UnixMilli()
}

// runAssetCtxPollingLoop 承担全市场 funding 粗筛职责。
// 这条链默认覆盖整个基础池，因为 metaAndAssetCtxs 的成本远低于逐 coin 盘口订阅。
func (c *HyperliquidMarketClient) runAssetCtxPollingLoop(ctx context.Context, symbols []entity.Symbol, sink MarketSink) {
	if len(symbols) == 0 {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	watch := make(map[string]entity.Symbol, len(symbols))
	for _, item := range symbols {
		watch[strings.ToUpper(item.VenueSymbol)] = item
	}
	for {
		meta, ctxs, err := c.fetchMetaAndAssetCtxs(ctx)
		if err != nil {
			c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
			sink.UpdateStatus(c.currentStatus())
		} else {
			now := time.Now().UTC()
			for idx, u := range meta.Universe {
				item, ok := watch[strings.ToUpper(u.Name)]
				if !ok || idx >= len(ctxs) {
					continue
				}
				ctxItem := ctxs[idx]
				sink.UpsertFunding(entity.FundingSnapshot{
					Exchange:             c.name,
					Symbol:               item.Symbol,
					VenueSymbol:          item.VenueSymbol,
					MarkPrice:            mustFloat(ctxItem.MarkPx),
					IndexPrice:           mustFloat(ctxItem.OraclePx),
					EstimatedSettlePrice: mustFloat(ctxItem.MidPx),
					FundingRate:          mustFloat(ctxItem.Funding),
					FundingTimeMs:        nextFundingTimeMs(now),
					FundingIntervalHours: c.cfg.DefaultFundingIntervalHours,
					EventTimeMs:          now.UnixMilli(),
				})
			}
			c.updateStatus(func(s *ConnectorStatus) {
				s.LastError = ""
				s.MarkPriceConnected = true
				s.LastMarketEventAt = time.Now().UTC()
			})
			sink.UpdateStatus(c.currentStatus())
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

type hyperWSMessage struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

type hyperLevel struct {
	Px string `json:"px"`
	Sz string `json:"sz"`
	N  int    `json:"n"`
}

type hyperBBOData struct {
	Coin string         `json:"coin"`
	Time int64          `json:"time"`
	BBO  [2]*hyperLevel `json:"bbo"`
}

// runBBOLoop 只为当前深扫池维护 Hyperliquid 的 bbo 订阅。
//
// 与早期“深扫池一变就整体断开重连”的做法不同，
// 这里改成：
// - 连接建立后尽量长期复用；
// - 深扫池变化时，只发送 subscribe / unsubscribe 增量指令；
// - 这样可以明显降低 websocket 抖动和代理层握手压力。
func (c *HyperliquidMarketClient) runBBOLoop(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	backoff := time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		desired := makeSymbolWatch(provider.BookSymbols(c.Name()))
		if len(desired) == 0 {
			time.Sleep(2 * time.Second)
			continue
		}

		conn, _, err := c.wsDialer.DialContext(ctx, c.cfg.PublicWSBaseURL, nil)
		if err != nil {
			c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
			sink.UpdateStatus(c.currentStatus())
			time.Sleep(backoff)
			if backoff < 15*time.Second {
				backoff *= 2
			}
			continue
		}

		currentSubs := make(map[string]entity.Symbol)
		if err := c.syncBBOSubscriptions(conn, currentSubs, desired); err != nil {
			_ = conn.Close()
			c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
			sink.UpdateStatus(c.currentStatus())
			time.Sleep(backoff)
			continue
		}
		backoff = time.Second

		for {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if isTimeoutErr(err) {
					desired = makeSymbolWatch(provider.BookSymbols(c.Name()))
					if err := c.syncBBOSubscriptions(conn, currentSubs, desired); err != nil {
						_ = conn.Close()
						c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
						sink.UpdateStatus(c.currentStatus())
						break
					}
					continue
				}
				_ = conn.Close()
				c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
				sink.UpdateStatus(c.currentStatus())
				break
			}

			var packet hyperWSMessage
			if err := json.Unmarshal(msg, &packet); err != nil {
				continue
			}
			if packet.Channel != "bbo" {
				continue
			}
			var data hyperBBOData
			if err := json.Unmarshal(packet.Data, &data); err != nil {
				continue
			}
			meta, ok := currentSubs[strings.ToUpper(data.Coin)]
			if !ok {
				continue
			}
			var bidPx, bidQty, askPx, askQty float64
			if data.BBO[0] != nil {
				bidPx, bidQty = mustFloat(data.BBO[0].Px), mustFloat(data.BBO[0].Sz)
			}
			if data.BBO[1] != nil {
				askPx, askQty = mustFloat(data.BBO[1].Px), mustFloat(data.BBO[1].Sz)
			}
			if bidPx <= 0 && askPx <= 0 {
				continue
			}
			sink.UpsertBookTop(entity.BookTopSnapshot{
				Exchange:    c.name,
				Symbol:      meta.Symbol,
				VenueSymbol: meta.VenueSymbol,
				BidPrice:    bidPx,
				BidQty:      bidQty,
				AskPrice:    askPx,
				AskQty:      askQty,
				EventTimeMs: int64(math.Max(float64(data.Time), float64(time.Now().UTC().UnixMilli()))),
			})
			c.updateStatus(func(s *ConnectorStatus) {
				s.LastError = ""
				s.BookTickerConnected = true
				s.LastBookEventAt = time.Now().UTC()
			})
			sink.UpdateStatus(c.currentStatus())
		}
	}
}

// syncBBOSubscriptions 将“当前已订阅集合”同步到“目标订阅集合”。
// 只发送差异部分，避免每轮深扫池变化都断开 websocket 重连。
func (c *HyperliquidMarketClient) syncBBOSubscriptions(conn *websocket.Conn, current, desired map[string]entity.Symbol) error {
	for coin := range current {
		if _, ok := desired[coin]; ok {
			continue
		}
		if err := conn.WriteJSON(map[string]any{"method": "unsubscribe", "subscription": map[string]any{"type": "bbo", "coin": coin}}); err != nil {
			return err
		}
		delete(current, coin)
	}
	for coin, meta := range desired {
		if _, ok := current[coin]; ok {
			continue
		}
		if err := conn.WriteJSON(map[string]any{"method": "subscribe", "subscription": map[string]any{"type": "bbo", "coin": coin}}); err != nil {
			return err
		}
		current[coin] = meta
	}
	return nil
}

package exchange

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"goKit/internal/domain/entity"
)

const binanceSpotBookTickerPollInterval = 3 * time.Second

type BinanceSpotMarketClient struct {
	baseStatusHolder
	name       string
	cfg        ExchangeConfig
	logger     *slog.Logger
	httpClient *http.Client
}

type binanceSpotExchangeInfoSymbol struct {
	Symbol               string               `json:"symbol"`
	Status               string               `json:"status"`
	BaseAsset            string               `json:"baseAsset"`
	QuoteAsset           string               `json:"quoteAsset"`
	IsSpotTradingAllowed bool                 `json:"isSpotTradingAllowed"`
	Filters              []exchangeInfoFilter `json:"filters"`
}

type binanceSpotExchangeInfoResponse struct {
	Symbols []binanceSpotExchangeInfoSymbol `json:"symbols"`
}

type binanceSpotBookTicker struct {
	Symbol   string `json:"symbol"`
	BidPrice string `json:"bidPrice"`
	BidQty   string `json:"bidQty"`
	AskPrice string `json:"askPrice"`
	AskQty   string `json:"askQty"`
}

func NewBinanceSpotMarketAdapter(name string, cfg ExchangeConfig, logger *slog.Logger) MarketAdapter {
	c := normalizeExchangeConfig(name, cfg)
	if c.RestBaseURL == "" {
		c.RestBaseURL = "https://api.binance.com"
	}
	appCfg := loadAppConfig()
	return &BinanceSpotMarketClient{
		name:             name,
		cfg:              c,
		logger:           logger,
		httpClient:       newHTTPClient(c, appCfg, logger, name+"-spot-market"),
		baseStatusHolder: baseStatusHolder{status: ConnectorStatus{Exchange: name}},
	}
}

func (c *BinanceSpotMarketClient) Name() string           { return c.name }
func (c *BinanceSpotMarketClient) Enabled() bool          { return c.cfg.Enabled }
func (c *BinanceSpotMarketClient) Fees() FeeConfig        { return c.cfg.Fees }
func (c *BinanceSpotMarketClient) Config() ExchangeConfig { return c.cfg }

func (c *BinanceSpotMarketClient) FetchTradableSymbols(ctx context.Context, quoteAsset string, allowed map[string]struct{}) ([]entity.Symbol, error) {
	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + "/api/v3/exchangeInfo"
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
		return nil, fmt.Errorf("%s spot exchangeInfo status=%d body=%s", c.name, resp.StatusCode, string(body))
	}

	var payload binanceSpotExchangeInfoResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}

	selected := make(map[string]entity.Symbol, len(payload.Symbols))
	for _, sym := range payload.Symbols {
		if quoteAsset != "" && !strings.EqualFold(sym.QuoteAsset, quoteAsset) {
			continue
		}
		if !strings.EqualFold(sym.Status, "TRADING") || !sym.IsSpotTradingAllowed {
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
			SettleAsset:          strings.ToUpper(firstNonEmpty(sym.QuoteAsset, c.cfg.SettleAsset)),
			Status:               strings.ToUpper(sym.Status),
			ContractType:         "SPOT",
			FundingIntervalHours: 0,
			VenueAssetID:         strings.ToUpper(sym.BaseAsset),
			Enabled:              true,
			Watched:              true,
		}
		execMeta := entity.SymbolExecutionMeta{}

		for _, f := range sym.Filters {
			switch f.FilterType {
			case "PRICE_FILTER":
				item.TickSize = f.TickSize
			case "LOT_SIZE":
				item.StepSize = firstNonEmpty(item.StepSize, f.StepSize)
				item.MinQty = firstNonEmpty(item.MinQty, f.MinQty)
				if maxQty := parseExchangeInfoFloat(f.MaxQty); maxQty > 0 {
					execMeta.LimitMaxQty = maxQty
					execMeta.MarketMaxQty = maxQty
				}
			case "MIN_NOTIONAL":
				item.MinNotional = firstNonEmpty(f.MinNotional, f.Notional)
			case "NOTIONAL":
				item.MinNotional = firstNonEmpty(item.MinNotional, f.MinNotional, f.Notional)
			}
		}
		if err := item.SetExecutionMeta(execMeta); err != nil {
			return nil, err
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

func (c *BinanceSpotMarketClient) Start(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	if !c.Enabled() {
		return
	}
	go c.runBookTickerPollingLoop(ctx, provider, sink)
}

func (c *BinanceSpotMarketClient) runBookTickerPollingLoop(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	ticker := time.NewTicker(binanceSpotBookTickerPollInterval)
	defer ticker.Stop()

	for {
		c.pollBookTickers(ctx, provider, sink)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (c *BinanceSpotMarketClient) pollBookTickers(ctx context.Context, provider MarketSubscriptionProvider, sink MarketSink) {
	desired := make(map[string]entity.Symbol)
	for _, item := range provider.FundingSymbols(c.name) {
		desired[strings.ToUpper(item.VenueSymbol)] = item
	}
	for _, item := range provider.BookSymbols(c.name) {
		desired[strings.ToUpper(item.VenueSymbol)] = item
	}
	if len(desired) == 0 {
		return
	}

	endpoint := strings.TrimRight(c.cfg.RestBaseURL, "/") + "/api/v3/ticker/bookTicker"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
		sink.UpdateStatus(c.currentStatus())
		return
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
		sink.UpdateStatus(c.currentStatus())
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
		sink.UpdateStatus(c.currentStatus())
		return
	}
	if resp.StatusCode >= 300 {
		c.updateStatus(func(s *ConnectorStatus) { s.LastError = fmt.Sprintf("status=%d body=%s", resp.StatusCode, string(body)) })
		sink.UpdateStatus(c.currentStatus())
		return
	}

	var payload []binanceSpotBookTicker
	if err := json.Unmarshal(body, &payload); err != nil {
		c.updateStatus(func(s *ConnectorStatus) { s.LastError = err.Error() })
		sink.UpdateStatus(c.currentStatus())
		return
	}

	now := time.Now().UTC()
	for _, item := range payload {
		meta, ok := desired[strings.ToUpper(item.Symbol)]
		if !ok {
			continue
		}
		sink.UpsertBookTop(entity.BookTopSnapshot{
			Exchange:    c.name,
			Symbol:      meta.Symbol,
			VenueSymbol: meta.VenueSymbol,
			BidPrice:    mustFloat(item.BidPrice),
			BidQty:      mustFloat(item.BidQty),
			AskPrice:    mustFloat(item.AskPrice),
			AskQty:      mustFloat(item.AskQty),
			EventTimeMs: now.UnixMilli(),
		})
	}

	c.updateStatus(func(s *ConnectorStatus) {
		s.BookTickerConnected = true
		s.LastBookEventAt = now
		s.LastError = ""
	})
	sink.UpdateStatus(c.currentStatus())
}

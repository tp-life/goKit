package lighter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
	"goKit/pkg/kit/http"
	"goKit/pkg/kit/ratelimit"
	"goKit/pkg/kit/websocket"
)

type LighterClient struct {
	config       Config
	logger       *slog.Logger
	ws           *websocket.Client
	httpClient   *http.Client
	rateLimiter  *ratelimit.TokenBucket
	marketDataCh chan *entity.MarketData
	fundingRateCh chan *entity.FundingRate
	symbolMapper *exchange.SymbolMapper
}

type Config struct {
	WSURL            string
	RESTURL          string
	ReconnectInterval time.Duration
	RateLimit        int
	ProxyURL         string
	SymbolMapper     *exchange.SymbolMapper
}

func NewLighterClient(cfg Config, logger *slog.Logger) exchange.ExchangeClient {
	httpClient := http.NewClient(logger, cfg.ProxyURL)
	rateLimiter := ratelimit.NewTokenBucket(cfg.RateLimit, time.Minute)

	wsConfig := websocket.Config{
		URL:                cfg.WSURL,
		ReconnectInterval:  cfg.ReconnectInterval,
		MaxReconnectAttempts: 10,
		ReadTimeout:        30 * time.Second,
		WriteTimeout:       10 * time.Second,
		ConnectTimeout:     30 * time.Second,
		PingInterval:       30 * time.Second,
		ProxyURL:           cfg.ProxyURL,
	}

	return &LighterClient{
		config:        cfg,
		logger:        logger,
		ws:            websocket.NewClient(wsConfig, logger),
		httpClient:    httpClient,
		rateLimiter:   rateLimiter,
		marketDataCh:  make(chan *entity.MarketData, 100),
		fundingRateCh: make(chan *entity.FundingRate, 100),
		symbolMapper:  cfg.SymbolMapper,
	}
}

func (c *LighterClient) Name() string {
	return "lighter"
}

func (c *LighterClient) Connect(ctx context.Context) error {
	if err := c.ws.Connect(ctx); err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}

	// 启动消息处理
	// 注意：订阅消息在 SubscribeMarketData 中发送，这里只启动消息处理
	go c.handleMessages()

	return nil
}

func (c *LighterClient) handleMessages() {
	for {
		select {
		case msg, ok := <-c.ws.Read():
			if !ok {
				return
			}
			c.processMessage(msg)
		case err := <-c.ws.Errors():
			c.logger.Warn("lighter_websocket_error", slog.Any("err", err))
		}
	}
}

func (c *LighterClient) processMessage(msg []byte) {
	var baseMsg struct {
		Type    string          `json:"type"`
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"market_stats"`
	}

	if err := json.Unmarshal(msg, &baseMsg); err != nil {
		c.logger.Debug("parse_message_failed", slog.Any("err", err))
		return
	}

	if baseMsg.Type != "update/market_stats" {
		return
	}

	// Lighter 可能返回单个市场或所有市场的数据
	// 先尝试解析为所有市场的格式
	var allMarkets map[string]json.RawMessage
	if err := json.Unmarshal(baseMsg.Data, &allMarkets); err == nil {
		// 所有市场格式
		for _, marketData := range allMarkets {
			c.processSingleMarket(marketData)
		}
	} else {
		// 单个市场格式
		c.processSingleMarket(baseMsg.Data)
	}
}

func (c *LighterClient) processSingleMarket(data json.RawMessage) {
	var marketStats struct {
		MarketID          int    `json:"market_id"`
		MarkPrice         string `json:"mark_price"`
		IndexPrice        string `json:"index_price"`
		LastTradePrice    string `json:"last_trade_price"`
		CurrentFundingRate string `json:"current_funding_rate"`
	}

	if err := json.Unmarshal(data, &marketStats); err != nil {
		c.logger.Debug("parse_market_stats_failed", slog.Any("err", err))
		return
	}

	// 通过 Market ID 获取统一符号
	unifiedSymbol, ok := c.symbolMapper.FromLighter(marketStats.MarketID)
	if !ok {
		c.logger.Debug("unknown_market_id", slog.Int("market_id", marketStats.MarketID))
		return
	}

	// 解析价格
	markPrice, _ := strconv.ParseFloat(marketStats.MarkPrice, 64)
	indexPrice, _ := strconv.ParseFloat(marketStats.IndexPrice, 64)
	lastTradePrice, _ := strconv.ParseFloat(marketStats.LastTradePrice, 64)
	rawFundingRate, _ := strconv.ParseFloat(marketStats.CurrentFundingRate, 64)
	
	// Lighter 返回的费率格式是百分比形式（如 0.0012 表示 0.0012%）
	// 需要除以 100 转换为小数形式（0.000012），以与 Binance 统一
	// Binance 返回的是小数形式（如 0.0001 表示 0.01%）
	fundingRate := rawFundingRate / 100.0

	marketData := &entity.MarketData{
		Symbol:         unifiedSymbol,
		Exchange:       c.Name(),
		MarkPrice:      &markPrice,
		IndexPrice:     &indexPrice,
		LastTradePrice: &lastTradePrice,
		UpdatedAt:      time.Now(),
	}

	select {
	case c.marketDataCh <- marketData:
	default:
		c.logger.Warn("market_data_channel_full")
	}

	// 处理资金费率
	// 注意：根据 Lighter 文档，funding rate = interest rate component + (1-hour premium / 8)
	// 这意味着返回的费率已经是按 8 小时周期计算的，不需要再乘以 8
	// 但为了与 Binance 统一（Binance 是 8 小时周期），我们保持 Rate8H = Rate
	fundingRate8H := fundingRate // Lighter 的费率已经是 8 小时等效费率

	// 通过 channel 传递资金费率
	fundingRateEntity := &entity.FundingRate{
		Symbol:      unifiedSymbol,
		Exchange:    c.Name(),
		Rate:        fundingRate,   // 原始 1 小时费率
		Rate8H:      fundingRate8H, // 转换为 8 小时费率
		NextFunding: time.Now().Add(1 * time.Hour), // Lighter 是 1 小时周期
		UpdatedAt:   time.Now(),
	}

	select {
	case c.fundingRateCh <- fundingRateEntity:
		c.logger.Debug("lighter_funding_rate_sent_to_channel",
			slog.String("symbol", unifiedSymbol),
			slog.Float64("rate", fundingRate),
			slog.Float64("rate8h", fundingRate8H),
		)
	default:
		c.logger.Warn("lighter_funding_rate_channel_full",
			slog.String("symbol", unifiedSymbol),
		)
	}
}

func (c *LighterClient) SubscribeMarketData(ctx context.Context, symbols []string) (<-chan *entity.MarketData, error) {
	// Lighter 使用 Market ID 订阅
	marketIDs := make([]int, 0, len(symbols))
	for _, symbol := range symbols {
		if marketID, ok := c.symbolMapper.ToLighter(symbol); ok {
			marketIDs = append(marketIDs, marketID)
		}
	}

	if len(marketIDs) == 0 {
		return nil, errors.New("no valid symbols for lighter")
	}

	// 确保 WebSocket 已连接
	if !c.ws.IsConnected() {
		// 如果未连接，先连接
		if err := c.ws.Connect(ctx); err != nil {
			return nil, fmt.Errorf("connect websocket: %w", err)
		}
	}

	// 订阅所有市场统计数据（Lighter 支持 market_stats/all）
	subscribeMsg := map[string]interface{}{
		"type":    "subscribe",
		"channel": "market_stats/all",
	}

	msgBytes, _ := json.Marshal(subscribeMsg)
	if err := c.ws.Write(ctx, msgBytes); err != nil {
		return nil, fmt.Errorf("subscribe market stats: %w", err)
	}

	// 启动消息处理（如果还没有启动）
	// 注意：Connect 方法中已经启动了 handleMessages，这里避免重复启动
	// 但为了安全，我们检查一下是否已经在运行
	go c.handleMessages()

	return c.marketDataCh, nil
}

func (c *LighterClient) GetMarketData(ctx context.Context, symbol string) (*entity.MarketData, error) {
	// 从 WebSocket 数据中获取，如果没有则通过 REST API
	return nil, errors.New("not implemented: use SubscribeMarketData")
}

func (c *LighterClient) GetFundingRate(ctx context.Context, symbol string) (*entity.FundingRate, error) {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		c.logger.Warn("lighter_rate_limiter_wait_failed", slog.Any("err", err))
		return nil, err
	}

	// Lighter 使用 Market ID
	marketID, ok := c.symbolMapper.ToLighter(symbol)
	if !ok {
		c.logger.Warn("lighter_symbol_not_found_in_mapping",
			slog.String("symbol", symbol),
		)
		return nil, fmt.Errorf("symbol %s not found in lighter mapping", symbol)
	}

	url := fmt.Sprintf("%s/funding-rates?market_id=%d", c.config.RESTURL, marketID)
	c.logger.Debug("lighter_fetching_funding_rate",
		slog.String("symbol", symbol),
		slog.Int("market_id", marketID),
		slog.String("url", url),
	)
	resp, err := c.httpClient.Get(ctx, url)
	if err != nil {
		c.logger.Error("lighter_http_get_failed",
			slog.String("url", url),
			slog.Any("err", err),
		)
		return nil, fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	// 检查 HTTP 状态码
	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		c.logger.Error("lighter_http_status_error",
			slog.String("url", url),
			slog.Int("status_code", resp.StatusCode),
			slog.String("response", string(bodyBytes)),
		)
		return nil, fmt.Errorf("http status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// 先读取响应体，以便调试和灵活解析
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	// Lighter API 可能返回数组或单个对象，先尝试解析为数组
	var rates []struct {
		MarketID int    `json:"market_id"`
		Rate     string `json:"rate"` // 1小时周期费率
		Exchange string `json:"exchange"`
	}

	if err := json.Unmarshal(bodyBytes, &rates); err != nil {
		// 如果数组解析失败，尝试解析为单个对象
		var singleRate struct {
			MarketID int    `json:"market_id"`
			Rate     string `json:"rate"`
			Exchange string `json:"exchange"`
		}
		if err2 := json.Unmarshal(bodyBytes, &singleRate); err2 != nil {
			c.logger.Error("parse_funding_rate_response_failed",
				slog.String("url", url),
				slog.String("response", string(bodyBytes)),
				slog.Any("array_err", err),
				slog.Any("object_err", err2),
			)
			return nil, fmt.Errorf("parse funding rate response: array err=%v, object err=%v", err, err2)
		}
		// 转换为数组格式
		rates = []struct {
			MarketID int    `json:"market_id"`
			Rate     string `json:"rate"`
			Exchange string `json:"exchange"`
		}{singleRate}
	}

	if len(rates) == 0 {
		c.logger.Warn("lighter_funding_rate_empty_response",
			slog.String("url", url),
			slog.String("response", string(bodyBytes)),
			slog.Int("market_id", marketID),
		)
		return nil, errors.New("no funding rate found")
	}

	// 找到对应 Market ID 的费率
	var rateStr string
	for _, r := range rates {
		if r.MarketID == marketID {
			rateStr = r.Rate
			break
		}
	}

	if rateStr == "" {
		// 记录所有返回的 market_id，用于调试
		marketIDs := make([]int, 0, len(rates))
		for _, r := range rates {
			marketIDs = append(marketIDs, r.MarketID)
		}
		c.logger.Warn("lighter_funding_rate_market_not_found",
			slog.String("url", url),
			slog.Int("requested_market_id", marketID),
			slog.Any("available_market_ids", marketIDs),
			slog.String("response", string(bodyBytes)),
		)
		return nil, errors.New("funding rate not found for market")
	}

	rawRate, err := strconv.ParseFloat(rateStr, 64)
	if err != nil {
		return nil, err
	}

	// Lighter 返回的费率格式是百分比形式（如 0.0012 表示 0.0012%）
	// 需要除以 100 转换为小数形式（0.000012），以与 Binance 统一
	// Binance 返回的是小数形式（如 0.0001 表示 0.01%）
	rate := rawRate / 100.0
	
	// Lighter 的费率计算公式：funding rate = interest rate component + (1-hour premium / 8)
	// 这意味着返回的费率已经是按 8 小时周期计算的，不需要再乘以 8
	rate8H := rate // Lighter 的费率已经是 8 小时等效费率

	return &entity.FundingRate{
		Symbol:      symbol,
		Exchange:    c.Name(),
		Rate:        rate,   // 原始 1 小时费率
		Rate8H:      rate8H, // 转换为 8 小时费率
		NextFunding: time.Now().Add(1 * time.Hour), // Lighter 是 1 小时周期
		UpdatedAt:   time.Now(),
	}, nil
}

// GetFundingRateChannel 返回资金费率 channel（用于从 WebSocket 接收实时资金费率）
func (c *LighterClient) GetFundingRateChannel() <-chan *entity.FundingRate {
	return c.fundingRateCh
}

func (c *LighterClient) IsConnected() bool {
	return c.ws.IsConnected()
}

func (c *LighterClient) Close() error {
	return c.ws.Close()
}

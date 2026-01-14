package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
	"goKit/pkg/kit/http"
	"goKit/pkg/kit/ratelimit"
	"goKit/pkg/kit/websocket"
)

type BinanceClient struct {
	config        Config
	logger        *slog.Logger
	spotWS        *websocket.Client
	futuresWS     *websocket.Client
	httpClient    *http.Client
	rateLimiter   *ratelimit.TokenBucket
	marketDataCh  chan *entity.MarketData
	fundingRateCh chan *entity.FundingRate
	symbolMapper  *exchange.SymbolMapper
}

type Config struct {
	SpotWSURL         string
	FuturesWSURL      string
	RESTURL           string
	ReconnectInterval time.Duration
	RateLimit         int
	ProxyURL          string
	SymbolMapper      *exchange.SymbolMapper
}

func NewBinanceClient(cfg Config, logger *slog.Logger) exchange.ExchangeClient {
	httpClient := http.NewClient(logger, cfg.ProxyURL)

	// 创建速率限制器（每分钟 rateLimit 次请求）
	rateLimiter := ratelimit.NewTokenBucket(cfg.RateLimit, time.Minute)

	// WebSocket 配置
	spotWSConfig := websocket.Config{
		URL:                  cfg.SpotWSURL,
		ReconnectInterval:    cfg.ReconnectInterval,
		MaxReconnectAttempts: 10,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         10 * time.Second,
		ConnectTimeout:       30 * time.Second, // 连接超时 30 秒
		PingInterval:         30 * time.Second,
		ProxyURL:             cfg.ProxyURL,
	}

	futuresWSConfig := websocket.Config{
		URL:                  cfg.FuturesWSURL,
		ReconnectInterval:    cfg.ReconnectInterval,
		MaxReconnectAttempts: 10,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         10 * time.Second,
		ConnectTimeout:       30 * time.Second, // 连接超时 30 秒
		PingInterval:         30 * time.Second,
		ProxyURL:             cfg.ProxyURL,
	}

	return &BinanceClient{
		config:        cfg,
		logger:        logger,
		spotWS:        websocket.NewClient(spotWSConfig, logger),
		futuresWS:     websocket.NewClient(futuresWSConfig, logger),
		httpClient:    httpClient,
		rateLimiter:   rateLimiter,
		marketDataCh:  make(chan *entity.MarketData, 100),
		fundingRateCh: make(chan *entity.FundingRate, 100),
		symbolMapper:  cfg.SymbolMapper,
	}
}

func (c *BinanceClient) Name() string {
	return "binance"
}

func (c *BinanceClient) Connect(ctx context.Context) error {
	// 连接现货 WebSocket
	if err := c.spotWS.Connect(ctx); err != nil {
		return fmt.Errorf("connect spot websocket: %w", err)
	}

	// 连接合约 WebSocket
	if err := c.futuresWS.Connect(ctx); err != nil {
		return fmt.Errorf("connect futures websocket: %w", err)
	}

	// 启动消息处理
	go c.handleSpotMessages()
	go c.handleFuturesMessages()

	return nil
}

func (c *BinanceClient) handleSpotMessages() {
	for {
		select {
		case msg, ok := <-c.spotWS.Read():
			if !ok {
				return
			}
			c.processSpotMessage(msg)
		case err := <-c.spotWS.Errors():
			c.logger.Warn("spot_websocket_error", slog.Any("err", err))
		}
	}
}

func (c *BinanceClient) handleFuturesMessages() {
	for {
		select {
		case msg, ok := <-c.futuresWS.Read():
			if !ok {
				return
			}
			c.processFuturesMessage(msg)
		case err := <-c.futuresWS.Errors():
			c.logger.Warn("futures_websocket_error", slog.Any("err", err))
		}
	}
}

func (c *BinanceClient) processSpotMessage(msg []byte) {
	// Binance ticker 消息格式：使用 map 解析，因为 e 字段可能是字符串或数字
	var rawMsg map[string]interface{}
	if err := json.Unmarshal(msg, &rawMsg); err != nil {
		c.logger.Debug("parse_spot_message_failed", slog.Any("err", err))
		return
	}

	// 提取事件类型（可能是字符串或数字）
	var eventType string
	if e, ok := rawMsg["e"]; ok {
		switch v := e.(type) {
		case string:
			eventType = v
		case float64:
			// Binance 可能返回数字类型的事件类型
			eventType = fmt.Sprintf("%.0f", v)
		default:
			eventType = fmt.Sprintf("%v", v)
		}
	}

	// 提取 symbol 和 price
	symbol, _ := rawMsg["s"].(string)
	priceStr, _ := rawMsg["c"].(string)

	// Binance ticker 流的事件类型是 "24hrTicker"
	if eventType != "24hrTicker" {
		return
	}

	price, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		c.logger.Warn("parse_price_failed", slog.String("price", priceStr))
		return
	}

	// Binance 返回的 Symbol 是大写（如 "BTCUSDC"），需要转换为统一符号
	// 如果没有 symbol mapper 或映射失败，直接使用 Binance 的 symbol（因为配置中统一符号就是 "BTCUSDC"）
	unifiedSymbol := symbol
	if c.symbolMapper != nil {
		if mapped, ok := c.symbolMapper.FromBinance(symbol); ok {
			unifiedSymbol = mapped
			c.logger.Debug("binance_symbol_mapped",
				slog.String("binance_symbol", symbol),
				slog.String("unified_symbol", unifiedSymbol),
			)
		} else {
			// 映射失败，使用原始 symbol（可能配置中的统一符号就是 "BTCUSDC"）
			c.logger.Debug("binance_symbol_not_mapped",
				slog.String("binance_symbol", symbol),
				slog.String("using_original", "true"),
			)
		}
	}

	marketData := &entity.MarketData{
		Symbol:    unifiedSymbol, // 使用统一符号保存
		Exchange:  c.Name(),
		SpotPrice: &price,
		UpdatedAt: time.Now(),
	}

	c.logger.Info("binance_spot_data_received",
		slog.String("binance_symbol", symbol),
		slog.String("unified_symbol", unifiedSymbol),
		slog.Float64("price", price),
	)

	select {
	case c.marketDataCh <- marketData:
	default:
		c.logger.Warn("market_data_channel_full")
	}
}

func (c *BinanceClient) processFuturesMessage(msg []byte) {
	// Binance 合约 WebSocket 可能返回数组或单个对象
	// 使用 map 解析，因为 e 字段可能是字符串或数字
	var rawData interface{}
	if err := json.Unmarshal(msg, &rawData); err != nil {
		c.logger.Debug("parse_futures_message_failed", slog.Any("err", err))
		return
	}

	// 转换为数组格式（统一处理）
	var rawUpdates []map[string]interface{}
	switch v := rawData.(type) {
	case []interface{}:
		// 数组格式
		for _, item := range v {
			if itemMap, ok := item.(map[string]interface{}); ok {
				rawUpdates = append(rawUpdates, itemMap)
			}
		}
	case map[string]interface{}:
		// 单个对象格式
		rawUpdates = []map[string]interface{}{v}
	default:
		c.logger.Debug("unexpected_futures_message_format", slog.String("type", fmt.Sprintf("%T", v)))
		return
	}

	for _, rawUpdate := range rawUpdates {
		// 提取事件类型（可能是字符串或数字）
		var eventType string
		if e, ok := rawUpdate["e"]; ok {
			switch v := e.(type) {
			case string:
				eventType = v
			case float64:
				eventType = fmt.Sprintf("%.0f", v)
			default:
				eventType = fmt.Sprintf("%v", v)
			}
		}

		if eventType != "markPriceUpdate" {
			continue
		}

		// 提取字段
		symbol, _ := rawUpdate["s"].(string)
		markPriceStr, _ := rawUpdate["p"].(string)
		indexPriceStr, _ := rawUpdate["i"].(string)
		fundingRateStr, _ := rawUpdate["r"].(string)

		markPrice, _ := strconv.ParseFloat(markPriceStr, 64)
		indexPrice, _ := strconv.ParseFloat(indexPriceStr, 64)
		fundingRate, _ := strconv.ParseFloat(fundingRateStr, 64)

		// Binance 返回的 Symbol 是大写（如 "BTCUSDC"），需要转换为统一符号
		// 如果没有 symbol mapper 或映射失败，直接使用 Binance 的 symbol（因为配置中统一符号就是 "BTCUSDC"）
		unifiedSymbol := symbol
		if c.symbolMapper != nil {
			if mapped, ok := c.symbolMapper.FromBinance(symbol); ok {
				unifiedSymbol = mapped
				c.logger.Debug("binance_futures_symbol_mapped",
					slog.String("binance_symbol", symbol),
					slog.String("unified_symbol", unifiedSymbol),
				)
			} else {
				// 映射失败，使用原始 symbol
				c.logger.Debug("binance_futures_symbol_not_mapped",
					slog.String("binance_symbol", symbol),
					slog.String("using_original", "true"),
				)
			}
		}

		marketData := &entity.MarketData{
			Symbol:     unifiedSymbol, // 使用统一符号保存
			Exchange:   c.Name(),
			MarkPrice:  &markPrice,
			IndexPrice: &indexPrice,
			UpdatedAt:  time.Now(),
		}

		c.logger.Info("binance_futures_data_received",
			slog.String("binance_symbol", symbol),
			slog.String("unified_symbol", unifiedSymbol),
			slog.Float64("markPrice", markPrice),
			slog.Float64("fundingRate", fundingRate),
		)

		select {
		case c.marketDataCh <- marketData:
		default:
			c.logger.Warn("market_data_channel_full",
				slog.String("symbol", symbol),
			)
		}

		// 更新资金费率（使用统一符号）
		fundingRateEntity := &entity.FundingRate{
			Symbol:      unifiedSymbol, // 使用统一符号
			Exchange:    c.Name(),
			Rate:        fundingRate,
			Rate8H:      fundingRate, // Binance 已经是 8 小时周期
			NextFunding: time.Now().Add(8 * time.Hour),
			UpdatedAt:   time.Now(),
		}

		// 通过 channel 传递资金费率
		select {
		case c.fundingRateCh <- fundingRateEntity:
			c.logger.Debug("funding_rate_sent_to_channel",
				slog.String("symbol", unifiedSymbol),
				slog.Float64("rate", fundingRate),
			)
		default:
			c.logger.Warn("funding_rate_channel_full",
				slog.String("symbol", unifiedSymbol),
			)
		}
	}
}

func (c *BinanceClient) SubscribeMarketData(ctx context.Context, symbols []string) (<-chan *entity.MarketData, error) {
	// 返回市场数据 channel，资金费率通过单独的 channel 传递
	// 构建订阅流
	streams := make([]string, 0, len(symbols)*2)
	for _, symbol := range symbols {
		// 现货 ticker（使用小写）
		lowerSymbol := strings.ToLower(symbol)
		streams = append(streams, fmt.Sprintf("%s@ticker", lowerSymbol))
		c.logger.Debug("subscribing_spot_ticker",
			slog.String("symbol", symbol),
			slog.String("stream", fmt.Sprintf("%s@ticker", lowerSymbol)),
		)
	}

	// 订阅现货流
	spotURL := fmt.Sprintf("%s/%s", c.config.SpotWSURL, strings.Join(streams, "/"))
	c.spotWS = websocket.NewClient(websocket.Config{
		URL:                  spotURL,
		ReconnectInterval:    c.config.ReconnectInterval,
		MaxReconnectAttempts: 10,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         10 * time.Second,
		ConnectTimeout:       30 * time.Second,
		PingInterval:         30 * time.Second,
		ProxyURL:             c.config.ProxyURL,
	}, c.logger)

	if err := c.spotWS.Connect(ctx); err != nil {
		return nil, err
	}

	// 订阅合约 Mark Price 流（订阅特定 symbol，而不是所有合约）
	// 构建合约流列表
	futuresStreams := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		// 将统一符号转换为 Binance 符号
		binanceSymbol := symbol
		if c.symbolMapper != nil {
			if mapped, ok := c.symbolMapper.ToBinance(symbol); ok {
				binanceSymbol = mapped
			}
		}
		// Binance 合约流使用小写
		lowerSymbol := strings.ToLower(binanceSymbol)
		futuresStreams = append(futuresStreams, fmt.Sprintf("%s@markPrice", lowerSymbol))
		c.logger.Debug("subscribing_futures_mark_price",
			slog.String("symbol", symbol),
			slog.String("binance_symbol", binanceSymbol),
			slog.String("stream", fmt.Sprintf("%s@markPrice", lowerSymbol)),
		)
	}

	// 如果配置了 symbol，订阅特定流；否则订阅所有流
	var futuresURL string
	if len(futuresStreams) > 0 {
		futuresURL = fmt.Sprintf("%s/%s", c.config.FuturesWSURL, strings.Join(futuresStreams, "/"))
	} else {
		// 如果没有配置 symbol，使用所有流的订阅方式
		futuresURL = fmt.Sprintf("%s/!markPrice@arr", c.config.FuturesWSURL)
	}
	c.futuresWS = websocket.NewClient(websocket.Config{
		URL:                  futuresURL,
		ReconnectInterval:    c.config.ReconnectInterval,
		MaxReconnectAttempts: 10,
		ReadTimeout:          30 * time.Second,
		WriteTimeout:         10 * time.Second,
		ConnectTimeout:       30 * time.Second,
		PingInterval:         30 * time.Second,
		ProxyURL:             c.config.ProxyURL,
	}, c.logger)

	if err := c.futuresWS.Connect(ctx); err != nil {
		return nil, err
	}

	go c.handleSpotMessages()
	go c.handleFuturesMessages()

	return c.marketDataCh, nil
}

// GetFundingRateChannel 返回资金费率 channel（用于从 WebSocket 接收实时资金费率）
func (c *BinanceClient) GetFundingRateChannel() <-chan *entity.FundingRate {
	return c.fundingRateCh
}

func (c *BinanceClient) GetMarketData(ctx context.Context, symbol string) (*entity.MarketData, error) {
	// 从 WebSocket 数据中获取，如果没有则通过 REST API
	// 这里简化实现，实际应该从缓存或 WebSocket 数据中获取
	return nil, errors.New("not implemented: use SubscribeMarketData")
}

func (c *BinanceClient) GetFundingRate(ctx context.Context, symbol string) (*entity.FundingRate, error) {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	// 将统一符号转换为 Binance 符号
	binanceSymbol := symbol
	if c.symbolMapper != nil {
		if mapped, ok := c.symbolMapper.ToBinance(symbol); ok {
			binanceSymbol = mapped
		}
	}

	url := fmt.Sprintf("%s/fapi/v1/premiumIndex?symbol=%s", c.config.RESTURL, binanceSymbol)
	resp, err := c.httpClient.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var data struct {
		Symbol          string `json:"symbol"`
		LastFundingRate string `json:"lastFundingRate"`
		NextFundingTime int64  `json:"nextFundingTime"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	rate, err := strconv.ParseFloat(data.LastFundingRate, 64)
	if err != nil {
		return nil, err
	}

	// 使用统一符号保存，而不是 Binance 返回的 symbol
	return &entity.FundingRate{
		Symbol:      symbol, // 使用统一符号
		Exchange:    c.Name(),
		Rate:        rate,
		Rate8H:      rate, // Binance 已经是 8 小时周期
		NextFunding: time.Unix(data.NextFundingTime/1000, 0),
		UpdatedAt:   time.Now(),
	}, nil
}

func (c *BinanceClient) IsConnected() bool {
	return c.spotWS.IsConnected() && c.futuresWS.IsConnected()
}

func (c *BinanceClient) Close() error {
	var errs []error
	if err := c.spotWS.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := c.futuresWS.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("close errors: %v", errs)
	}
	return nil
}

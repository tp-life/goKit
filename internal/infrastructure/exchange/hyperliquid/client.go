package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
	"goKit/pkg/kit/http"
	"goKit/pkg/kit/ratelimit"
	"goKit/pkg/kit/websocket"
)

type HyperliquidClient struct {
	config       Config
	logger       *slog.Logger
	ws           *websocket.Client
	httpClient   *http.Client
	rateLimiter  *ratelimit.TokenBucket
	marketDataCh chan *entity.MarketData
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

func NewHyperliquidClient(cfg Config, logger *slog.Logger) exchange.ExchangeClient {
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

	return &HyperliquidClient{
		config:       cfg,
		logger:       logger,
		ws:           websocket.NewClient(wsConfig, logger),
		httpClient:   httpClient,
		rateLimiter:  rateLimiter,
		marketDataCh: make(chan *entity.MarketData, 100),
		symbolMapper: cfg.SymbolMapper,
	}
}

func (c *HyperliquidClient) Name() string {
	return "hyperliquid"
}

func (c *HyperliquidClient) Connect(ctx context.Context) error {
	if err := c.ws.Connect(ctx); err != nil {
		return fmt.Errorf("connect websocket: %w", err)
	}

	// Hyperliquid WebSocket 订阅格式可能不同，需要根据实际 API 调整
	// 这里先实现基础连接
	go c.handleMessages()

	return nil
}

func (c *HyperliquidClient) handleMessages() {
	for {
		select {
		case msg, ok := <-c.ws.Read():
			if !ok {
				return
			}
			c.processMessage(msg)
		case err := <-c.ws.Errors():
			c.logger.Warn("hyperliquid_websocket_error", slog.Any("err", err))
		}
	}
}

func (c *HyperliquidClient) processMessage(msg []byte) {
	// Hyperliquid 的消息格式需要根据实际 API 文档调整
	// 这里实现一个通用的解析逻辑
	var data map[string]interface{}
	if err := json.Unmarshal(msg, &data); err != nil {
		c.logger.Debug("parse_message_failed", slog.Any("err", err))
		return
	}

	// 根据 Hyperliquid 的实际消息格式解析
	// 这里需要根据实际 API 文档调整
	c.logger.Debug("hyperliquid_message_received", slog.Any("data", data))
}

func (c *HyperliquidClient) SubscribeMarketData(ctx context.Context, symbols []string) (<-chan *entity.MarketData, error) {
	// Hyperliquid 使用简化的符号（如 BTC, ETH）
	hyperliquidSymbols := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		if hlSymbol, ok := c.symbolMapper.ToHyperliquid(symbol); ok {
			hyperliquidSymbols = append(hyperliquidSymbols, hlSymbol)
		}
	}

	if len(hyperliquidSymbols) == 0 {
		return nil, errors.New("no valid symbols for hyperliquid")
	}

	// Hyperliquid WebSocket 订阅格式需要根据实际 API 调整
	// 这里先返回 channel，实际订阅逻辑需要根据 API 文档实现
	if c.ws.IsConnected() {
		// TODO: 根据 Hyperliquid API 文档实现订阅消息
		c.logger.Info("hyperliquid_subscribe", slog.Any("symbols", hyperliquidSymbols))
	}

	go c.handleMessages()

	return c.marketDataCh, nil
}

func (c *HyperliquidClient) GetMarketData(ctx context.Context, symbol string) (*entity.MarketData, error) {
	// 从 WebSocket 数据中获取，如果没有则通过 REST API
	_, ok := c.symbolMapper.ToHyperliquid(symbol)
	if !ok {
		return nil, fmt.Errorf("symbol %s not found in hyperliquid mapping", symbol)
	}

	// 使用 REST API 获取市场数据
	url := fmt.Sprintf("%s/info", c.config.RESTURL)
	
	// Hyperliquid API 可能需要 POST 请求，这里需要根据实际 API 调整
	reqBody := map[string]interface{}{
		"type": "meta",
	}
	
	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := c.httpClient.Post(ctx, url, bytes.NewReader(bodyBytes), map[string]string{
		"Content-Type": "application/json",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 解析响应（需要根据实际 API 格式调整）
	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	// TODO: 根据 Hyperliquid API 实际响应格式解析市场数据
	return nil, errors.New("not fully implemented: need Hyperliquid API documentation")
}

func (c *HyperliquidClient) GetFundingRate(ctx context.Context, symbol string) (*entity.FundingRate, error) {
	if err := c.rateLimiter.Wait(ctx); err != nil {
		return nil, err
	}

	hyperliquidSymbol, ok := c.symbolMapper.ToHyperliquid(symbol)
	if !ok {
		return nil, fmt.Errorf("symbol %s not found in hyperliquid mapping", symbol)
	}

	// Hyperliquid API 可能需要 POST 请求
	url := fmt.Sprintf("%s/info", c.config.RESTURL)
	
	reqBody := map[string]interface{}{
		"type": "fundingHistory",
		"coin": hyperliquidSymbol,
	}
	
	bodyBytes, _ := json.Marshal(reqBody)
	resp, err := c.httpClient.Post(ctx, url, bytes.NewReader(bodyBytes), map[string]string{
		"Content-Type": "application/json",
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 解析响应（需要根据实际 API 格式调整）
	var response struct {
		FundingRate string `json:"fundingRate"` // 需要根据实际格式调整
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	rate, err := strconv.ParseFloat(response.FundingRate, 64)
	if err != nil {
		return nil, err
	}

	// Hyperliquid 通常是 8 小时周期（需要确认）
	return &entity.FundingRate{
		Symbol:      symbol,
		Exchange:    c.Name(),
		Rate:        rate,
		Rate8H:      rate, // 假设是 8 小时周期，需要根据实际确认
		NextFunding: time.Now().Add(8 * time.Hour),
		UpdatedAt:   time.Now(),
	}, nil
}

func (c *HyperliquidClient) IsConnected() bool {
	return c.ws.IsConnected()
}

func (c *HyperliquidClient) Close() error {
	return c.ws.Close()
}

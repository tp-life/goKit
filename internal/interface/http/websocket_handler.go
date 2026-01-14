package http

import (
	"encoding/json"
	"log/slog"
	"time"

	"goKit/internal/application/service"
	"goKit/internal/domain/entity"

	"github.com/gofiber/websocket/v2"
)

type WebSocketHandler struct {
	comparisonService *service.ComparisonService
	logger            *slog.Logger
}

func NewWebSocketHandler(
	comparisonService *service.ComparisonService,
	logger *slog.Logger,
) *WebSocketHandler {
	return &WebSocketHandler{
		comparisonService: comparisonService,
		logger:            logger,
	}
}

func (h *WebSocketHandler) HandleComparison(c *websocket.Conn) {
	// 发送初始数据
	h.sendComparisonData(c)

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// 读取客户端消息的 channel
	done := make(chan struct{})
	go func() {
		for {
			_, _, err := c.ReadMessage()
			if err != nil {
				close(done)
				return
			}
		}
	}()

	// 定时推送更新（每3秒）
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := h.sendComparisonData(c); err != nil {
				h.logger.Warn("websocket_send_error", slog.Any("err", err))
				return
			}
		}
	}
}

func (h *WebSocketHandler) sendComparisonData(c *websocket.Conn) error {
	comparisons := h.comparisonService.GetAllComparisons()

	// 转换为前端需要的格式
	data := make(map[string]interface{})
	for symbol, comparison := range comparisons {
		priceDiff := getPriceDiff(comparison)
		data[symbol] = map[string]interface{}{
			"prices": map[string]interface{}{
				"binance": map[string]interface{}{
					"markPrice": getPrice(comparison.Prices["binance"]),
					"spotPrice": getSpotPrice(comparison.Prices["binance"]),
				},
				"lighter": map[string]interface{}{
					"markPrice": getPrice(comparison.Prices["lighter"]),
					"spotPrice": getSpotPrice(comparison.Prices["lighter"]),
				},
				"hyperliquid": map[string]interface{}{
					"markPrice": getPrice(comparison.Prices["hyperliquid"]),
					"spotPrice": getSpotPrice(comparison.Prices["hyperliquid"]),
				},
			},
			"rates": map[string]interface{}{
				"binance": map[string]interface{}{
					"rate":   getRate(comparison.Rates["binance"]),
					"rate8H": getRate8H(comparison.Rates["binance"]),
				},
				"lighter": map[string]interface{}{
					"rate":   getRate(comparison.Rates["lighter"]),
					"rate8H": getRate8H(comparison.Rates["lighter"]),
				},
				"hyperliquid": map[string]interface{}{
					"rate":   getRate(comparison.Rates["hyperliquid"]),
					"rate8H": getRate8H(comparison.Rates["hyperliquid"]),
				},
			},
			"priceDiff":      priceDiff,
			"rateDiff":       getRateDiff(comparison),
			"arbitrageLevel": getArbitrageLevel(comparison),
		}
	}

	message := map[string]interface{}{
		"type": "comparison",
		"data": data,
	}

	msgBytes, err := json.Marshal(message)
	if err != nil {
		return err
	}

	return c.WriteMessage(websocket.TextMessage, msgBytes)
}

func getPrice(marketData interface{}) interface{} {
	if md, ok := marketData.(*entity.MarketData); ok && md != nil {
		// 优先使用 MarkPrice
		if md.MarkPrice != nil {
			return *md.MarkPrice
		}
	}
	return nil
}

func getSpotPrice(marketData interface{}) interface{} {
	if md, ok := marketData.(*entity.MarketData); ok && md != nil {
		if md.SpotPrice != nil {
			return *md.SpotPrice
		}
	}
	return nil
}

func getRate(fundingRate interface{}) interface{} {
	if fr, ok := fundingRate.(*entity.FundingRate); ok && fr != nil {
		return fr.Rate
	}
	return nil
}

func getRate8H(fundingRate interface{}) interface{} {
	if fr, ok := fundingRate.(*entity.FundingRate); ok && fr != nil {
		return fr.Rate8H
	}
	return nil
}

func getPriceDiff(comparison *entity.ComparisonResult) float64 {
	if len(comparison.Comparisons) == 0 {
		return 0
	}
	// 优先返回 binance-lighter 的价差（最重要的对比）
	for _, comp := range comparison.Comparisons {
		if (comp.ExchangeA == "binance" && comp.ExchangeB == "lighter") ||
			(comp.ExchangeA == "lighter" && comp.ExchangeB == "binance") {
			return comp.PriceDiff
		}
	}
	// 如果没有 binance-lighter，返回第一个对比的价差
	return comparison.Comparisons[0].PriceDiff
}

func getRateDiff(comparison *entity.ComparisonResult) float64 {
	if len(comparison.Comparisons) == 0 {
		return 0
	}
	// 优先返回 binance-lighter 的费率差（最重要的对比）
	for _, comp := range comparison.Comparisons {
		if (comp.ExchangeA == "binance" && comp.ExchangeB == "lighter") ||
			(comp.ExchangeA == "lighter" && comp.ExchangeB == "binance") {
			return comp.RateDiff
		}
	}
	// 如果没有 binance-lighter，返回第一个对比的费率差
	return comparison.Comparisons[0].RateDiff
}

func getArbitrageLevel(comparison *entity.ComparisonResult) string {
	if len(comparison.Comparisons) == 0 {
		return "none"
	}
	// 优先返回 binance-lighter 的套利级别（最重要的对比）
	for _, comp := range comparison.Comparisons {
		if (comp.ExchangeA == "binance" && comp.ExchangeB == "lighter") ||
			(comp.ExchangeA == "lighter" && comp.ExchangeB == "binance") {
			return comp.ArbitrageLevel
		}
	}
	// 如果没有 binance-lighter，返回第一个对比的套利级别
	return comparison.Comparisons[0].ArbitrageLevel
}

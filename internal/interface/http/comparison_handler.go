package http

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"goKit/internal/application/service"
)

type ComparisonHandler struct {
	comparisonService *service.ComparisonService
	logger            *slog.Logger
}

func NewComparisonHandler(
	comparisonService *service.ComparisonService,
	logger *slog.Logger,
) *ComparisonHandler {
	return &ComparisonHandler{
		comparisonService: comparisonService,
		logger:            logger,
	}
}

func (h *ComparisonHandler) RegisterRoutes(app *fiber.App) {
	api := app.Group("/api/v1")
	
	// 对比数据
	api.Get("/comparison", h.GetAllComparisons)
	api.Get("/comparison/:symbol", h.GetComparison)
	
	// 套利机会
	api.Get("/arbitrage", h.GetArbitrageOpportunities)
	api.Get("/arbitrage/high", h.GetHighArbitrageOpportunities)
	
	// 配置
	api.Put("/threshold", h.UpdateThreshold)
	
	// WebSocket 路由在 main.go 中注册
}

// GetAllComparisons 获取所有交易对的对比数据
func (h *ComparisonHandler) GetAllComparisons(c *fiber.Ctx) error {
	comparisons := h.comparisonService.GetAllComparisons()
	return c.JSON(fiber.Map{
		"code": 0,
		"data": comparisons,
	})
}

// GetComparison 获取单个交易对的对比数据
func (h *ComparisonHandler) GetComparison(c *fiber.Ctx) error {
	symbol := c.Params("symbol")
	
	comparison, ok := h.comparisonService.GetComparison(symbol)
	if !ok {
		// 如果缓存中没有，实时计算
		ctx := c.UserContext()
		result, err := h.comparisonService.CompareExchanges(ctx, symbol)
		if err != nil {
			return c.Status(500).JSON(fiber.Map{
				"code": 1,
				"msg":  err.Error(),
			})
		}
		return c.JSON(fiber.Map{
			"code": 0,
			"data": result,
		})
	}

	return c.JSON(fiber.Map{
		"code": 0,
		"data": comparison,
	})
}

// GetArbitrageOpportunities 获取套利机会列表
func (h *ComparisonHandler) GetArbitrageOpportunities(c *fiber.Ctx) error {
	// TODO: 从数据库查询套利机会
	return c.JSON(fiber.Map{
		"code": 0,
		"data": []interface{}{},
	})
}

// GetHighArbitrageOpportunities 获取高套利机会
func (h *ComparisonHandler) GetHighArbitrageOpportunities(c *fiber.Ctx) error {
	// TODO: 从数据库查询高套利机会
	return c.JSON(fiber.Map{
		"code": 0,
		"data": []interface{}{},
	})
}

// UpdateThreshold 更新套利阈值
func (h *ComparisonHandler) UpdateThreshold(c *fiber.Ctx) error {
	// TODO: 实现阈值更新
	return c.JSON(fiber.Map{
		"code": 0,
		"msg":  "not implemented",
	})
}

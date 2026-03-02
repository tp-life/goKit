package http

import (
	"goKit/internal/application/service"

	"github.com/gofiber/fiber/v2"
)

type StrategyHandler struct {
	svc *service.StrategyService
}

func NewStrategyHandler(svc *service.StrategyService) *StrategyHandler {
	return &StrategyHandler{svc: svc}
}

func (h *StrategyHandler) RegisterRoutes(app *fiber.App) {
	g := app.Group("/api/v1/strategy")
	g.Get("/golden-pit", h.GetGoldenPit)
}

func (h *StrategyHandler) GetGoldenPit(c *fiber.Ctx) error {
	// 获取查询参数，这里可以使用默认值作为测试
	reportDate := c.Query("report_date", "2023-09-30")
	quarterStart := c.Query("quarter_start", "2023-07-01")
	quarterEnd := c.Query("quarter_end", "2023-09-30")

	data, err := h.svc.FindGoldenPitStocks(c.UserContext(), reportDate, quarterStart, quarterEnd)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	return c.JSON(fiber.Map{
		"code":    0,
		"message": "success",
		"data":    data,
		"total":   len(data),
	})
}

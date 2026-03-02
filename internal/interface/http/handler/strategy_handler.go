package handler

import (
	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

type StrategyHandler struct {
	svc *service.StrategyService
}

func NewStrategyHandler(svc *service.StrategyService) *StrategyHandler {
	return &StrategyHandler{svc: svc}
}

func (h *StrategyHandler) GetGoldenPit(c *fiber.Ctx) error {
	reportDate := c.Query("report_date")
	if reportDate == "" {
		// 缺少核心参数，直接阻断
		return response.ErrBadRequest("report_date 参数不能为空")
	}

	quarterStart := c.Query("quarter_start", "2023-07-01")
	quarterEnd := c.Query("quarter_end", "2023-09-30")

	data, err := h.svc.FindGoldenPitStocks(c.UserContext(), reportDate, quarterStart, quarterEnd)
	if err != nil {
		// 数据库异常或策略引擎报错
		return response.ErrInternal(err, "策略引擎计算异常: "+err.Error())
	}

	return response.Success(c, fiber.Map{
		"list":  data,
		"total": len(data),
	})
}

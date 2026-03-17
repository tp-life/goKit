package handler

import (
	"strings"

	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

type MarketHandler struct {
	svc *service.MarketQueryService
}

func NewMarketHandler(svc *service.MarketQueryService) *MarketHandler {
	return &MarketHandler{svc: svc}
}

func (h *MarketHandler) Snapshot(c *fiber.Ctx) error {
	symbol := strings.TrimSpace(c.Params("symbol"))
	if symbol == "" {
		return response.ErrBadRequest("缺少 symbol")
	}
	return response.Success(c, h.svc.Snapshot(symbol))
}

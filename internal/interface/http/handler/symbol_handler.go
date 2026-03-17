package handler

import (
	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

type SymbolHandler struct {
	svc *service.SymbolService
}

func NewSymbolHandler(svc *service.SymbolService) *SymbolHandler {
	return &SymbolHandler{svc: svc}
}

func (h *SymbolHandler) List(c *fiber.Ctx) error {
	items, err := h.svc.List(c.UserContext())
	if err != nil {
		return response.ErrInternal(err, "查询交易对失败")
	}
	return response.Success(c, items)
}

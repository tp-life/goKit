package handler

import (
	"strconv"

	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

type OpportunityHandler struct {
	svc *service.OpportunityQueryService
}

func NewOpportunityHandler(svc *service.OpportunityQueryService) *OpportunityHandler {
	return &OpportunityHandler{svc: svc}
}

func (h *OpportunityHandler) List(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	items, err := h.svc.ListLatest(c.UserContext(), limit)
	if err != nil {
		return response.ErrInternal(err, "查询套利机会失败")
	}
	return response.Success(c, items)
}

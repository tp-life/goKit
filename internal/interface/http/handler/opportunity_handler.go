package handler

import (
	"errors"
	"strconv"

	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
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

func (h *OpportunityHandler) ListSummary(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	items, err := h.svc.ListLatestSummary(c.UserContext(), limit)
	if err != nil {
		return response.ErrInternal(err, "查询套利机会摘要失败")
	}
	return response.Success(c, items)
}

func (h *OpportunityHandler) Detail(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return response.ErrBadRequest("无效的机会ID")
	}
	item, err := h.svc.GetByID(c.UserContext(), uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return response.ErrNotFound("未找到机会详情")
		}
		return response.ErrInternal(err, "查询套利机会详情失败")
	}
	if item == nil {
		return response.ErrNotFound("未找到机会详情")
	}
	return response.Success(c, item)
}

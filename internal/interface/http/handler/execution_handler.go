package handler

import (
	"strconv"
	"strings"

	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

type ExecutionHandler struct {
	svc *service.ExecutionService
}

func NewExecutionHandler(svc *service.ExecutionService) *ExecutionHandler {
	return &ExecutionHandler{svc: svc}
}

func (h *ExecutionHandler) List(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	items, err := h.svc.ListLatest(c.UserContext(), limit)
	if err != nil {
		return response.ErrInternal(err, "查询执行记录失败")
	}
	return response.Success(c, items)
}

func (h *ExecutionHandler) Orders(c *fiber.Ctx) error {
	planKey := strings.TrimSpace(c.Params("planKey"))
	if planKey == "" {
		return response.ErrBadRequest("缺少 planKey")
	}
	items, err := h.svc.ListOrdersByPlanKey(c.UserContext(), planKey)
	if err != nil {
		return response.ErrInternal(err, "查询订单记录失败")
	}
	return response.Success(c, items)
}

func (h *ExecutionHandler) Open(c *fiber.Ctx) error {
	planKey := strings.TrimSpace(c.Params("planKey"))
	if planKey == "" {
		return response.ErrBadRequest("缺少 planKey")
	}
	item, err := h.svc.OpenByPlanKey(c.UserContext(), planKey)
	if err != nil {
		return response.ErrInternal(err, "执行开仓失败")
	}
	return response.Success(c, item)
}

func (h *ExecutionHandler) Close(c *fiber.Ctx) error {
	planKey := strings.TrimSpace(c.Params("planKey"))
	if planKey == "" {
		return response.ErrBadRequest("缺少 planKey")
	}
	item, err := h.svc.CloseByPlanKey(c.UserContext(), planKey)
	if err != nil {
		return response.ErrInternal(err, "执行平仓失败")
	}
	return response.Success(c, item)
}

package handler

import (
	"strconv"
	"strings"

	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

type ExecutionPlanHandler struct {
	svc *service.ExecutionPlanService
}

func NewExecutionPlanHandler(svc *service.ExecutionPlanService) *ExecutionPlanHandler {
	return &ExecutionPlanHandler{svc: svc}
}

func (h *ExecutionPlanHandler) List(c *fiber.Ctx) error {
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	opportunityBatchID := strings.TrimSpace(c.Query("opportunity_batch_id"))

	if opportunityBatchID != "" {
		items, err := h.svc.ListByOpportunityBatch(c.UserContext(), opportunityBatchID, limit)
		if err != nil {
			return response.ErrInternal(err, "按机会批次查询执行计划失败")
		}
		return response.Success(c, items)
	}

	items, err := h.svc.ListLatest(c.UserContext(), limit)
	if err != nil {
		return response.ErrInternal(err, "查询执行计划失败")
	}
	return response.Success(c, items)
}

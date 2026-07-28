package handler

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/oplog"
	"goKit/internal/interface/http/response"
)

type OplogHandler struct {
	svc *oplog.OplogService
}

func NewOplogHandler(svc *oplog.OplogService) *OplogHandler {
	return &OplogHandler{svc: svc}
}

// List GET /logs
func (h *OplogHandler) List(c *fiber.Ctx) error {
	resp, err := h.svc.List(c.UserContext(), parsePage(c))
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, resp)
}

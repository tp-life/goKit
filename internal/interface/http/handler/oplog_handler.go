package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/oplog"
	domainoplog "goKit/internal/domain/oplog"
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
	q := domainoplog.Query{
		Username: c.Query("username"),
		Method:   c.Query("method"),
		Path:     c.Query("path"),
	}
	if s := c.Query("status"); s != "" {
		if v, err := strconv.Atoi(s); err == nil {
			q.Status = &v
		}
	}
	resp, err := h.svc.List(c.UserContext(), parsePage(c), q)
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, resp)
}

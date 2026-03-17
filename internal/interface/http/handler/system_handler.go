package handler

import (
	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

type SystemHandler struct {
	svc *service.SystemService
}

func NewSystemHandler(svc *service.SystemService) *SystemHandler {
	return &SystemHandler{svc: svc}
}

func (h *SystemHandler) Status(c *fiber.Ctx) error {
	return response.Success(c, h.svc.Status())
}

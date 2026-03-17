package handler

import (
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

type HealthHandler struct{}

func NewHealthHandler() *HealthHandler { return &HealthHandler{} }

func (h *HealthHandler) Get(c *fiber.Ctx) error {
	return response.Success(c, fiber.Map{"status": "ok", "service": "funding-arbitrage-monitor"})
}

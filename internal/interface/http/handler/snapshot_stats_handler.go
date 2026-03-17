package handler

import (
	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"

	"github.com/gofiber/fiber/v2"
)

// SnapshotStatsHandler 用于查询最近 24h 的快照统计。
type SnapshotStatsHandler struct {
	service *service.SnapshotStatsService
}

func NewSnapshotStatsHandler(service *service.SnapshotStatsService) *SnapshotStatsHandler {
	return &SnapshotStatsHandler{
		service: service,
	}
}

func (h *SnapshotStatsHandler) Get(c *fiber.Ctx) error {
	stats, err := h.service.Get24hStats(c.Context())
	if err != nil {
		return err
	}

	return response.Success(c, stats)
}

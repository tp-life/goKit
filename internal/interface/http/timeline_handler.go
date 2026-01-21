package http

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"goKit/internal/application/service"
)

type TimelineHandler struct {
	timelineService *service.TimelineService
	authService     *service.AuthService
}

func NewTimelineHandler(timelineService *service.TimelineService, authService *service.AuthService) *TimelineHandler {
	return &TimelineHandler{
		timelineService: timelineService,
		authService:     authService,
	}
}

// RegisterRoutes 已迁移到 routes.go，此方法保留用于向后兼容（但不会被调用）
func (h *TimelineHandler) RegisterRoutes(app *fiber.App) {
	// 路由已集中管理在 routes.go 中
}

// Get 获取时间轴
// GET /api/v1/timeline?limit=20&offset=0
// 支持访客模式：未登录时返回公开内容
func (h *TimelineHandler) Get(c *fiber.Ctx) error {
	userID := GetUserID(c) // 可能为 0（访客模式）

	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	offset, _ := strconv.Atoi(c.Query("offset", "0"))

	req := service.GetTimelineRequest{
		Limit:  limit,
		Offset: offset,
	}

	resp, err := h.timelineService.GetTimeline(c.UserContext(), userID, req)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

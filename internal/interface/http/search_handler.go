package http

import (
	"strconv"

	"goKit/internal/application/service"

	"github.com/gofiber/fiber/v2"
)

type SearchHandler struct {
	searchService *service.SearchService
}

func NewSearchHandler(searchService *service.SearchService) *SearchHandler {
	return &SearchHandler{
		searchService: searchService,
	}
}

// Search 执行搜索
// GET /api/v1/search?q=关键词
func (h *SearchHandler) Search(c *fiber.Ctx) error {
	userID := GetUserID(c)
	// 搜索默认允许所有用户访问（包括未登录用户）
	// 如果需要限制，可以在这里添加认证检查

	query := c.Query("q")
	if query == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": "query parameter 'q' is required",
		})
	}

	// 获取分页参数
	limit := 50 // 默认每页50条
	offset := 0

	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}
	if offsetStr := c.Query("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	resp, err := h.searchService.Search(c.UserContext(), userID, query, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

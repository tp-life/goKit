package http

import (
	"strconv"

	"goKit/internal/application/service"

	"github.com/gofiber/fiber/v2"
)

type TagHandler struct {
	tagService *service.TagService
}

func NewTagHandler(tagService *service.TagService) *TagHandler {
	return &TagHandler{
		tagService: tagService,
	}
}

// GetTags 获取标签列表
// GET /api/v1/tags
func (h *TagHandler) GetTags(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == 0 {
		return c.Status(401).JSON(fiber.Map{
			"error": "unauthorized",
		})
	}

	resp, err := h.tagService.GetTags(c.UserContext(), userID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

// GetTagTimeline 获取标签聚合时间轴
// GET /api/v1/tags/:name/timeline
func (h *TagHandler) GetTagTimeline(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == 0 {
		return c.Status(401).JSON(fiber.Map{
			"error": "unauthorized",
		})
	}

	tagName := c.Params("name")
	if tagName == "" {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid tag name",
		})
	}

	// 获取分页参数
	limit := 50
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

	resp, err := h.tagService.GetTagTimeline(c.UserContext(), userID, tagName, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

// DeleteTag 删除标签
// DELETE /api/v1/tags/:id
func (h *TagHandler) DeleteTag(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == 0 {
		return c.Status(401).JSON(fiber.Map{
			"error": "unauthorized",
		})
	}

	tagID, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid tag id",
		})
	}

	resp, err := h.tagService.DeleteTag(c.UserContext(), userID, tagID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

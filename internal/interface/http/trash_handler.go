package http

import (
	"strconv"

	"goKit/internal/application/service"

	"github.com/gofiber/fiber/v2"
)

type TrashHandler struct {
	trashService *service.TrashService
}

func NewTrashHandler(trashService *service.TrashService) *TrashHandler {
	return &TrashHandler{
		trashService: trashService,
	}
}

// GetTrash 获取回收站列表
// GET /api/v1/trash
func (h *TrashHandler) GetTrash(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == 0 {
		return c.Status(401).JSON(fiber.Map{
			"error": "unauthorized",
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

	resp, err := h.trashService.GetTrash(c.UserContext(), userID, limit, offset)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

// RestoreItem 恢复回收站项目
// POST /api/v1/trash/:type/:id/restore
func (h *TrashHandler) RestoreItem(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == 0 {
		return c.Status(401).JSON(fiber.Map{
			"error": "unauthorized",
		})
	}

	itemType := c.Params("type")
	if itemType != "memo" && itemType != "page" {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid item type, must be 'memo' or 'page'",
		})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid item id",
		})
	}

	resp, err := h.trashService.RestoreItem(c.UserContext(), itemType, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

// PermanentlyDeleteItem 永久删除回收站项目
// DELETE /api/v1/trash/:type/:id
func (h *TrashHandler) PermanentlyDeleteItem(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == 0 {
		return c.Status(401).JSON(fiber.Map{
			"error": "unauthorized",
		})
	}

	itemType := c.Params("type")
	if itemType != "memo" && itemType != "page" {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid item type, must be 'memo' or 'page'",
		})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid item id",
		})
	}

	resp, err := h.trashService.PermanentlyDeleteItem(c.UserContext(), itemType, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

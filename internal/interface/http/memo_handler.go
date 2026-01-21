package http

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"goKit/internal/application/service"
)

type MemoHandler struct {
	memoService *service.MemoService
	authService *service.AuthService
}

func NewMemoHandler(memoService *service.MemoService, authService *service.AuthService) *MemoHandler {
	return &MemoHandler{
		memoService: memoService,
		authService: authService,
	}
}

// RegisterRoutes 已迁移到 routes.go，此方法保留用于向后兼容（但不会被调用）
func (h *MemoHandler) RegisterRoutes(app *fiber.App) {
	// 路由已集中管理在 routes.go 中
}

// Create 创建闪念
// POST /api/v1/memos
// 默认允许公开访问（未登录用户也可以创建 Memo）
func (h *MemoHandler) Create(c *fiber.Ctx) error {
	userID := GetUserID(c)
	// 允许未登录用户创建 Memo，userID 为 0 时在 Service 层处理

	var req service.CreateMemoRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	if req.Source == "" {
		req.Source = "mobile"
	}

	resp, err := h.memoService.CreateMemo(c.UserContext(), userID, req)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.Status(201).JSON(resp)
}

// Get 获取 Memo 详情
// GET /api/v1/memos/:id
// 默认允许公开访问（未登录用户也可以查看 Memo）
func (h *MemoHandler) Get(c *fiber.Ctx) error {
	userID := GetUserID(c) // 可能为 0（未登录用户）

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid memo id",
		})
	}

	// 允许未登录用户访问，userID 为 0 时在 Service 层处理
	memo, err := h.memoService.GetMemo(c.UserContext(), userID, id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": "memo not found",
		})
	}

	return c.JSON(memo)
}

// Update 更新 Memo
// PUT /api/v1/memos/:id
// 默认允许公开访问（未登录用户也可以更新 Memo）
func (h *MemoHandler) Update(c *fiber.Ctx) error {
	userID := GetUserID(c) // 可能为 0（未登录用户）

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid memo id",
		})
	}

	var req service.UpdateMemoRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	resp, err := h.memoService.UpdateMemo(c.UserContext(), userID, id, req)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

// Delete 删除 Memo（软删除）
// DELETE /api/v1/memos/:id
func (h *MemoHandler) Delete(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == 0 {
		return c.Status(401).JSON(fiber.Map{
			"error": "unauthorized",
		})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid memo id",
		})
	}

	err = h.memoService.DeleteMemo(c.UserContext(), userID, id)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Memo deleted successfully",
	})
}

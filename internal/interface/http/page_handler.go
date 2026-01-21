package http

import (
	"strconv"
	"time"

	"goKit/internal/application/service"

	"github.com/gofiber/fiber/v2"
)

const (
	defaultEditorJSVersion = "2.0"
)

type PageHandler struct {
	pageService *service.PageService
	authService *service.AuthService
}

func NewPageHandler(pageService *service.PageService, authService *service.AuthService) *PageHandler {
	return &PageHandler{
		pageService: pageService,
		authService: authService,
	}
}

// EditorJSBlockDTO Editor.js 块结构（DTO 层）
type EditorJSBlockDTO struct {
	ID   string         `json:"id"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// EditorJSDataDTO Editor.js 完整数据结构（DTO 层）
type EditorJSDataDTO struct {
	Time    any                `json:"time"` // 可能是 int64 或 float64
	Version string             `json:"version"`
	Blocks  []EditorJSBlockDTO `json:"blocks"`
}

// CreatePageRequestDTO HTTP 层请求结构体
type CreatePageRequestDTO struct {
	ID     any             `json:"id"` // 可能是字符串或数字
	Title  string          `json:"title"`
	Cover  string          `json:"cover"`
	Tags   []string        `json:"tags"`
	Blocks EditorJSDataDTO `json:"blocks"` // Editor.js 格式数据
}

// parseID 解析 ID 字段，支持多种类型
func parseID(id any) (uint64, error) {
	if id == nil {
		return 0, nil
	}

	switch v := id.(type) {
	case string:
		if v == "" {
			return 0, nil
		}
		parsed, err := strconv.ParseUint(v, 10, 64)
		if err != nil {
			return 0, err
		}
		return parsed, nil
	case float64:
		return uint64(v), nil
	case int64:
		return uint64(v), nil
	case int:
		return uint64(v), nil
	case uint64:
		return v, nil
	default:
		return 0, nil
	}
}

// parseTime 解析时间字段，支持多种类型，默认返回当前时间
func parseTime(t any) int64 {
	if t == nil {
		return time.Now().Unix()
	}

	switch v := t.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return time.Now().Unix()
	}
}

// convertDTOToServiceRequest 将 HTTP DTO 转换为 Service 层请求
func (h *PageHandler) convertDTOToServiceRequest(dto CreatePageRequestDTO) (service.CreatePageRequest, error) {
	var req service.CreatePageRequest

	// 解析 ID
	id, err := parseID(dto.ID)
	if err != nil {
		return req, fiber.NewError(fiber.StatusBadRequest, "invalid page id format: "+err.Error())
	}
	req.ID = id

	req.Title = dto.Title
	req.Cover = dto.Cover
	req.Tags = dto.Tags

	// 处理 blocks 字段
	req.Blocks.Version = dto.Blocks.Version
	if req.Blocks.Version == "" {
		req.Blocks.Version = defaultEditorJSVersion
	}

	req.Blocks.Time = parseTime(dto.Blocks.Time)

	// 转换 blocks 数组
	if len(dto.Blocks.Blocks) > 0 {
		req.Blocks.Blocks = make([]service.EditorJSBlock, 0, len(dto.Blocks.Blocks))
		for _, blockDTO := range dto.Blocks.Blocks {
			block := service.EditorJSBlock{
				ID:   blockDTO.ID,
				Type: blockDTO.Type,
				Data: blockDTO.Data,
			}
			// 确保 data 不为 nil
			if block.Data == nil {
				block.Data = make(map[string]any)
			}
			req.Blocks.Blocks = append(req.Blocks.Blocks, block)
		}
	} else {
		req.Blocks.Blocks = []service.EditorJSBlock{}
	}

	return req, nil
}

// CreateOrUpdate 创建或更新页面
// POST /api/v1/pages
// 默认允许公开访问（未登录用户也可以创建页面）
func (h *PageHandler) CreateOrUpdate(c *fiber.Ctx) error {
	userID := GetUserID(c)
	// 如果未登录，使用默认用户 ID（0 或 1，根据业务需求调整）
	// 这里允许未登录用户创建页面，userID 为 0 时在 Service 层处理

	// 解析请求体
	var dto CreatePageRequestDTO
	if err := c.BodyParser(&dto); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error":   "invalid request body",
			"details": err.Error(),
		})
	}

	// 转换为 Service 层请求
	req, err := h.convertDTOToServiceRequest(dto)
	if err != nil {
		return err
	}

	resp, err := h.pageService.CreateOrUpdatePage(c.UserContext(), userID, req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

// Get 获取页面详情（混合模式：Owner 或 Guest）
// GET /api/v1/pages/:id
// 权限规则：
// - 若 JWT 有效且为 Owner -> 允许访问（包括私有页面）
// - 若无 JWT 但 is_shared=1 -> 允许只读 (Guest Mode)
// - 其他 -> 403 Forbidden
func (h *PageHandler) Get(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid page id",
		})
	}

	page, err := h.pageService.GetPage(c.UserContext(), id)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
			"error": "page not found",
		})
	}

	// 默认允许所有用户访问（公开访问）
	// 如果有 JWT 且为 Owner，可以访问所有页面（包括私有页面）
	// 如果没有 JWT，也可以访问所有页面（默认公开）
	return c.JSON(page)
}

// GetByShareID 通过 ShareID 获取公开页面
// GET /api/v1/public/pages/:share_id
func (h *PageHandler) GetByShareID(c *fiber.Ctx) error {
	shareID := c.Params("share_id")
	if shareID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid share_id",
		})
	}

	page, err := h.pageService.GetPageByShareID(c.UserContext(), shareID)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{
			"error": "page not found",
		})
	}

	return c.JSON(page)
}

// Share 开启/关闭页面分享
// POST /api/v1/pages/:id/share
// 默认允许公开访问（未登录用户也可以分享页面）
func (h *PageHandler) Share(c *fiber.Ctx) error {
	userID := GetUserID(c)
	// 允许未登录用户分享页面，userID 为 0 时在 Service 层处理

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid page id",
		})
	}

	var req service.SharePageRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}

	resp, err := h.pageService.SharePage(c.UserContext(), userID, id, req.Enable)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(resp)
}

// Delete 删除 Page（软删除）
// DELETE /api/v1/pages/:id
func (h *PageHandler) Delete(c *fiber.Ctx) error {
	userID := GetUserID(c)
	if userID == 0 {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"error": "unauthorized",
		})
	}

	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid page id",
		})
	}

	err = h.pageService.DeletePage(c.UserContext(), userID, id)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Page deleted successfully",
	})
}

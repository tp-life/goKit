package http

import (
	"log/slog"

	"goKit/internal/domain/repository"

	"github.com/gofiber/fiber/v2"
)

type UploadHandler struct {
	storageService repository.StorageService
	logger         *slog.Logger
}

func NewUploadHandler(storageService repository.StorageService, logger *slog.Logger) *UploadHandler {
	return &UploadHandler{
		storageService: storageService,
		logger:         logger,
	}
}

// Upload 图片上传接口（兼容 Editor.js 格式）
// POST /api/v1/upload
// 返回格式: {"success": 1, "file": {"url": "..."}}
func (h *UploadHandler) Upload(c *fiber.Ctx) error {
	// 获取上传的文件
	file, err := c.FormFile("image")
	if err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": 0,
			"error":   "no file uploaded",
		})
	}

	// 打开文件
	src, err := file.Open()
	if err != nil {
		h.logger.ErrorContext(c.UserContext(), "open_upload_file_failed",
			slog.Any("error", err),
		)
		return c.Status(500).JSON(fiber.Map{
			"success": 0,
			"error":   "failed to open file",
		})
	}
	defer src.Close()

	// 生成存储路径
	key := h.storageService.GenerateKey(file.Filename)

	// 上传到七牛云
	url, err := h.storageService.UploadFile(c.UserContext(), key, src, file.Size)
	if err != nil {
		h.logger.ErrorContext(c.UserContext(), "upload_to_storage_failed",
			slog.Any("error", err),
		)
		return c.Status(500).JSON(fiber.Map{
			"success": 0,
			"error":   "failed to upload file",
		})
	}

	// 返回 Editor.js 要求的格式
	return c.JSON(fiber.Map{
		"success": 1,
		"file": fiber.Map{
			"url": url,
		},
	})
}

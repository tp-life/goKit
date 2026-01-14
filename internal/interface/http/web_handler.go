package http

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/gofiber/fiber/v2"
)

type WebHandler struct {
	logger    *slog.Logger
	webDistFS embed.FS
}

func NewWebHandler(logger *slog.Logger, webDistFS embed.FS) *WebHandler {
	return &WebHandler{
		logger:    logger,
		webDistFS: webDistFS,
	}
}

func (h *WebHandler) RegisterRoutes(app *fiber.App) {
	// 优先尝试使用 embed 的文件（生产环境）
	// 如果 embed 失败，使用文件系统（开发环境）

	// 尝试从 embed 文件系统读取
	// webDistFS 从 main 包通过依赖注入传入，路径是 web/dist（在 cmd/server/web/dist）
	distFS, err := fs.Sub(h.webDistFS, "web/dist")
	if err != nil {
		// 如果 Sub 失败，可能是开发模式或 embed 为空，尝试直接使用
		distFS = h.webDistFS
	}

	// 检查 embed 文件系统是否有内容
	if entries, err := fs.ReadDir(distFS, "."); err == nil && len(entries) > 0 {
		// 使用 embed 的文件系统
		httpFS := http.FS(distFS)
		// 使用 Fiber 的 FileSystem 方法
		app.Use("/", func(c *fiber.Ctx) error {
			path := c.Path()
			if path == "/" {
				path = "/index.html"
			}
			file, err := httpFS.Open(path)
			if err != nil {
				return c.Next()
			}
			defer file.Close()

			stat, err := file.Stat()
			if err != nil {
				return c.Next()
			}

			// 设置内容类型
			contentType := "text/html"
			if path[len(path)-4:] == ".css" {
				contentType = "text/css"
			} else if path[len(path)-3:] == ".js" {
				contentType = "application/javascript"
			}

			c.Type(contentType)
			return c.SendStream(file, int(stat.Size()))
		})
		h.logger.Info("web_ui_served_from_embed")
		return
	}

	// 如果 embed 失败或为空，检查是否存在 web/dist 目录（开发环境）
	if _, err := os.Stat("web/dist"); err == nil {
		// 使用文件系统（开发环境）
		app.Static("/", "./web/dist", fiber.Static{
			Index:  "index.html",
			MaxAge: 3600,
		})
		h.logger.Info("web_ui_served_from_filesystem", slog.String("path", "web/dist"))
	} else {
		// 如果没有 dist 目录，提供一个简单的提示页面
		app.Get("/", func(c *fiber.Ctx) error {
			return c.SendString(`
				<!DOCTYPE html>
				<html>
				<head><title>套利监控系统</title></head>
				<body style="font-family: Arial; padding: 40px; text-align: center;">
					<h1>Web UI 未构建</h1>
					<p>请先构建前端：</p>
					<pre style="background: #f5f5f5; padding: 20px; display: inline-block; border-radius: 4px;">
cd web
npm install
npm run build
					</pre>
					<p style="margin-top: 20px;">
						<a href="/api/v1/comparison" style="color: #007bff;">查看 API 文档</a>
					</p>
				</body>
				</html>
			`)
		})
		h.logger.Warn("web_dist_not_found", slog.String("hint", "run 'cd web && npm run build' first"))
	}

	// API 路由不受影响
}

package http

import (
	"goKit/internal/application/service"

	"github.com/gofiber/fiber/v2"
)

// RegisterRoutes 集中注册所有 HTTP 路由
func RegisterRoutes(
	app *fiber.App,
	authHandler *AuthHandler,
	uploadHandler *UploadHandler,
	memoHandler *MemoHandler,
	pageHandler *PageHandler,
	timelineHandler *TimelineHandler,
	trashHandler *TrashHandler,
	searchHandler *SearchHandler,
	tagHandler *TagHandler,
	authService *service.AuthService,
) {
	// ========== 认证相关路由（独立路径，不在 /api/v1 下） ==========
	auth := app.Group("/auth")
	auth.Post("/register", authHandler.Register)
	auth.Post("/login", authHandler.Login)
	auth.Post("/refresh", authHandler.Refresh)

	api := app.Group("/api/v1")

	// ========== 公开路由（无需鉴权） ==========
	api.Get("/public/pages/:share_id", pageHandler.GetByShareID)

	// ========== 所有路由默认公开访问（使用混合模式） ==========
	// 使用 OptionalAuthMiddleware：有 Token 则验证，无 Token 则允许继续（在 Handler 中检查权限）
	mixed := api.Group("", OptionalAuthMiddleware(authService))

	// 上传（需要登录，在 Handler 中检查）
	mixed.Post("/upload", uploadHandler.Upload)

	// Memo 相关（默认公开访问）
	mixed.Post("/memos", memoHandler.Create)
	mixed.Get("/memos/:id", memoHandler.Get)
	mixed.Put("/memos/:id", memoHandler.Update)

	// Page 相关（默认公开访问）
	mixed.Post("/pages", pageHandler.CreateOrUpdate)
	mixed.Get("/pages/:id", pageHandler.Get)
	mixed.Post("/pages/:id/share", pageHandler.Share)
	mixed.Delete("/pages/:id", pageHandler.Delete) // 删除需要登录，在 Handler 中检查
	mixed.Delete("/memos/:id", memoHandler.Delete) // 删除需要登录，在 Handler 中检查

	// Timeline（默认公开访问）
	mixed.Get("/timeline", timelineHandler.Get)

	// 回收站（需要登录）
	trash := api.Group("/trash", AuthMiddleware(authService))
	trash.Get("", trashHandler.GetTrash)
	trash.Post("/:type/:id/restore", trashHandler.RestoreItem)
	trash.Delete("/:type/:id", trashHandler.PermanentlyDeleteItem)

	// 搜索（默认公开访问）
	mixed.Get("/search", searchHandler.Search)

	// 标签相关（需要登录）
	tags := api.Group("/tags", AuthMiddleware(authService))
	tags.Get("", tagHandler.GetTags)
	tags.Get("/:name/timeline", tagHandler.GetTagTimeline)
	tags.Delete("/:id", tagHandler.DeleteTag)
}

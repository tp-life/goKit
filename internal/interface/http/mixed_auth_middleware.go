package http

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"goKit/internal/application/service"
)

// OptionalAuthMiddleware 混合模式权限中间件
// 默认允许所有请求（公开访问）
// 如果有 JWT Token，则解析并设置 userID，但不会阻止未登录用户访问
func OptionalAuthMiddleware(authService *service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// 尝试解析 JWT Token（可选）
		authHeader := c.Get("Authorization")
		var userID uint64

		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token := parts[1]
				if uid, err := authService.ValidateToken(token); err == nil {
					userID = uid
					c.Locals(UserIDKey, userID)
				}
			}
		}

		// 允许所有请求继续（包括未登录用户）
		// Handler 中可以根据 userID 是否为 0 来决定是否允许操作
		return c.Next()
	}
}

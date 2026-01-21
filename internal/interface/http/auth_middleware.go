package http

import (
	"strings"

	"github.com/gofiber/fiber/v2"
	"goKit/internal/application/service"
)

const UserIDKey = "user_id"

// AuthMiddleware JWT 鉴权中间件
func AuthMiddleware(authService *service.AuthService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Status(401).JSON(fiber.Map{
				"error": "missing authorization header",
			})
		}

		// 提取 Bearer token
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return c.Status(401).JSON(fiber.Map{
				"error": "invalid authorization header format",
			})
		}

		token := parts[1]
		userID, err := authService.ValidateToken(token)
		if err != nil {
			return c.Status(401).JSON(fiber.Map{
				"error": "invalid or expired token",
			})
		}

		// 将 userID 存入 Context
		c.Locals(UserIDKey, userID)
		return c.Next()
	}
}

// GetUserID 从 Context 获取 UserID
func GetUserID(c *fiber.Ctx) uint64 {
	userID, ok := c.Locals(UserIDKey).(uint64)
	if !ok {
		return 0
	}
	return userID
}

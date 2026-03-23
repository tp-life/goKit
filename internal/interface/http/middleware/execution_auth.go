package middleware

import (
	"crypto/subtle"
	"os"
	"strings"

	"goKit/internal/interface/http/response"
	"goKit/pkg/kit/web"

	"github.com/gofiber/fiber/v2"
)

func RequireExecutionAuth(cfg web.Config) fiber.Handler {
	tokenEnv := strings.TrimSpace(cfg.ExecutionAPITokenEnv)

	return func(c *fiber.Ctx) error {
		if tokenEnv == "" {
			return response.ErrUnauthorized("执行接口未配置鉴权 token")
		}

		expected := strings.TrimSpace(os.Getenv(tokenEnv))
		if expected == "" {
			return response.ErrUnauthorized("执行接口鉴权 token 未加载")
		}

		provided := strings.TrimSpace(c.Get("X-Execution-Token"))
		if provided == "" {
			auth := strings.TrimSpace(c.Get(fiber.HeaderAuthorization))
			if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
				provided = strings.TrimSpace(auth[len("Bearer "):])
			}
		}

		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			return response.ErrUnauthorized("未授权的执行接口访问")
		}
		return c.Next()
	}
}

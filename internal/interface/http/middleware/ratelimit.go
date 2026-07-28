package middleware

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"

	"goKit/internal/interface/http/response"
)

// LoginRateLimiter 登录接口限流（按客户端 IP，进程内计数）。
// max<=0 默认 10 次，window<=0 默认 1 分钟；超限返回 429/42900。
// 多实例部署时各实例独立计数，如需全局限流可替换为 Redis 存储。
func LoginRateLimiter(max int, window time.Duration) fiber.Handler {
	if max <= 0 {
		max = 10
	}
	if window <= 0 {
		window = time.Minute
	}
	return limiter.New(limiter.Config{
		Max:        max,
		Expiration: window,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return response.ErrTooManyRequests("登录尝试过于频繁，请稍后再试")
		},
	})
}

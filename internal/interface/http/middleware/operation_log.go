package middleware

import (
	"context"
	"log/slog"
	"time"

	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/oplog"
	domain "goKit/internal/domain/oplog"
	"goKit/pkg/kit/auth"
)

// 操作日志异步落库超时
const oplogTimeout = 3 * time.Second

// OperationLog 操作日志中间件：记录写操作（POST/PUT/DELETE/PATCH）的审计信息。
// 必须挂在 JWTAuth 之后才能取到操作人；GET 请求不记录。
// 登录接口（公开）使用时从请求体提取账号作为操作人。
// 落库异步执行，失败仅告警，不阻塞请求。
func OperationLog(svc *oplog.OplogService, l *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		switch c.Method() {
		case fiber.MethodGet, fiber.MethodHead, fiber.MethodOptions:
			return c.Next()
		}

		// 未登录场景（登录接口）尝试从请求体取账号；BodyParser 可被后续 handler 再次调用
		var bodyUsername string
		if auth.FromContext(c.UserContext()) == nil {
			var body struct {
				Username string `json:"username"`
			}
			_ = c.BodyParser(&body)
			bodyUsername = body.Username
		}

		start := time.Now()
		err := c.Next()

		entry := &domain.OperationLog{
			Username:  bodyUsername,
			Method:    c.Method(),
			Path:      c.Route().Path,
			IP:        c.IP(),
			Status:    c.Response().StatusCode(),
			LatencyMs: time.Since(start).Milliseconds(),
		}
		if u := auth.FromContext(c.UserContext()); u != nil {
			entry.UserID = u.UserID
			entry.Username = u.Username
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), oplogTimeout)
			defer cancel()
			if e := svc.Record(ctx, entry); e != nil {
				l.Warn("operation_log_write_failed", slog.Any("err", e))
			}
		}()
		return err
	}
}

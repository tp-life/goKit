package middleware

import (
	"log/slog"

	"goKit/internal/interface/http/response" // 引入响应包

	"github.com/gofiber/fiber/v2"
)

func ErrorHandler(logger *slog.Logger) fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := c.Next()
		if err == nil {
			return nil
		}

		// 拦截自定义的 AppError
		if appErr, ok := err.(*response.AppError); ok {
			if appErr.HTTPCode >= 500 {
				logger.ErrorContext(c.UserContext(), "http_internal_error",
					slog.String("path", c.Path()),
					slog.String("biz_msg", appErr.Message),
					slog.Any("raw_error", appErr.RawError),
				)
			}
			return c.Status(appErr.HTTPCode).JSON(response.BaseResponse{
				Code:    appErr.BusinessCode,
				Message: appErr.Message,
			})
		}

		// 处理 Fiber 框架原生错误
		if fiberErr, ok := err.(*fiber.Error); ok {
			return c.Status(fiberErr.Code).JSON(response.BaseResponse{
				Code:    fiberErr.Code,
				Message: fiberErr.Message,
			})
		}

		// 兜底未知错误
		logger.ErrorContext(c.UserContext(), "http_unknown_error", slog.Any("err", err))
		return c.Status(500).JSON(response.BaseResponse{
			Code:    response.CodeInternalServer,
			Message: "internal server error",
		})
	}
}

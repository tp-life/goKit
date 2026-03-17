package response

import "github.com/gofiber/fiber/v2"

type BaseResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func Success(c *fiber.Ctx, data any) error {
	return c.JSON(BaseResponse{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	})
}

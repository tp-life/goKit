package http

import (
	"strconv"

	"goKit/internal/application/dto"
	"goKit/internal/application/service"

	"github.com/gofiber/fiber/v2"
)

type UserHandler struct {
	svc *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}
func (h *UserHandler) Create(c *fiber.Ctx) error {
	var req dto.CreateUserReq
	if err := c.BodyParser(&req); err != nil {
		// 以前: return Error(c, 400, 40001, "invalid request")
		// 现在: 极其清晰的语义
		return BadRequest(c, "JSON解析失败，请检查请求体格式")
	}

	id, err := h.svc.CreateUser(c.UserContext(), req)
	if err != nil {
		// 统一走 500 内部错误
		return InternalServer(c, err.Error())
	}

	return Success(c, fiber.Map{"id": id})
}

func (h *UserHandler) Get(c *fiber.Ctx) error {
	id, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	user, err := h.svc.GetUser(c.UserContext(), id)

	if err != nil {
		if err.Error() == "user not found" {
			// 直接丢出一个 NotFound
			return NotFound(c, "您要查找的用户不存在")
		}
		return InternalServer(c, "获取用户信息失败")
	}

	return Success(c, user)
}

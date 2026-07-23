package handler

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/interface/http/response"
	"goKit/internal/modules/system/application/dto"
	"goKit/internal/modules/system/application/service"
)

type AuthHandler struct {
	svc *service.AuthService
}

func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Login POST /auth/login
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req dto.LoginReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if req.Username == "" || req.Password == "" {
		return response.ErrBadRequest("用户名和密码不能为空")
	}
	resp, err := h.svc.Login(c.UserContext(), req)
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, resp)
}

// Profile GET /auth/profile
func (h *AuthHandler) Profile(c *fiber.Ctx) error {
	resp, err := h.svc.Profile(c.UserContext())
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, resp)
}

// ChangePassword PUT /auth/password
func (h *AuthHandler) ChangePassword(c *fiber.Ctx) error {
	var req dto.ChangePwdReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if req.NewPassword == "" {
		return response.ErrBadRequest("新密码不能为空")
	}
	if err := h.svc.ChangePassword(c.UserContext(), req); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

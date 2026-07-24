package handler

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/auth"
	"goKit/internal/interface/http/response"
)

type AuthHandler struct {
	svc *auth.AuthService
}

func NewAuthHandler(svc *auth.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Login POST /auth/login
func (h *AuthHandler) Login(c *fiber.Ctx) error {
	var req auth.LoginReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if err := Validate(c, &req); err != nil {
		return err
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
	var req auth.ChangePwdReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if err := Validate(c, &req); err != nil {
		return err
	}
	if err := h.svc.ChangePassword(c.UserContext(), req); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

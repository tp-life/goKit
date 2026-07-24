package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/shared"
	"goKit/internal/application/user"
	"goKit/internal/interface/http/response"
)

// parseID 解析路径参数 :id
func parseID(c *fiber.Ctx) (uint64, error) {
	id, err := strconv.ParseUint(c.Params("id"), 10, 64)
	if err != nil || id == 0 {
		return 0, response.ErrBadRequest("非法的ID参数")
	}
	return id, nil
}

// parsePage 解析分页查询参数
func parsePage(c *fiber.Ctx) shared.PageReq {
	var page shared.PageReq
	_ = c.QueryParser(&page)
	return page
}

type UserHandler struct {
	svc *user.UserService
}

func NewUserHandler(svc *user.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) Create(c *fiber.Ctx) error {
	var req user.CreateUserReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if err := Validate(c, &req); err != nil {
		return err
	}
	id, err := h.svc.Create(c.UserContext(), req)
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, fiber.Map{"id": id})
}

func (h *UserHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req user.UpdateUserReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if err := Validate(c, &req); err != nil {
		return err
	}
	if err := h.svc.Update(c.UserContext(), id, req); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

func (h *UserHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

func (h *UserHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	user, err := h.svc.Get(c.UserContext(), id)
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, user)
}

func (h *UserHandler) List(c *fiber.Ctx) error {
	resp, err := h.svc.List(c.UserContext(), parsePage(c))
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, resp)
}

func (h *UserHandler) AssignRoles(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req user.AssignRolesReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if err := Validate(c, &req); err != nil {
		return err
	}
	if err := h.svc.AssignRoles(c.UserContext(), id, req); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

func (h *UserHandler) ResetPassword(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req user.ResetPwdReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if err := Validate(c, &req); err != nil {
		return err
	}
	if err := h.svc.ResetPassword(c.UserContext(), id, req); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

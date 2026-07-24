package handler

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/role"
	"goKit/internal/interface/http/response"
)

type RoleHandler struct {
	svc *role.RoleService
}

func NewRoleHandler(svc *role.RoleService) *RoleHandler {
	return &RoleHandler{svc: svc}
}

func (h *RoleHandler) Create(c *fiber.Ctx) error {
	var req role.CreateRoleReq
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

func (h *RoleHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req role.UpdateRoleReq
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

func (h *RoleHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

func (h *RoleHandler) Get(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	role, err := h.svc.Get(c.UserContext(), id)
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, role)
}

func (h *RoleHandler) List(c *fiber.Ctx) error {
	resp, err := h.svc.List(c.UserContext(), parsePage(c))
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, resp)
}

// AssignMenus 给角色分配菜单权限
func (h *RoleHandler) AssignMenus(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req role.AssignMenusReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if err := Validate(c, &req); err != nil {
		return err
	}
	if err := h.svc.AssignMenus(c.UserContext(), id, req); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

// AssignUsers 给角色分配用户
func (h *RoleHandler) AssignUsers(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req role.AssignUsersReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if err := Validate(c, &req); err != nil {
		return err
	}
	if err := h.svc.AssignUsers(c.UserContext(), id, req); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

// GetUserIDs 查询角色下的用户
func (h *RoleHandler) GetUserIDs(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	ids, err := h.svc.GetUserIDs(c.UserContext(), id)
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, fiber.Map{"user_ids": ids})
}

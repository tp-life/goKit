package handler

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/interface/http/response"
	"goKit/internal/modules/system/application/dto"
	"goKit/internal/modules/system/application/service"
)

type RoleHandler struct {
	svc *service.RoleService
}

func NewRoleHandler(svc *service.RoleService) *RoleHandler {
	return &RoleHandler{svc: svc}
}

func (h *RoleHandler) Create(c *fiber.Ctx) error {
	var req dto.CreateRoleReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if req.Name == "" || req.Code == "" {
		return response.ErrBadRequest("角色名称和编码不能为空")
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
	var req dto.UpdateRoleReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
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
	var req dto.AssignMenusReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
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
	var req dto.AssignUsersReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
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

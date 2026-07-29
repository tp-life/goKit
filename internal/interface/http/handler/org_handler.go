package handler

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/dept"
	"goKit/internal/application/menu"
	"goKit/internal/interface/http/response"
)

type DeptHandler struct {
	svc *dept.DeptService
}

func NewDeptHandler(svc *dept.DeptService) *DeptHandler {
	return &DeptHandler{svc: svc}
}

func (h *DeptHandler) Create(c *fiber.Ctx) error {
	var req dept.CreateDeptReq
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

func (h *DeptHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req dept.UpdateDeptReq
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

func (h *DeptHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

func (h *DeptHandler) Tree(c *fiber.Ctx) error {
	tree, err := h.svc.Tree(c.UserContext())
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, tree)
}

type MenuHandler struct {
	svc *menu.MenuService
}

func NewMenuHandler(svc *menu.MenuService) *MenuHandler {
	return &MenuHandler{svc: svc}
}

func (h *MenuHandler) Create(c *fiber.Ctx) error {
	var req menu.CreateMenuReq
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

func (h *MenuHandler) Update(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	var req menu.UpdateMenuReq
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

func (h *MenuHandler) Delete(c *fiber.Ctx) error {
	id, err := parseID(c)
	if err != nil {
		return err
	}
	if err := h.svc.Delete(c.UserContext(), id); err != nil {
		return mapErr(err)
	}
	return response.Success(c, nil)
}

func (h *MenuHandler) Tree(c *fiber.Ctx) error {
	tree, err := h.svc.Tree(c.UserContext())
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, tree)
}

// Mine GET /menus/mine 当前用户可见菜单树（登录即可，无需额外权限点）
func (h *MenuHandler) Mine(c *fiber.Ctx) error {
	tree, err := h.svc.Mine(c.UserContext())
	if err != nil {
		return mapErr(err)
	}
	return response.Success(c, tree)
}

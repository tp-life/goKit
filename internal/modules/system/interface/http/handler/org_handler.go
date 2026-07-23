package handler

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/interface/http/response"
	"goKit/internal/modules/system/application/dto"
	"goKit/internal/modules/system/application/service"
)

type DeptHandler struct {
	svc *service.DeptService
}

func NewDeptHandler(svc *service.DeptService) *DeptHandler {
	return &DeptHandler{svc: svc}
}

func (h *DeptHandler) Create(c *fiber.Ctx) error {
	var req dto.CreateDeptReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if req.Name == "" {
		return response.ErrBadRequest("部门名称不能为空")
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
	var req dto.UpdateDeptReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
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
	svc *service.MenuService
}

func NewMenuHandler(svc *service.MenuService) *MenuHandler {
	return &MenuHandler{svc: svc}
}

func (h *MenuHandler) Create(c *fiber.Ctx) error {
	var req dto.CreateMenuReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
	}
	if req.Title == "" {
		return response.ErrBadRequest("菜单名称不能为空")
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
	var req dto.UpdateMenuReq
	if err := c.BodyParser(&req); err != nil {
		return response.ErrBadRequest("JSON解析失败，请检查请求体格式")
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

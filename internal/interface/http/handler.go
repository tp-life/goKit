package http

import (
	"strconv"

	"github.com/gofiber/fiber/v2"
	"goKit/internal/application/dto"
	"goKit/internal/application/service"
)

type UserHandler struct {
	svc *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

func (h *UserHandler) RegisterRoutes(app *fiber.App) {
	g := app.Group("/api/v1")
	g.Post("/users", h.Create)
	g.Get("/users/:id", h.Get)
}

func (h *UserHandler) Create(c *fiber.Ctx) error {
	var req dto.CreateUserReq
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
	}
	id, err := h.svc.CreateUser(c.UserContext(), req)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}
	return c.Status(201).JSON(fiber.Map{"id": id})
}

func (h *UserHandler) Get(c *fiber.Ctx) error {
	id, _ := strconv.ParseUint(c.Params("id"), 10, 64)
	user, err := h.svc.GetUser(c.UserContext(), id)
	if err != nil {
		return c.Status(404).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(user)
}

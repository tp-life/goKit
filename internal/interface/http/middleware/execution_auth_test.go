package middleware

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"goKit/pkg/kit/web"

	"github.com/gofiber/fiber/v2"
)

func TestRequireExecutionAuth_BlocksMissingToken(t *testing.T) {
	app := fiber.New()
	app.Use(ErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.Post("/protected", RequireExecutionAuth(web.Config{ExecutionAPITokenEnv: "EXECUTION_API_TOKEN"}), func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected status 401, got %d", resp.StatusCode)
	}
}

func TestRequireExecutionAuth_AcceptsBearerToken(t *testing.T) {
	t.Setenv("EXECUTION_API_TOKEN", "secret-token")

	app := fiber.New()
	app.Use(ErrorHandler(slog.New(slog.NewTextHandler(io.Discard, nil))))
	app.Post("/protected", RequireExecutionAuth(web.Config{ExecutionAPITokenEnv: "EXECUTION_API_TOKEN"}), func(c *fiber.Ctx) error {
		return c.SendStatus(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/protected", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}
}

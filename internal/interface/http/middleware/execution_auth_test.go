package middleware

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"goKit/pkg/kit/web"

	"github.com/gofiber/fiber/v2"
)

func TestIsLocalIP_AllowsLoopback(t *testing.T) {
	if !isLocalIP(net.ParseIP("127.0.0.1")) {
		t.Fatal("expected loopback IP to be treated as local")
	}
}

func TestIsLocalIP_AllowsInterfaceAddress(t *testing.T) {
	orig := interfaceAddrsFn
	interfaceAddrsFn = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("192.168.31.9"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}
	t.Cleanup(func() {
		interfaceAddrsFn = orig
	})

	if !isLocalIP(net.ParseIP("192.168.31.9")) {
		t.Fatal("expected host interface IP to be treated as local")
	}
}

func TestIsLocalIP_RejectsRemoteAddress(t *testing.T) {
	orig := interfaceAddrsFn
	interfaceAddrsFn = func() ([]net.Addr, error) {
		return []net.Addr{
			&net.IPNet{IP: net.ParseIP("192.168.31.9"), Mask: net.CIDRMask(24, 32)},
		}, nil
	}
	t.Cleanup(func() {
		interfaceAddrsFn = orig
	})

	if isLocalIP(net.ParseIP("203.0.113.8")) {
		t.Fatal("expected remote IP to require auth")
	}
}

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
	req.RemoteAddr = "203.0.113.8:34567"
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

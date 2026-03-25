package middleware

import (
	"crypto/subtle"
	"net"
	"os"
	"strings"

	"goKit/internal/interface/http/response"
	"goKit/pkg/kit/web"

	"github.com/gofiber/fiber/v2"
)

var interfaceAddrsFn = net.InterfaceAddrs

func RequireExecutionAuth(cfg web.Config) fiber.Handler {
	tokenEnv := strings.TrimSpace(cfg.ExecutionAPITokenEnv)

	return func(c *fiber.Ctx) error {
		if isLocalExecutionRequest(c) {
			return c.Next()
		}

		if tokenEnv == "" {
			return response.ErrUnauthorized("执行接口未配置鉴权 token")
		}

		expected := strings.TrimSpace(os.Getenv(tokenEnv))
		if expected == "" {
			return response.ErrUnauthorized("执行接口鉴权 token 未加载")
		}

		provided := strings.TrimSpace(c.Get("X-Execution-Token"))
		if provided == "" {
			auth := strings.TrimSpace(c.Get(fiber.HeaderAuthorization))
			if strings.HasPrefix(strings.ToLower(auth), "bearer ") {
				provided = strings.TrimSpace(auth[len("Bearer "):])
			}
		}

		if subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			return response.ErrUnauthorized("未授权的执行接口访问")
		}
		return c.Next()
	}
}

func isLocalExecutionRequest(c *fiber.Ctx) bool {
	return isLocalIP(c.Context().RemoteIP())
}

func isLocalIP(clientIP net.IP) bool {
	if clientIP == nil {
		return false
	}
	if clientIP.IsLoopback() {
		return true
	}

	interfaceAddrs, err := interfaceAddrsFn()
	if err != nil {
		return false
	}
	for _, addr := range interfaceAddrs {
		switch v := addr.(type) {
		case *net.IPNet:
			if v.IP != nil && v.IP.Equal(clientIP) {
				return true
			}
		case *net.IPAddr:
			if v.IP != nil && v.IP.Equal(clientIP) {
				return true
			}
		}
	}
	return false
}

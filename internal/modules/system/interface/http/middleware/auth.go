package middleware

import (
	"strings"

	"github.com/gofiber/fiber/v2"

	"goKit/internal/interface/http/response"
	"goKit/pkg/kit/auth"
)

// JWTAuth 认证中间件：解析 Bearer Token，将 CurrentUser 注入 ctx
func JWTAuth(tokens *auth.TokenManager) fiber.Handler {
	return func(c *fiber.Ctx) error {
		header := c.Get("Authorization")
		tokenStr, found := strings.CutPrefix(header, "Bearer ")
		if !found || tokenStr == "" {
			return response.ErrUnauthorized("缺少访问令牌")
		}
		claims, err := tokens.Parse(tokenStr)
		if err != nil {
			return response.ErrUnauthorized("令牌无效或已过期")
		}
		ctx := auth.WithCurrentUser(c.UserContext(), &auth.CurrentUser{
			UserID:   claims.UserID,
			Username: claims.Username,
			IsSuper:  claims.IsSuper,
		})
		c.SetUserContext(ctx)
		return c.Next()
	}
}

// RequirePerm 功能权限中间件：校验当前用户是否拥有指定权限点
func RequirePerm(authorizer auth.Authorizer, permCode string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		cu := auth.FromContext(c.UserContext())
		if cu == nil {
			return response.ErrUnauthorized("")
		}
		if cu.IsSuper {
			return c.Next()
		}
		ok, err := authorizer.CheckPerm(c.UserContext(), cu.UserID, permCode)
		if err != nil {
			return response.ErrInternal(err, "权限校验失败")
		}
		if !ok {
			return response.ErrForbidden("")
		}
		return c.Next()
	}
}

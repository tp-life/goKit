package auth

import "context"

// CurrentUser 当前登录用户，由认证中间件写入 ctx，HTTP/gRPC 共用
type CurrentUser struct {
	UserID   uint64
	Username string
	IsSuper  bool
}

type currentUserKey struct{}

// WithCurrentUser 将当前用户注入 context
func WithCurrentUser(ctx context.Context, u *CurrentUser) context.Context {
	return context.WithValue(ctx, currentUserKey{}, u)
}

// FromContext 取出当前用户，未登录返回 nil
func FromContext(ctx context.Context) *CurrentUser {
	if u, ok := ctx.Value(currentUserKey{}).(*CurrentUser); ok {
		return u
	}
	return nil
}

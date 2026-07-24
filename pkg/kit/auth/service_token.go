package auth

import (
	"context"

	grpcauth "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/auth"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// NewServiceTokenAuthFunc 服务间共享密钥认证（gRPC 服务端）。
// 校验调用方 metadata 中的 "authorization: Bearer <token>"，用于保护
// AuthzService 等内部服务不被未授权访问。
// token 为空时返回 nil（不启用认证），由 rpc.Server 的 AuthFunc 空值判断跳过。
func NewServiceTokenAuthFunc(token string) grpcauth.AuthFunc {
	if token == "" {
		return nil
	}
	return func(ctx context.Context) (context.Context, error) {
		got, err := grpcauth.AuthFromMD(ctx, "bearer")
		if err != nil {
			return nil, status.Errorf(codes.Unauthenticated, "missing bearer token")
		}
		if got != token {
			return nil, status.Errorf(codes.Unauthenticated, "invalid service token")
		}
		return ctx, nil
	}
}

// bearerTokenCreds 调用方凭证：为每个 RPC 附加 Bearer Token
type bearerTokenCreds struct {
	token string
}

func (c bearerTokenCreds) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + c.token}, nil
}

// RequireTransportSecurity 返回 false 以支持内网明文 gRPC；
// 跨不可信网络部署时应改用 TLS/mTLS
func (c bearerTokenCreds) RequireTransportSecurity() bool { return false }

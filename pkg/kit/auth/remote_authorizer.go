package auth

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	authzv1 "goKit/api/gen/authz/v1"
)

// RemoteAuthorizer 远程授权器（微服务模式），通过 gRPC 调用系统服务的 AuthzService。
// 业务服务使用它做权限校验时无需连接数据库，只需与系统服务共享同一 JWT secret。
type RemoteAuthorizer struct {
	client authzv1.AuthzServiceClient
	conn   *grpc.ClientConn
}

// NewRemoteAuthorizer 连接系统服务 gRPC 地址（如 "system-service:9090"）。
// token 为服务间共享密钥，与系统服务 authz.token 一致；为空表示不携带认证。
func NewRemoteAuthorizer(addr, token string) (*RemoteAuthorizer, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(bearerTokenCreds{token: token}),
	)
	if err != nil {
		return nil, fmt.Errorf("dial authz service %s: %w", addr, err)
	}
	return &RemoteAuthorizer{client: authzv1.NewAuthzServiceClient(conn), conn: conn}, nil
}

func (a *RemoteAuthorizer) CheckPerm(ctx context.Context, userID uint64, permCode string) (bool, error) {
	resp, err := a.client.CheckPerm(ctx, &authzv1.CheckPermRequest{
		UserId:   userID,
		PermCode: permCode,
	})
	if err != nil {
		return false, err
	}
	return resp.GetAllowed(), nil
}

// Close 关闭底层连接
func (a *RemoteAuthorizer) Close() error {
	return a.conn.Close()
}

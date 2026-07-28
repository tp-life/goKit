package auth

import (
	"context"
	"fmt"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	authzv1 "goKit/api/gen/authz/v1"
)

// RemoteUserProvider 远程用户信息提供者（微服务模式），
// 通过 gRPC 调用系统服务的 AuthzService，业务服务无需连接权限库。
type RemoteUserProvider struct {
	client authzv1.AuthzServiceClient
	conn   *grpc.ClientConn
}

// NewRemoteUserProvider 连接系统服务 gRPC 地址（如 "system-service:9090"）。
// token 为服务间共享密钥，与系统服务 authz.token 一致；为空表示不携带认证。
func NewRemoteUserProvider(addr, token string) (*RemoteUserProvider, error) {
	conn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(bearerTokenCreds{token: token}),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
	)
	if err != nil {
		return nil, fmt.Errorf("dial authz service %s: %w", addr, err)
	}
	return &RemoteUserProvider{client: authzv1.NewAuthzServiceClient(conn), conn: conn}, nil
}

func fromProtoUser(u *authzv1.UserInfo) *UserInfo {
	return &UserInfo{
		ID:       u.GetId(),
		Username: u.GetUsername(),
		Nickname: u.GetNickname(),
		Email:    u.GetEmail(),
		DeptID:   u.GetDeptId(),
		Status:   int8(u.GetStatus()),
		IsSuper:  u.GetIsSuper(),
	}
}

func (p *RemoteUserProvider) GetUser(ctx context.Context, id uint64) (*UserInfo, error) {
	resp, err := p.client.GetUser(ctx, &authzv1.GetUserRequest{UserId: id})
	if err != nil {
		return nil, err
	}
	if resp.GetUser() == nil {
		return nil, nil
	}
	return fromProtoUser(resp.GetUser()), nil
}

func (p *RemoteUserProvider) GetUsers(ctx context.Context, ids []uint64) (map[uint64]*UserInfo, error) {
	result := make(map[uint64]*UserInfo, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	resp, err := p.client.GetUsers(ctx, &authzv1.GetUsersRequest{UserIds: ids})
	if err != nil {
		return nil, err
	}
	for _, u := range resp.GetUsers() {
		result[u.GetId()] = fromProtoUser(u)
	}
	return result, nil
}

// Close 关闭底层连接
func (p *RemoteUserProvider) Close() error {
	return p.conn.Close()
}

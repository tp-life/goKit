// Package grpc 提供 system 模块的 gRPC 接口实现
package grpc

import (
	"context"

	authzv1 "goKit/api/gen/authz/v1"
	"goKit/internal/modules/system/application/service"
)

// AuthzServer 授权服务 gRPC 实现，供微服务模式下的业务服务远程调用
type AuthzServer struct {
	authzv1.UnimplementedAuthzServiceServer
	resolver *service.PermResolver
}

func NewAuthzServer(resolver *service.PermResolver) *AuthzServer {
	return &AuthzServer{resolver: resolver}
}

func (s *AuthzServer) CheckPerm(ctx context.Context, req *authzv1.CheckPermRequest) (*authzv1.CheckPermResponse, error) {
	set, err := s.resolver.Resolve(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}
	return &authzv1.CheckPermResponse{Allowed: set.Has(req.GetPermCode())}, nil
}

func (s *AuthzServer) GetUserPerms(ctx context.Context, req *authzv1.GetUserPermsRequest) (*authzv1.GetUserPermsResponse, error) {
	set, err := s.resolver.Resolve(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}
	codes := make([]string, 0, len(set.Codes))
	for c := range set.Codes {
		codes = append(codes, c)
	}
	return &authzv1.GetUserPermsResponse{IsSuper: set.IsSuper, PermCodes: codes}, nil
}

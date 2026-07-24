// Package grpc 提供 system 模块的 gRPC 接口实现
package grpc

import (
	"context"

	authzv1 "goKit/api/gen/authz/v1"
	authapp "goKit/internal/application/auth"
	kitauth "goKit/pkg/kit/auth"
)

// AuthzServer 授权服务 gRPC 实现，供微服务模式下的业务服务远程调用
type AuthzServer struct {
	authzv1.UnimplementedAuthzServiceServer
	resolver *authapp.PermResolver
	provider *authapp.LocalUserProvider
}

func NewAuthzServer(resolver *authapp.PermResolver, provider *authapp.LocalUserProvider) *AuthzServer {
	return &AuthzServer{resolver: resolver, provider: provider}
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

func toProtoUser(u *kitauth.UserInfo) *authzv1.UserInfo {
	return &authzv1.UserInfo{
		Id:       u.ID,
		Username: u.Username,
		Nickname: u.Nickname,
		Email:    u.Email,
		DeptId:   u.DeptID,
		Status:   int32(u.Status),
		IsSuper:  u.IsSuper,
	}
}

func (s *AuthzServer) GetUser(ctx context.Context, req *authzv1.GetUserRequest) (*authzv1.GetUserResponse, error) {
	user, err := s.provider.GetUser(ctx, req.GetUserId())
	if err != nil {
		return nil, err
	}
	resp := &authzv1.GetUserResponse{}
	if user != nil {
		resp.User = toProtoUser(user)
	}
	return resp, nil
}

func (s *AuthzServer) GetUsers(ctx context.Context, req *authzv1.GetUsersRequest) (*authzv1.GetUsersResponse, error) {
	users, err := s.provider.GetUsers(ctx, req.GetUserIds())
	if err != nil {
		return nil, err
	}
	resp := &authzv1.GetUsersResponse{Users: make([]*authzv1.UserInfo, 0, len(users))}
	for _, u := range users {
		resp.Users = append(resp.Users, toProtoUser(u))
	}
	return resp, nil
}

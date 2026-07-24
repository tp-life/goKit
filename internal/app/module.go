// Package app system 业务（认证/用户/角色/菜单/部门/数据权限）的 Fx 装配。
// 依赖按层组织：interface → application → domain ← infrastructure，
// 各层内部按业务聚合/用例分包（user/role/dept/menu/auth 等）。
package app

import (
	"go.uber.org/fx"

	"goKit/internal/application/auth"
	"goKit/internal/application/dept"
	"goKit/internal/application/menu"
	"goKit/internal/application/role"
	"goKit/internal/application/shared"
	"goKit/internal/application/user"
	"goKit/internal/infrastructure/persistence"
	"goKit/internal/infrastructure/seed"
	sysgrpc "goKit/internal/interface/grpc"
	syshttp "goKit/internal/interface/http"
	"goKit/internal/interface/http/handler"
)

// Module system 业务的 Fx 装配（Authorizer 的 local/remote 选择由 main 按配置注入）
var Module = fx.Options(
	// 基础设施层
	fx.Provide(
		persistence.NewUserRepository,
		persistence.NewRoleRepository,
		persistence.NewDeptRepository,
		persistence.NewMenuRepository,
		persistence.NewAssignRepository,
		seed.NewSeeder,
	),
	// 应用层
	fx.Provide(
		auth.NewPermResolver,
		auth.NewLocalAuthorizer,
		auth.NewLocalUserProvider,
		shared.NewDataScopeHelper,
		auth.NewAuthService,
		user.NewUserService,
		role.NewRoleService,
		menu.NewMenuService,
		dept.NewDeptService,
	),
	// 接口层
	fx.Provide(
		handler.NewAuthHandler,
		handler.NewUserHandler,
		handler.NewRoleHandler,
		handler.NewDeptHandler,
		handler.NewMenuHandler,
		syshttp.NewHTTPModule,
		sysgrpc.NewAuthzServer,
	),
)

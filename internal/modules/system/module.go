// Package system 系统管理模块：认证、用户、角色、菜单、部门与数据权限。
// 模块自含 domain/application/infrastructure/interface 四层，
// 微服务部署时可整体抽离为独立的系统服务。
package system

import (
	"go.uber.org/fx"

	"goKit/internal/modules/system/application/service"
	"goKit/internal/modules/system/infrastructure/persistence"
	"goKit/internal/modules/system/infrastructure/seed"
	sysgrpc "goKit/internal/modules/system/interface/grpc"
	syshttp "goKit/internal/modules/system/interface/http"
	"goKit/internal/modules/system/interface/http/handler"
)

// Module system 模块的 Fx 装配（Authorizer 的 local/remote 选择由 main 按配置注入）
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
		service.NewPermResolver,
		service.NewLocalAuthorizer,
		service.NewDataScopeHelper,
		service.NewAuthService,
		service.NewUserService,
		service.NewRoleService,
		service.NewMenuService,
		service.NewDeptService,
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

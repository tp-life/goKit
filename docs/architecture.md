# GoKit 技术设计文档

> 版本：v1.0 ｜ 适用范围：`cmd/server` + `internal/{app,domain,application,infrastructure,interface}` + `pkg/kit`
>
> 本文档描述 GoKit 的整体设计、分层架构、RBAC 权限体系（功能权限 + 数据权限）、双模式部署方案及关键技术点。

---

## 1. 设计目标

| 目标 | 说明 |
| :--- | :--- |
| 基础设施标准化 | DB / HTTP / gRPC / 日志 / 认证等底座收敛在 `pkg/kit`，业务不重复造轮子 |
| 业务逻辑纯粹化 | DDD 四层隔离，领域层零框架依赖、可单测 |
| 权限体系完整 | JWT 认证 + RBAC 功能权限 + 基于部门的数据权限，开箱即用 |
| 部署形态可切换 | 单机单二进制 ↔ 微服务，仅改配置，业务代码零改动 |

**非目标（有意不做）**：Redis 缓存、登录验证码/限流、操作日志、在线用户管理 —— 均留作扩展点（见 §9）。

---

## 2. 整体架构

### 2.1 分层与模块

```text
GoKit/
├── cmd/server/main.go            # 程序入口：Fx 装配、配置加载、local/remote 授权器选择
├── api/
│   ├── proto/authz/v1/           # 授权服务 proto 契约
│   └── gen/authz/v1/             # 生成的 pb 代码（make proto）
├── configs/                      # 配置（config.yaml）
├── internal/
│   ├── app/module.go             # system 业务的 Fx 装配（var Module）
│   ├── domain/                   # 领域层：按聚合分包，实体 + 仓储接口（端口），零框架依赖
│   │   ├── user/                 #   User 实体 + UserRepository
│   │   ├── role/                 #   Role + UserRole/RoleMenu/RoleDept 关联实体 + RoleRepository/AssignRepository
│   │   ├── dept/                 #   Dept 实体 + DeptRepository
│   │   ├── menu/                 #   Menu 实体 + MenuRepository
│   │   └── shared/datascope/     #   MergeDataScope 纯函数
│   ├── application/              # 应用层：按用例分包，业务编排、DTO、授权器本地实现、权限缓存
│   │   ├── auth/                 #   登录/个人信息 + PermResolver/LocalAuthorizer/LocalUserProvider
│   │   ├── user/                 #   用户管理用例
│   │   ├── role/                 #   角色管理用例
│   │   ├── dept/                 #   部门管理用例
│   │   ├── menu/                 #   菜单管理用例
│   │   └── shared/               #   通用分页 DTO、业务错误、DataScopeHelper
│   ├── infrastructure/           # 基础设施层：Gorm 仓储实现、AutoMigrate、播种
│   │   ├── persistence/          #   仓储接口的 Gorm 实现 + AutoMigrate
│   │   └── seed/                 #   首次启动数据播种
│   └── interface/                # 接口层：全局接入层与 system 业务接口同树合并
│       ├── grpc/                 #   gRPC AuthzService 实现
│       └── http/                 #   routes.go 挂载 system 全部路由（HTTPModule）
│           ├── handler/          #   system 业务 HTTP Handler（auth/user/role/dept/menu）
│           ├── middleware/       #   全局错误处理 + JWT 认证/权限点校验中间件
│           ├── response/         #   统一响应与错误码
│           └── router/           #   全局路由聚合器（/api/v1 分组、健康检查）
├── pkg/kit/                      # 通用底座
│   ├── db/                       #   Gorm 客户端（读写分离、闭包事务 WithTx）
│   ├── web/                      #   Fiber 服务（Sonic JSON、中间件插槽、生命周期）
│   ├── rpc/                      #   gRPC 服务（KeepAlive、拦截器链、AuthFunc 插槽）
│   ├── log/                      #   slog 封装（TraceID 注入）
│   ├── auth/                     #   JWT TokenManager、CurrentUser、Authorizer/UserProvider 端口、远程实现
│   └── cache/                    #   进程内缓存（版本号批量失效）
├── Dockerfile
└── docker-compose.yaml           # 单机一键部署（app + MySQL）
```

**设计要点**

- **分层与分包**：代码按 domain/application/infrastructure/interface 四层组织，各层内部按业务聚合（domain 的 user/role/dept/menu）与用例（application 的 auth/user/role/dept/menu）分包。微服务抽离时以 domain/application 的业务包边界为单位拆分。
- **依赖方向**：interface → application → domain ← infrastructure。领域层不 import 任何框架与基础设施。
- **依赖注入**：Uber Fx 全自动装配。`kit.Module`（底座）、`app.Module`（业务）各自聚合，main 只做配置绑定与模式选择。
- **中间件/拦截器插槽**：`web.AsMiddlewares`、`rpc.AsUnaryInterceptor` 基于 Fx Group，新增横切能力不改底座代码。

### 2.2 一次请求的处理链路

```text
请求 ──▶ recover → requestid → otel tracing → metrics → CORS（全局中间件，Fx Group 注入）
     ──▶ ErrorHandler（统一错误 → {code, message, data}）
     ──▶ ① JWTAuth        解析 Bearer Token → CurrentUser 注入 ctx
     ──▶ ② RequirePerm    功能权限校验（超管短路 → Authorizer.CheckPerm）
     ──▶ ③ Handler        BodyParser / 参数校验
     ──▶ ④ Service        业务编排（需要时 WithTx 事务）
     ──▶ ⑤ DataScope      数据权限：Build(ctx) → SQL 过滤条件
     ──▶ ⑥ Repository     Gorm 持久化（GetDB(ctx) 自动感知事务）
```

---

## 3. RBAC 权限模型

### 3.1 数据模型

```text
┌──────────┐   ┌──────────────┐   ┌──────────┐   ┌──────────────┐
│  User    │──<│  UserRole    │>──│   Role   │──<│  RoleMenu    │>──┐
│ id       │   │ user_id      │   │ id       │   │ role_id      │   │
│ username │   │ role_id      │   │ code     │   │ menu_id      │   │
│ password │   └──────────────┘   │ data_scope│  └──────────────┘   ▼
│ dept_id ─┼──┐                   │ status   │               ┌──────────┐
│ is_super │  │                   └────┬─────┘               │   Menu   │
└──────────┘  │                        │                     │ parent_id│
              │                        │                     │ perm_code│ ← 功能权限点
              │                        │                     └──────────┘
              │                        │                     ┌──────────┐
              │                        └──< RoleDept >──────>│   Dept   │
              └────────────────────────────────────────────>│  (树形)  │
                   用户归属部门                              └──────────┘
```

表结构（Gorm AutoMigrate，`database.auto_migrate=true` 时启动自动迁移）：

| 表 | 关键字段 | 说明 |
| :--- | :--- | :--- |
| `sys_users` | username(唯一), password(bcrypt), dept_id, status, is_super | 用户 |
| `sys_roles` | code(唯一), data_scope, status | 角色，data_scope 承载数据权限 |
| `sys_depts` | parent_id, sort | 部门树 |
| `sys_menus` | parent_id, type(目录/菜单/按钮), perm_code | 菜单树，权限点挂在按钮节点 |
| `sys_user_roles` | (user_id, role_id) 联合主键 | 用户-角色 |
| `sys_role_menus` | (role_id, menu_id) 联合主键 | 角色-权限点 |
| `sys_role_depts` | (role_id, dept_id) 联合主键 | 角色-自定义部门（data_scope=2） |
| `sys_operation_logs` | user_id, method, path, ip, status, latency_ms | 操作日志（审计），写操作自动记录 |

### 3.2 功能权限（AuthZ）

- 权限点以 `资源:动作` 命名（如 `system:user:list`、`system:role:assign-menu`），存储于 `sys_menus.perm_code`。
- 路由注册时逐点标注：`RequirePerm(az, "system:user:list")`。
- 校验链：超管短路 → `Authorizer.CheckPerm(uid, permCode)` → 用户角色 → 角色菜单 → perm_code 集合。
- **权限缓存**：`PermResolver` 将用户权限集合缓存于进程内（`pkg/kit/cache`，TTL 10 分钟兜底）；任何角色/菜单/分配/状态变更触发 `BumpVersion()` 批量失效。

### 3.3 数据权限

基于部门的数据范围，角色携带 `data_scope`：

| 值 | 范围 | 展开规则 |
| :--- | :--- | :--- |
| 1 | 全部数据 | 不加过滤 |
| 2 | 自定义部门 | `sys_role_depts` 中的 dept_ids |
| 3 | 本部门及以下 | user.dept_id + 子孙部门（BFS 展开） |
| 4 | 本部门 | user.dept_id |
| 5 | 仅本人 | SelfID = 当前用户 id |

**多角色合并策略：取最宽**。任一角色为“全部”即全部；否则部门集合并集；“仅本人”作为附加项；无角色/空范围则 DenyAll。

```text
DataScopeHelper.Build(ctx)
  └─▶ 当前用户角色列表 → 逐角色展开部门集合 → MergeDataScope（domain 纯函数）
        └─▶ Filter{All | DeptIDs | SelfID | DenyAll}
              └─▶ applyDataScope 翻译为 SQL：
                  All      → 无条件
                  DeptIDs  → WHERE dept_id IN (?)
                  +SelfID  → OR id = uid        -- 业务表可用 created_by
                  DenyAll  → WHERE 1 = 0
```

关键设计：**合并逻辑是纯领域函数**（`domain/shared/datascope/data_scope.go`，零 IO、完整单测），部门展开等 IO 由应用层预处理后传入；SQL 翻译在持久层完成，三层职责清晰。

### 3.4 认证（AuthN）

- 登录：`POST /auth/login` → bcrypt 校验 → 签发 JWT（HS256），Claims 含 `uid / username / is_super / exp`。
- 验签：`JWTAuth` 中间件解析 Bearer Token → `CurrentUser` 注入 ctx（HTTP/gRPC 共用 `auth.WithCurrentUser`）。
- 无状态：无服务端会话，天然适配多实例与微服务；各服务共享同一 `jwt.secret` 即可独立验签。

### 3.5 内置数据（播种）

`seed.enabled=true` 时首次启动幂等播种（admin 已存在则跳过）：根部门 → admin 超管（默认密码 `Admin@123`，需首登修改）→ 内置 admin 角色（全部数据权限）→ 系统管理菜单树（用户/角色/部门/菜单四组标准权限点）→ 角色关联。

---

## 4. 双模式部署

> 详细的架构图、请求/授权流转图、单机与微服务部署步骤（Compose / 二进制 / K8s / 可观测性 / 检查清单）见 [deployment.md](deployment.md)。

同一份代码、两种形态，切换只改配置：

```yaml
authz:
  mode: "local"          # local=单机（默认） | remote=微服务
  addr: "system:9090"    # remote 模式系统服务 gRPC 地址
  token: "<各服务共享>"   # 服务间共享密钥，非空时系统服务 gRPC 启用 Bearer 认证
jwt:
  secret: "<各服务共享>"  # 微服务间共享同一 secret 即可本地验签
```

### 4.1 抽象核心：Authorizer / UserProvider 端口

```go
// pkg/kit/auth/authorizer.go
type Authorizer interface {
    CheckPerm(ctx context.Context, userID uint64, permCode string) (bool, error)
}

// pkg/kit/auth/user_provider.go
type UserProvider interface {
    GetUser(ctx context.Context, id uint64) (*UserInfo, error)          // 不存在返回 (nil, nil)
    GetUsers(ctx context.Context, ids []uint64) (map[uint64]*UserInfo, error)
}
```

| 端口 | 单机 `local` | 微服务 `remote` |
| :--- | :--- | :--- |
| `Authorizer`（功能权限） | `LocalAuthorizer`（进程内查库 + 缓存） | `RemoteAuthorizer`（gRPC AuthzService） |
| `UserProvider`（用户信息） | `LocalUserProvider`（进程内查库） | `RemoteUserProvider`（gRPC AuthzService） |

main.go 中按 `authz.mode` 条件注入，业务代码只依赖端口，零改动。**其他模块需要用户信息时注入 `auth.UserProvider` 即可**，无需 import system 模块内部。

### 4.2 微服务形态

```text
┌────────────┐   gRPC (authz.v1)   ┌────────────┐
│ 系统服务    │◀──────────────────▶│ 业务服务    │
│ system模块  │  CheckPerm         │ RemoteAuth- │
│ AuthzSvc   │  GetUserPerms      │ orizer     │
└─────┬──────┘                     └─────┬──────┘
      ▼                                  │ JWT 本地验签（共享 secret）
    MySQL                                ▼
                                      各自数据源
```

- 契约：`api/proto/authz/v1/authz.proto`（`CheckPerm` / `GetUserPerms` / `GetUser` / `GetUsers`），`make proto` 重新生成。
- 权限数据与缓存集中在系统服务侧管理；业务服务无需连接权限库。
- **服务间认证**：`authz.token` 非空时，系统服务 gRPC 通过 `AuthFunc` 拦截器校验 `Bearer <token>`（`auth.NewServiceTokenAuthFunc`），远程客户端（`RemoteAuthorizer` / `RemoteUserProvider`）自动携带同一 token；为空则不启用（仅限内网可信环境）。跨不可信网络部署时应叠加 TLS/mTLS。
- 微服务抽离路径：以 domain/application 的业务包边界拆分（system 相关包整体迁出）→ 独立 cmd 入口 → 即系统服务。

### 4.3 单机形态

```bash
docker compose up -d --build   # app + MySQL，自动迁移 + 播种，开箱即用
```

---

## 5. 关键技术点

| 技术点 | 方案 | 位置 |
| :--- | :--- | :--- |
| JWT 认证 | `golang-jwt/jwt/v5` HS256，自定义 Claims | `pkg/kit/auth/token.go` |
| 密码哈希 | `golang.org/x/crypto/bcrypt` | 用户创建/改密/重置 |
| 权限缓存 | 进程内缓存 + 版本号批量失效（TTL 兜底） | `pkg/kit/cache`、`PermResolver` |
| 数据权限 | 领域纯函数合并 + 持久层 SQL 翻译 | `domain/shared/datascope/data_scope.go`、`applyDataScope` |
| 事务 | 闭包式 `WithTx`，ctx 传播 tx，仓储 `GetDB(ctx)` 自动感知 | `pkg/kit/db/client.go` |
| 依赖注入 | Uber Fx，Module 聚合 + Group 插槽 | `kit.Module`、`app.Module` |
| 输入校验 | `go-playground/validator/v10`，DTO validate tag + 统一 400 映射 | `internal/interface/http/handler/validate.go` |
| 服务间认证 | 共享密钥 Bearer 校验（AuthFunc 插槽），客户端 PerRPCCredentials 携带 | `pkg/kit/auth/service_token.go` |
| 统一响应 | `{code, message, data}` + `AppError` 业务码（40100/40300…） | `internal/interface/http/response` |
| 错误处理 | `ErrorHandler` 中间件集中映射 AppError/fiber.Error/未知错误 | `internal/interface/http/middleware` |
| gRPC | KeepAlive + Recovery/Metrics/Validator/Auth 拦截器链 + Fx Group 插槽 | `pkg/kit/rpc` |
| 指标 | Prometheus `/metrics` 端点 + HTTP/gRPC 请求计数与耗时直方图（路径取路由模板防基数爆炸） | `pkg/kit/web/metrics.go`、`pkg/kit/rpc/metrics.go` |
| 链路追踪 | OTel SDK + OTLP gRPC 导出；HTTP（otelfiber）/gRPC（otelgrpc）自动埋点，`trace.endpoint` 为空时全局 Noop 零开销 | `pkg/kit/obs` |
| 探针 | `/api/v1/health`（liveness）/ `/api/v1/readyz`（readiness，探 DB 连通性） | `internal/interface/http/router` |
| 登录限流 | fiber limiter 按 IP 进程内计数（默认 10 次/分钟），超限返回 429/42900 | `internal/interface/http/middleware/ratelimit.go` |
| 操作日志 | 写操作（POST/PUT/DELETE）旁路异步落库审计，登录接口取请求体账号；`GET /logs` 查询 | `internal/interface/http/middleware/operation_log.go` |
| 读写分离 | Gorm dbresolver（replicas 配置即启用） | `pkg/kit/db` |
| 版本化迁移 | golang-migrate | `migrations/` + `pkg/kit/db/migrate.go` |
| JSON | Sonic 编解码 | `pkg/kit/web` |
| 服务间契约 | protobuf + gRPC（授权服务） | `api/proto/authz/v1` |

**新增依赖控制**：在 `golang-jwt/jwt/v5`、`golang.org/x/crypto` 基础上，本轮引入 `go-playground/validator/v10`（输入校验）；sonic 因 Go 1.26 兼容性升级至 v1.15.2。

---

## 6. API 一览（/api/v1）

> 完整的 OpenAPI 3.0 接口文档（请求/响应 schema、错误码、鉴权方式）见 [`api/openapi.yaml`](../api/openapi.yaml)。

| 模块 | 路由 | 权限点 |
| :--- | :--- | :--- |
| 认证 | `POST /auth/login` | 公开 |
| | `GET /auth/profile`、`PUT /auth/password` | 登录即可 |
| 用户 | `GET/POST /users`，`GET/PUT/DELETE /users/:id` | `system:user:*` |
| | `PUT /users/:id/roles` | `system:user:assign-role` |
| | `PUT /users/:id/password` | `system:user:reset-pwd` |
| 角色 | `GET/POST /roles`，`GET/PUT/DELETE /roles/:id` | `system:role:*` |
| | `PUT /roles/:id/menus` | `system:role:assign-menu` |
| | `PUT/GET /roles/:id/users` | `system:role:assign-user` / `system:role:list` |
| 部门 | `GET /depts/tree`，`POST/PUT/DELETE /depts...` | `system:dept:*` |
| 菜单 | `GET /menus/tree`，`POST/PUT/DELETE /menus...` | `system:menu:*` |
| 日志 | `GET /logs` | `system:log:list` |

用户列表查询已接入数据权限过滤，其他业务查询按 §3.3 的 `DataScopeHelper → Filter → applyDataScope` 三步接入。

---

## 7. 配置参考

| 配置项 | 说明 | 默认值 |
| :--- | :--- | :--- |
| `web.port` / `rpc.port` | HTTP / gRPC 端口 | `:8080` / `:9090` |
| `database.dsn` / `replicas` | 主库 / 从库连接串 | - |
| `database.auto_migrate` | 启动自动迁移 sys_* 表 | `false` |
| `database.migrations_enabled` | 启动执行 `migrations/` 版本化迁移（生产推荐，与 auto_migrate 同开时优先） | `false` |
| `jwt.secret` / `expire_minutes` | 签名密钥（生产必改）/ 有效期（分钟） | - / `120` |
| `authz.mode` / `addr` / `token` | 授权模式 local/remote / 系统服务地址 / 服务间共享密钥 | `local` / - / 空（不启用） |
| `trace.endpoint` / `service_name` / `sample_ratio` | OTLP 上报地址（空=不启用）/ 服务名 / 采样率 | 空 / `gokit` / `1.0` |
| `rate_limit.login_max` / `login_window` | 登录接口限流次数 / 窗口（按 IP，进程内） | `10` / `1m` |
| `seed.enabled` | 首次启动播种内置数据 | `false` |

---

## 8. 测试与质量

- 单测：`MergeDataScope` 七个分支、`PermSet.Has`、JWT 签发/解析/过期/错签/空令牌、`NewServiceTokenAuthFunc` 服务间认证。
- 应用层：`AuthService` 登录/改密/Profile、`LocalUserProvider`、`DataScopeHelper` 全分支、`UserService` 非事务路径（fake 仓储，`WithTx` 路径需真实 DB，留给集成测试）。
- 请求级：fiber `app.Test()` 覆盖登录/401/403/400/404 及正常链路（统一响应结构断言）。
- 校验：`Validate` 单测 + handler 层校验失败 400 用例。
- 校验命令：`go build ./...`、`go vet ./...`、`go test ./...`。
- 冒烟路径（需 MySQL）：登录拿 token → 无 token 401 → 无权限 403 → 建角色/分权限/分用户 → 数据权限过滤生效。

---

## 9. 扩展点（未内置）

| 扩展 | 接入方式 |
| :--- | :--- |
| Redis 权限缓存 | 替换 `pkg/kit/cache` 实现，`PermResolver` 不动 |
| 远程授权本地缓存 | 在 `RemoteAuthorizer` 外包短 TTL 缓存，降低 gRPC 调用频次 |
| 全局限流（多实例） | 进程内限流换 Redis 存储，替换 `LoginRateLimiter` 的 Storage |
| 登录验证码 | 新增全局中间件，`web.AsMiddlewares` 注入 |
| 在线用户 / 踢出 | 用户表加 token_version，JWT 校验时比对 |

# GoKit 技术设计文档

> 版本：v1.0 ｜ 适用范围：`cmd/server` + `internal/modules/system` + `pkg/kit`
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
│   ├── interface/http/           # 全局接入层：路由聚合 / 统一响应 / 错误处理中间件
│   └── modules/system/           # 系统模块（认证/用户/角色/菜单/部门/数据权限）
│       ├── domain/               #   领域层：实体、仓储接口、数据权限纯逻辑（零框架依赖）
│       │   ├── entity/           #   User/Role/Dept/Menu + 三张关联实体
│       │   ├── repository/       #   仓储接口（端口）
│       │   └── service/          #   MergeDataScope 纯函数
│       ├── application/          #   应用层：业务编排、DTO、授权器本地实现、权限缓存
│       ├── infrastructure/       #   基础设施层：Gorm 仓储实现、AutoMigrate、播种
│       └── interface/            #   接口层：HTTP Handler/中间件、gRPC AuthzService
├── pkg/kit/                      # 通用底座
│   ├── db/                       #   Gorm 客户端（读写分离、闭包事务 WithTx）
│   ├── web/                      #   Fiber 服务（Sonic JSON、中间件插槽、生命周期）
│   ├── rpc/                      #   gRPC 服务（KeepAlive、拦截器链、AuthFunc 插槽）
│   ├── log/                      #   slog 封装（TraceID 注入）
│   ├── auth/                     #   JWT TokenManager、CurrentUser、Authorizer 端口、远程实现
│   └── cache/                    #   进程内缓存（版本号批量失效）
├── Dockerfile
└── docker-compose.yaml           # 单机一键部署（app + MySQL）
```

**设计要点**

- **模块化边界**：`internal/modules/<模块>` 自含完整的 domain/application/infrastructure/interface 四层。system 模块即是未来的“系统服务”，抽离时整个目录搬走即可。
- **依赖方向**：interface → application → domain ← infrastructure。领域层不 import 任何框架与基础设施。
- **依赖注入**：Uber Fx 全自动装配。`kit.Module`（底座）、`system.Module`（业务）各自聚合，main 只做配置绑定与模式选择。
- **中间件/拦截器插槽**：`web.AsMiddlewares`、`rpc.AsUnaryInterceptor` 基于 Fx Group，新增横切能力不改底座代码。

### 2.2 一次请求的处理链路

```text
请求 ──▶ recover → requestid → CORS（全局中间件，Fx Group 注入）
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

关键设计：**合并逻辑是纯领域函数**（`domain/service/data_scope.go`，零 IO、完整单测），部门展开等 IO 由应用层预处理后传入；SQL 翻译在持久层完成，三层职责清晰。

### 3.4 认证（AuthN）

- 登录：`POST /auth/login` → bcrypt 校验 → 签发 JWT（HS256），Claims 含 `uid / username / is_super / exp`。
- 验签：`JWTAuth` 中间件解析 Bearer Token → `CurrentUser` 注入 ctx（HTTP/gRPC 共用 `auth.WithCurrentUser`）。
- 无状态：无服务端会话，天然适配多实例与微服务；各服务共享同一 `jwt.secret` 即可独立验签。

### 3.5 内置数据（播种）

`seed.enabled=true` 时首次启动幂等播种（admin 已存在则跳过）：根部门 → admin 超管（默认密码 `Admin@123`，需首登修改）→ 内置 admin 角色（全部数据权限）→ 系统管理菜单树（用户/角色/部门/菜单四组标准权限点）→ 角色关联。

---

## 4. 双模式部署

同一份代码、两种形态，切换只改配置：

```yaml
authz:
  mode: "local"          # local=单机（默认） | remote=微服务
  addr: "system:9090"    # remote 模式系统服务 gRPC 地址
jwt:
  secret: "<各服务共享>"  # 微服务间共享同一 secret 即可本地验签
```

### 4.1 抽象核心：Authorizer 端口

```go
// pkg/kit/auth/authorizer.go
type Authorizer interface {
    CheckPerm(ctx context.Context, userID uint64, permCode string) (bool, error)
}
```

| 模式 | 注入实现 | 行为 |
| :--- | :--- | :--- |
| 单机 `local` | `LocalAuthorizer`（system 模块应用层） | 进程内查库 + 内存缓存 |
| 微服务 `remote` | `RemoteAuthorizer`（`pkg/kit/auth`） | gRPC 调用系统服务 `AuthzService` |

main.go 中按 `authz.mode` 条件注入，业务代码只依赖端口，零改动。

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

- 契约：`api/proto/authz/v1/authz.proto`（`CheckPerm` / `GetUserPerms`），`make proto` 重新生成。
- 权限数据与缓存集中在系统服务侧管理；业务服务无需连接权限库。
- 模块抽离路径：`internal/modules/system` 整体搬出 → 独立 cmd 入口 → 即系统服务。

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
| 数据权限 | 领域纯函数合并 + 持久层 SQL 翻译 | `domain/service/data_scope.go`、`applyDataScope` |
| 事务 | 闭包式 `WithTx`，ctx 传播 tx，仓储 `GetDB(ctx)` 自动感知 | `pkg/kit/db/client.go` |
| 依赖注入 | Uber Fx，Module 聚合 + Group 插槽 | `kit.Module`、`system.Module` |
| 统一响应 | `{code, message, data}` + `AppError` 业务码（40100/40300…） | `internal/interface/http/response` |
| 错误处理 | `ErrorHandler` 中间件集中映射 AppError/fiber.Error/未知错误 | `internal/interface/http/middleware` |
| gRPC | KeepAlive + Recovery/Validator/Auth 拦截器链 + Fx Group 插槽 | `pkg/kit/rpc` |
| 读写分离 | Gorm dbresolver（replicas 配置即启用） | `pkg/kit/db` |
| JSON | Sonic 编解码 | `pkg/kit/web` |
| 服务间契约 | protobuf + gRPC（授权服务） | `api/proto/authz/v1` |

**新增依赖控制**：本次仅引入 `golang-jwt/jwt/v5` 与 `golang.org/x/crypto`；sonic 因 Go 1.26 兼容性升级至 v1.15.2。

---

## 6. API 一览（/api/v1）

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

用户列表查询已接入数据权限过滤，其他业务查询按 §3.3 的 `DataScopeHelper → Filter → applyDataScope` 三步接入。

---

## 7. 配置参考

| 配置项 | 说明 | 默认值 |
| :--- | :--- | :--- |
| `web.port` / `rpc.port` | HTTP / gRPC 端口 | `:8080` / `:9090` |
| `database.dsn` / `replicas` | 主库 / 从库连接串 | - |
| `database.auto_migrate` | 启动自动迁移 sys_* 表 | `false` |
| `jwt.secret` / `expire_minutes` | 签名密钥（生产必改）/ 有效期（分钟） | - / `120` |
| `authz.mode` / `addr` | 授权模式 local/remote / 系统服务地址 | `local` |
| `seed.enabled` | 首次启动播种内置数据 | `false` |

---

## 8. 测试与质量

- 单测：`MergeDataScope` 七个分支（超管/全部/并集/仅本人/叠加/无角色/空自定义）、`PermSet.Has`、JWT 签发/解析/过期/错签/空令牌。
- 校验命令：`go build ./...`、`go vet ./...`、`go test ./...`。
- 冒烟路径（需 MySQL）：登录拿 token → 无 token 401 → 无权限 403 → 建角色/分权限/分用户 → 数据权限过滤生效。

---

## 9. 扩展点（未内置）

| 扩展 | 接入方式 |
| :--- | :--- |
| Redis 权限缓存 | 替换 `pkg/kit/cache` 实现，`PermResolver` 不动 |
| 远程授权本地缓存 | 在 `RemoteAuthorizer` 外包短 TTL 缓存，降低 gRPC 调用频次 |
| 登录验证码 / 限流 | 新增全局中间件，`web.AsMiddlewares` 注入 |
| 操作日志 | Handler 旁路 + Fx Group 中间件 |
| 在线用户 / 踢出 | 用户表加 token_version，JWT 校验时比对 |

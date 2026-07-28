# GoKit - High Performance Go DDD Scaffolding

[![CI](https://github.com/tp-life/goKit/actions/workflows/ci.yml/badge.svg)](https://github.com/tp-life/goKit/actions/workflows/ci.yml)
![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8.svg)
![Fiber](https://img.shields.io/badge/fiber-v2.52-green)
![Gorm](https://img.shields.io/badge/gorm-v1.25-red)
![Fx](https://img.shields.io/badge/uber--fx-v1.20-blueviolet)
![License](https://img.shields.io/badge/license-MIT-blue)

**GoKit** 是一个基于 **Golang** 构建的现代化后台服务脚手架。它融合了 **领域驱动设计 (DDD)**、**整洁架构 (Clean Architecture)** 与 **依赖注入 (DI)** 的最佳实践，内置完整的 **RBAC 权限体系**，并支持 **单机 / 微服务双模式部署**。

核心目标：**让基础设施代码标准化，让业务逻辑纯粹化。**

> 📖 详细设计、架构图与技术点见 [docs/architecture.md](docs/architecture.md)。

---

## ✨ 核心特性

- **🏗 标准 DDD 分层**: Domain / Application / Infrastructure / Interface 四层，层内按业务聚合与用例分包。
- **🔐 完整 RBAC**: JWT 认证 + 用户/角色/菜单(权限点)/部门管理 + 接口级权限校验。
- **🛡 数据权限**: 基于部门的数据范围（全部 / 自定义 / 本部门及以下 / 本部门 / 仅本人），SQL 级过滤。
- **🚢 双模式部署**: 单机单二进制开箱即用；改一行配置 (`authz.mode=remote`) 即切换为微服务授权模式。
- **🧩 依赖注入**: 基于 **Uber Fx** 实现全自动组件装配与生命周期管理。
- **🚀 极致性能**: **Fiber v2** + **Sonic** (JSON) + **Gorm** (读写分离/预编译) + **gRPC** (KeepAlive)。
- **📝 可观测性**: 基于 **slog** 封装，自动注入 TraceID，支持 Text/JSON 切换。

---

## 🚀 快速开始 (Quick Start)

### 1. 环境准备
- **Go**: 1.21+
- **MySQL**: 5.7+（或直接使用 docker compose）

### 2. 初始化配置
```bash
cp configs/config.yaml.example configs/config.yaml
# 修改 database.dsn 与 jwt.secret
```

### 3. 启动服务

```bash
make tidy && make run
# 或 docker compose up -d --build
```

首次启动会自动迁移 `sys_*` 表并播种内置数据：

| 内置账号 | 密码 | 说明 |
| :--- | :--- | :--- |
| `admin` | `Admin@123` | 超级管理员，**请首次登录后立即修改** |

### 4. 接口测试

**登录获取 Token**
```bash
curl -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username": "admin", "password": "Admin@123"}'
```
*响应:* `{"code":0,"message":"success","data":{"token":"eyJhbGc..."}}`

**携带 Token 访问（数据权限自动过滤）**
```bash
curl http://localhost:8080/api/v1/users \
  -H "Authorization: Bearer <token>"
```

---

## 🔐 权限体系

### 模型

```text
用户(User) ──< 用户角色(UserRole) >── 角色(Role) ──< 角色菜单(RoleMenu) >── 菜单/权限点(Menu.perm_code)
                   │                     │
                   │                     └──< 角色部门(RoleDept) >── 部门(Dept)  ← 数据权限
                   └── 部门(Dept)
```

- **功能权限**：权限点挂在菜单树上（如 `system:user:create`），路由中间件逐点校验。
- **数据权限**：角色携带 `data_scope`，查询时合并用户所有角色的范围（取最宽），翻译成 SQL 条件：
  - `1` 全部数据 / `2` 自定义部门 / `3` 本部门及以下 / `4` 本部门 / `5` 仅本人
- **超级管理员** (`is_super=true`) 放行一切。

### API 一览（/api/v1）

| 模块 | 路由 | 权限点 |
| :--- | :--- | :--- |
| 认证 | `POST /auth/login` | 公开 |
| | `GET /auth/profile` / `PUT /auth/password` | 登录即可 |
| 用户 | `GET/POST /users`，`GET/PUT/DELETE /users/:id` | `system:user:*` |
| | `PUT /users/:id/roles`（分配角色） | `system:user:assign-role` |
| | `PUT /users/:id/password`（重置密码） | `system:user:reset-pwd` |
| 角色 | `GET/POST /roles`，`GET/PUT/DELETE /roles/:id` | `system:role:*` |
| | `PUT /roles/:id/menus`（分配权限） | `system:role:assign-menu` |
| | `PUT /roles/:id/users`、`GET /roles/:id/users`（分配用户） | `system:role:assign-user` |
| 部门 | `GET /depts/tree`，`POST/PUT/DELETE /depts...` | `system:dept:*` |
| 菜单 | `GET /menus/tree`，`POST/PUT/DELETE /menus...` | `system:menu:*` |

### 给新接口加权限校验

```go
// 路由注册时标注权限点即可
secured.Get("/orders", middleware.RequirePerm(az, "order:list"), h.List)

// 查询需要数据权限过滤时，repository 层应用 scope：
filter, _ := s.scope.Build(ctx)          // 解析当前用户数据范围
repo.List(ctx, filter, page, pageSize)   // 内部翻译成 SQL WHERE 条件
```

---

## 🚢 两种部署模式

### 模式一：单机部署（默认）

单二进制 + MySQL，`authz.mode=local`，权限校验在进程内完成（查库 + 内存缓存）：

```bash
docker compose up -d --build   # 应用 + MySQL 一键启动
```

### 模式二：微服务部署

将 system 业务（`internal/domain`、`internal/application` 中的 user/role/dept/menu/auth 等包及配套基础设施与接口层）抽离为独立的**系统服务**（认证 + 授权中心），业务服务通过 gRPC 远程校验权限：

```yaml
# 业务服务配置
authz:
  mode: "remote"
  addr: "system-service:9090"
jwt:
  secret: "<与系统服务共享同一 secret>"   # JWT 在业务服务本地验签
```

工作原理：

1. 系统服务通过 gRPC 暴露 `AuthzService`（`api/proto/authz/v1/authz.proto`：`CheckPerm` / `GetUserPerms`）。
2. 业务服务注入 `auth.RemoteAuthorizer`（实现同一个 `auth.Authorizer` 端口），业务代码零改动。
3. JWT 为无状态验签，各服务共享 secret 即可；权限点数据集中在系统服务侧缓存管理。

重新生成 protobuf 代码：`make proto`（需安装 protoc / protoc-gen-go / protoc-gen-go-grpc）。

---

## 📂 目录结构

```text
GoKit/
├── cmd/server/main.go           # 程序入口 (Fx 组装，含 local/remote 授权器选择)
├── api/
│   ├── proto/authz/v1/          # 授权服务 proto 定义
│   └── gen/authz/v1/            # 生成的 pb 代码 (make proto)
├── configs/                     # 配置文件
├── internal/
│   ├── app/                     # system 业务的 Fx 装配 (var Module)
│   ├── domain/                  # [领域层] 按聚合分包：实体、仓储接口、数据权限纯逻辑
│   ├── application/             # [应用层] 按用例分包：Service、DTO、授权器(本地实现)
│   ├── infrastructure/          # [基础设施层] Gorm 实现、迁移、播种
│   └── interface/               # [接口层] HTTP Handler/中间件/路由聚合、gRPC 授权服务
├── pkg/kit/                     # 🧱 通用底座 (DB, RPC, Web, Log, Auth, Cache)
├── Dockerfile
└── docker-compose.yaml          # 单机一键部署
```

---

## ⚙️ 配置说明

| 模块 | 配置项 | 说明 | 默认值 |
| :--- | :--- | :--- | :--- |
| **Web** | `web.port` | HTTP 端口 | `:8080` |
| **RPC** | `rpc.port` | gRPC 端口 | `:9090` |
| **DB** | `database.dsn` | 主库连接串 | - |
| | `database.replicas` | 从库连接串列表 | `[]` |
| | `database.auto_migrate` | 启动自动迁移表结构 | `false` |
| **JWT** | `jwt.secret` | 签名密钥（生产必改，微服务间共享） | - |
| | `jwt.expire_minutes` | 令牌有效期（分钟） | `120` |
| **Authz** | `authz.mode` | 授权模式：`local` / `remote` | `local` |
| | `authz.addr` | remote 模式系统服务 gRPC 地址 | - |
| **Seed** | `seed.enabled` | 首次启动播种内置数据 | `false` |

---

## 🛣 后续扩展点（未内置）

- Redis 权限缓存（替换 `pkg/kit/cache` 内存实现即可）
- 登录验证码 / 限流、操作日志、在线用户管理

## 📄 License

MIT © 2024 GoKit Team

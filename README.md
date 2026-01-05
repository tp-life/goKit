# Nexus - High Performance Go DDD Scaffolding

![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8.svg)
![Fiber](https://img.shields.io/badge/fiber-v2.52-green)
![Gorm](https://img.shields.io/badge/gorm-v1.25-red)
![Fx](https://img.shields.io/badge/uber--fx-v1.20-blueviolet)
![License](https://img.shields.io/badge/license-MIT-blue)

**Nexus** 是一个基于 **Golang 1.21+** 构建的现代化微服务脚手架。它融合了 **领域驱动设计 (DDD)**、**整洁架构 (Clean Architecture)** 与 **依赖注入 (DI)** 的最佳实践。

核心目标：**让基础设施代码标准化，让业务逻辑纯粹化。**

---

## ✨ 核心特性

- **🏗 标准 DDD 分层**: 严格隔离 Domain / Application / Infrastructure / Interface。
- **🧩 依赖注入**: 基于 **Uber Fx** 实现全自动组件装配与生命周期管理。
- **🚀 极致性能**: **Fiber v2** + **Sonic** (JSON) + **Gorm** (读写分离/预编译) + **gRPC** (KeepAlive)。
- **🛡 健壮性**: 闭包式事务管理 (`WithTx`)，支持 Context 自动传播。
- **📝 可观测性**: 基于 **slog** 封装，自动注入 TraceID，支持 Text/JSON 切换。
- **🔌 插件化**: 为 HTTP/gRPC 预留了基于 Fx Group 的中间件插槽。

---

## 🚀 快速开始 (Quick Start)

### 1. 环境准备
确保本地已安装：
- **Go**: 1.21+
- **MySQL**: 5.7+
- **Make** (可选，推荐)

### 2. 初始化配置
项目默认读取 `configs/local.yaml`。请根据实际情况修改数据库连接：

```yaml
# configs/local.yaml
database:
  driver: "mysql"
  # 修改为你的账号密码和数据库名
  dsn: "root:root@tcp(127.0.0.1:3306)/nexus_db?charset=utf8mb4&parseTime=True&loc=Local"
```

### 3. 启动服务

**方式 A: 使用 Makefile (推荐)**
```bash
# 下载依赖
make tidy

# 运行服务
make run
```

**方式 B: 使用 Go 命令**
```bash
go mod tidy
go run cmd/server/main.go
```

启动成功后，你将看到以下日志：
```text
INFO http_server_start addr=:8080
INFO grpc_server_start addr=:9090
```

### 4. 接口测试

项目内置了用户 (User) 模块的 CRUD 示例。

**创建用户 (HTTP)**
```bash
curl -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{"name": "Nexus Developer", "email": "dev@nexus.com"}'
```
*响应:* `{"id": 1}`

**查询用户 (HTTP)**
```bash
curl http://localhost:8080/api/v1/users/1
```
*响应:* `{"id": 1, "name": "Nexus Developer", "email": "dev@nexus.com"}`

---

## 📂 目录结构

```text
nexus/
├── cmd/server/main.go           # 程序入口 (Fx 组装)
├── configs/                     # 配置文件
├── internal/                    # 🔒 业务代码
│   ├── application/             # [应用层] Service, DTO, 事务编排
│   ├── domain/                  # [领域层] Entity, Repository 接口 (无依赖)
│   ├── infrastructure/          # [基础设施层] Repository 实现 (Gorm)
│   └── interface/               # [接入层] HTTP/gRPC Handler
├── pkg/kit/                     # 🧱 通用底座 (DB, RPC, Web, Log)
└── Makefile                     # 开发命令
```

---

## 🛠 开发指南

### 如何开发一个新的 API？

遵循 DDD 原则，请按以下步骤操作：

1.  **Domain**: 在 `internal/domain/entity` 定义实体，在 `repository` 定义接口。
2.  **Infrastructure**: 在 `internal/infrastructure/persistence` 实现接口。
    > *Tip: 使用 `r.client.GetDB(ctx)` 获取数据库连接，它会自动处理事务。*
3.  **Application**: 在 `internal/application/service` 编写业务逻辑。
    > *Tip: 使用 `s.tx.WithTx(ctx, func...)` 包裹事务逻辑。*
4.  **Interface**: 在 `internal/interface/http` 编写 Handler 并绑定 DTO。
5.  **Main**: 在 `cmd/server/main.go` 中注册 (Provide) 你的组件。

### 事务使用示例

```go
func (s *UserService) Create(ctx context.Context, req dto.CreateReq) error {
    // 自动开启事务，出错自动回滚，成功自动提交
    return s.tx.WithTx(ctx, func(ctx context.Context) error {
        user := req.ToEntity()
        if err := s.repo.Create(ctx, user); err != nil {
            return err
        }
        // ... 其他业务逻辑
        return nil
    })
}
```

### 注入中间件

无需修改底层代码，在 `main.go` 中注入即可生效：

```go
// 注入 HTTP 中间件
fx.Provide(AsMiddleware(func() fiber.Handler {
    return cors.New()
})),

// 注入 gRPC 拦截器
fx.Provide(AsUnaryInterceptor(func(l *slog.Logger) grpc.UnaryServerInterceptor {
    return myInterceptor(l)
})),
```

---

## ⚙️ 配置说明

| 模块 | 配置项 | 说明 | 默认值 |
| :--- | :--- | :--- | :--- |
| **Web** | `web.port` | HTTP 端口 | `:8080` |
| | `web.prefork` | 多进程模式 (Linux) | `false` |
| **RPC** | `rpc.port` | gRPC 端口 | `:9090` |
| **DB** | `database.dsn` | 主库连接串 | - |
| | `database.replicas` | 从库连接串列表 | `[]` |
| **Log** | `log.level` | 日志级别 (debug/info) | `info` |

---

## 🐳 Docker 构建

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o server cmd/server/main.go

FROM alpine:latest
WORKDIR /app
COPY --from=builder /app/server .
COPY configs/ ./configs/
CMD ["./server"]
```

## 📄 License

MIT © 2024 Nexus Team

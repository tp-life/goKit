# Notion-Lite Backend (基于 GoKit)

![Go Version](https://img.shields.io/badge/go-1.21+-00ADD8.svg)
![Fiber](https://img.shields.io/badge/fiber-v2.52-green)
![Gorm](https://img.shields.io/badge/gorm-v1.25-red)
![Fx](https://img.shields.io/badge/uber--fx-v1.20-blueviolet)
![License](https://img.shields.io/badge/license-MIT-blue)

**Notion-Lite** 是一个轻量级的笔记应用后端，基于 **GoKit** 脚手架构建。它实现了移动端极速录入、PC端块编辑、统一时间轴等核心功能。

**核心理念:** "移动端快写 (Memo) + PC端慢读 (Block) + 统一时间轴 (Timeline)"

---

## ✨ 核心特性

- **📱 移动端极速录入**: 1秒内完成闪念记录，支持图片上传
- **💻 PC端块编辑**: 基于 Editor.js 的结构化编辑，所见即所得
- **📊 统一时间轴**: 聚合 Memos 和 Pages，按时间倒序展示
- **🔐 双Token鉴权**: JWT Access Token + Refresh Token，支持持久登录
- **☁️ 七牛云存储**: 图片自动上传到七牛云，返回 CDN URL
- **🔗 公开分享**: 支持页面分享，生成唯一 ShareID，访客可只读访问

## 🏗️ 技术架构

- **标准 DDD 分层**: 严格隔离 Domain / Application / Infrastructure / Interface
- **依赖注入**: 基于 **Uber Fx** 实现全自动组件装配与生命周期管理
- **极致性能**: **Fiber v2** + **Sonic** (JSON) + **Gorm** (事务/预编译)
- **健壮性**: 闭包式事务管理 (`WithTx`)，支持 Context 自动传播
- **可观测性**: 基于 **slog** 封装，支持 JSON 日志输出

---

## 🚀 快速开始

### 1. 环境准备
确保本地已安装：
- **Go**: 1.21+
- **MySQL**: 8.0+ (必须支持 JSON 字段)
- **七牛云账号**: 用于对象存储

### 2. 安装依赖
```bash
go mod tidy
```

### 3. 初始化数据库
```bash
mysql -u root -p < docs/migrations/001_initial_schema.sql
```

### 4. 配置应用
```bash
cp configs/config.yaml.example configs/config.yaml
```

编辑 `configs/config.yaml`，填入：
- 数据库连接信息
- 七牛云 AccessKey、SecretKey、Bucket、Domain
- JWT 密钥（至少32字符）

### 5. 启动服务
```bash
go run cmd/server/main.go
```

服务将在 `http://localhost:8080` 启动。

详细部署说明请参考：[部署指南](docs/NOTION_LITE_SETUP.md)

### 4. 接口测试

项目内置了用户 (User) 模块的 CRUD 示例。

**创建用户 (HTTP)**
```bash
curl -X POST http://localhost:8080/api/v1/users \
  -H "Content-Type: application/json" \
  -d '{"name": "GoKit Developer", "email": "dev@GoKit.com"}'
```
*响应:* `{"id": 1}`

**查询用户 (HTTP)**
```bash
curl http://localhost:8080/api/v1/users/1
```
*响应:* `{"id": 1, "name": "GoKit Developer", "email": "dev@GoKit.com"}`

---

## 📂 目录结构

```text
GoKit/
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

MIT © 2024 GoKit Team

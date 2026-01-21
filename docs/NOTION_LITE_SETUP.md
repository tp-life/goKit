# Notion-Lite 后端部署指南

本文档说明如何部署和运行 Notion-Lite 后端服务。

## 📋 前置要求

- **Go**: 1.21+
- **MySQL**: 8.0+ (必须支持 JSON 字段)
- **七牛云账号**: 用于对象存储

## 🚀 快速开始

### 1. 安装依赖

```bash
go mod tidy
```

### 2. 配置数据库

创建数据库并执行迁移脚本：

```bash
mysql -u root -p < docs/migrations/001_initial_schema.sql
```

### 3. 配置应用

复制配置文件模板并修改：

```bash
cp configs/config.yaml.example configs/config.yaml
```

编辑 `configs/config.yaml`，填入以下配置：

- **数据库连接**: 修改 `database.dsn`
- **七牛云配置**: 填入 `storage` 部分的 AccessKey、SecretKey、Bucket 和 Domain
- **JWT 密钥**: 设置 `auth.jwt_secret`（至少32字符）

### 4. 运行服务

```bash
go run cmd/server/main.go
```

或使用 Makefile：

```bash
make run
```

服务将在 `http://localhost:8080` 启动。

## 📡 API 接口

### 认证接口

- `POST /auth/register` - 用户注册
- `POST /auth/login` - 用户登录（返回 Access Token 和 Refresh Token）
- `POST /auth/refresh` - 刷新 Access Token

### 上传接口

- `POST /api/v1/upload` - 图片上传（兼容 Editor.js 格式）

### Memo 接口

- `POST /api/v1/memos` - 创建闪念

### Page 接口

- `POST /api/v1/pages` - 创建/更新页面
- `GET /api/v1/pages/:id` - 获取页面详情（支持混合模式：Owner/Guest）
- `POST /api/v1/pages/:id/share` - 开启/关闭分享
- `GET /api/v1/public/pages/:share_id` - 通过 ShareID 获取公开页面

### Timeline 接口

- `GET /api/v1/timeline?limit=20&offset=0` - 获取统一时间轴（聚合 Memos 和 Pages）

## 🔐 鉴权说明

### 双 Token 机制

- **Access Token**: JWT 格式，有效期 15 分钟，前端内存存储
- **Refresh Token**: Opaque 字符串，有效期 30 天，存储在数据库

### 使用方式

1. 登录后获得双 Token
2. 请求时在 Header 中携带：`Authorization: Bearer <access_token>`
3. Access Token 过期后，使用 Refresh Token 调用 `/auth/refresh` 获取新的 Access Token

### 权限模式

- **私有模式**: 需要有效的 JWT Token，只能访问自己的资源
- **混合模式** (Page): 
  - 有 JWT 且为 Owner -> 允许所有操作
  - 无 JWT 但 `is_shared=1` 且 Method=GET -> 允许只读 (Guest Mode)
  - 其他 -> 403 Forbidden

## 🗄️ 数据库结构

详见 `docs/migrations/001_initial_schema.sql`

核心表：
- `users` - 用户表
- `user_sessions` - 会话表（Refresh Token）
- `memos` - 闪念表
- `pages` - 页面表
- `blocks` - 块数据表（Editor.js 格式）

## 🔧 开发说明

### 项目结构

```
internal/
├── domain/              # 领域层
│   ├── entity/          # 实体定义
│   └── repository/       # Repository 接口
├── infrastructure/      # 基础设施层
│   ├── persistence/     # GORM 实现
│   └── storage/         # 七牛云存储
├── application/         # 应用层
│   └── service/         # 业务逻辑
└── interface/           # 接口层
    └── http/            # HTTP Handlers
```

### 依赖注入

使用 Uber Fx 进行依赖注入，所有组件在 `cmd/server/main.go` 中注册。

## ⚠️ 注意事项

1. **ID 生成**: 当前使用时间戳生成 ID，生产环境建议使用雪花算法
2. **密码加密**: 使用 Bcrypt，无需额外 Salt
3. **图片上传**: 前端需先压缩图片至 500KB 以内
4. **Editor.js 格式**: 上传接口必须返回 `{"success": 1, "file": {"url": "..."}}` 格式

## 📝 待优化项

### 🔴 高优先级（影响核心功能）

#### 1. 实现真正的雪花 ID 生成器
**当前状态**: 使用时间戳生成 ID，存在并发冲突风险  
**影响**: 高并发场景下可能产生 ID 冲突  
**实现方案**:
- 使用 `github.com/bwmarrin/snowflake` 或自实现雪花算法
- 配置机器 ID 和数据中心 ID（可通过环境变量或配置中心获取）
- 替换 `MemoService` 和 `PageService` 中的 ID 生成逻辑

**参考代码**:
```go
// pkg/kit/id/snowflake.go
type IDGenerator interface {
    NextID() uint64
}
```

#### 2. 实现游标分页（Cursor-based Pagination）
**当前状态**: 使用 Offset 分页，大数据量下性能差  
**影响**: Timeline 查询在数据量大时性能下降  
**实现方案**:
- 使用 `created_at` + `id` 作为游标
- 修改 `GetTimelineRequest` 支持 `cursor` 参数
- 优化 Repository 查询，使用 `WHERE created_at < ? AND id < ?` 条件

**API 变更**:
```go
type GetTimelineRequest struct {
    Limit  int    `json:"limit"`
    Cursor string `json:"cursor"` // base64 编码的游标: {created_at, id}
}
```

#### 3. 添加图片压缩中间件
**当前状态**: 依赖前端压缩，无法保证一致性  
**影响**: 存储成本高，加载速度慢  
**实现方案**:
- 使用 `github.com/nfnt/resize` 或 `github.com/disintegration/imaging`
- 在 `UploadHandler` 中自动压缩图片
- 支持配置压缩质量、最大尺寸等参数

**配置示例**:
```yaml
storage:
  image_compress:
    enabled: true
    max_width: 1920
    max_height: 1920
    quality: 85
    max_size_kb: 500
```

### 🟡 中优先级（提升性能和体验）

#### 4. 添加 Redis 缓存层
**当前状态**: 所有查询直接访问数据库  
**影响**: 高并发下数据库压力大，响应慢  
**实现方案**:
- 缓存热点数据：用户信息、公开页面、Timeline（短期缓存）
- 使用 `github.com/redis/go-redis/v9`
- 实现缓存策略：Cache-Aside 模式
- 设置合理的 TTL（如 Timeline 缓存 5 分钟）

**缓存场景**:
- 用户信息（TTL: 1 小时）
- 公开页面（TTL: 30 分钟）
- Timeline 列表（TTL: 5 分钟）
- Page 详情（TTL: 10 分钟）

#### 5. 实现请求限流（Rate Limiting）
**当前状态**: 无限流保护  
**影响**: 可能被恶意请求攻击  
**实现方案**:
- 使用 `golang.org/x/time/rate` 或 Redis 实现分布式限流
- 按用户 ID 或 IP 限流
- 配置不同接口的限流策略（如登录接口更严格）

**配置示例**:
```yaml
rate_limit:
  enabled: true
  default_rps: 100  # 每秒请求数
  login_rps: 5      # 登录接口更严格
```

#### 6. 优化 Timeline 查询性能
**当前状态**: 每次查询都合并两个表的数据  
**影响**: 数据量大时内存占用高，查询慢  
**实现方案**:
- 使用数据库 UNION 查询替代内存合并
- 添加 `created_at` 索引优化
- 考虑使用物化视图或定时任务预聚合

**SQL 优化**:
```sql
SELECT * FROM (
  SELECT id, 'memo' as type, content, NULL as title, created_at 
  FROM memos WHERE user_id = ?
  UNION ALL
  SELECT id, 'page' as type, NULL, title, created_at 
  FROM pages WHERE user_id = ?
) AS timeline 
ORDER BY created_at DESC 
LIMIT ? OFFSET ?;
```

#### 7. 添加全文搜索功能
**当前状态**: 无法搜索 Memo 和 Page 内容  
**影响**: 用户体验差，无法快速找到内容  
**实现方案**:
- 使用 MySQL Full-Text Search 或集成 Elasticsearch
- 实现搜索接口：`GET /api/v1/search?q=keyword&type=memo|page`
- 支持高亮、分词等功能

### 🟢 低优先级（增强功能）

#### 8. 实现 Block 增量更新
**当前状态**: Page 更新时全量删除并重建 Blocks  
**影响**: 更新操作慢，可能丢失并发编辑  
**实现方案**:
- 实现 Block 的 `version` 字段（乐观锁）
- 对比新旧 Blocks，只更新变更的部分
- 支持冲突检测和合并策略

#### 9. 添加操作日志（Audit Log）
**当前状态**: 无操作记录  
**影响**: 无法追溯用户操作，难以排查问题  
**实现方案**:
- 创建 `audit_logs` 表记录关键操作
- 记录：创建/更新/删除 Memo/Page、分享操作等
- 支持按时间、用户、操作类型查询

#### 10. 实现 WebSocket 实时同步
**当前状态**: 无实时功能  
**影响**: 多端编辑无法实时同步  
**实现方案**:
- 使用 `github.com/gofiber/websocket` 实现 WebSocket
- 支持 Page 编辑的实时协作（可选功能）
- 实现冲突解决机制

#### 11. 添加数据导出功能
**当前状态**: 无法导出数据  
**影响**: 用户数据迁移困难  
**实现方案**:
- 实现导出接口：`GET /api/v1/export?format=json|markdown`
- 支持导出所有 Memos 和 Pages
- 生成可导入的格式（Markdown、JSON）

#### 12. 实现标签（Tags）系统
**当前状态**: 无分类功能  
**影响**: 内容组织困难  
**实现方案**:
- 创建 `tags` 表和 `memo_tags`、`page_tags` 关联表
- 支持为 Memo 和 Page 添加标签
- 实现按标签筛选 Timeline

#### 13. 添加统计和分析功能
**当前状态**: 无数据统计  
**影响**: 无法了解使用情况  
**实现方案**:
- 实现统计接口：`GET /api/v1/stats`
- 统计：Memo/Page 数量、创建趋势、最活跃时段等
- 支持图表数据导出

#### 14. 优化错误处理和日志
**当前状态**: 错误信息不够详细  
**影响**: 问题排查困难  
**实现方案**:
- 统一错误码和错误消息格式
- 添加请求追踪 ID（Request ID）
- 实现结构化日志，支持日志级别过滤
- 集成 Sentry 或类似错误监控服务

#### 15. 添加单元测试和集成测试
**当前状态**: 无测试覆盖  
**影响**: 代码质量无法保证，重构风险高  
**实现方案**:
- 使用 `testing` 包编写单元测试
- 使用 `testify` 简化测试代码
- 实现 Repository、Service 层的测试
- 添加 API 集成测试

**测试覆盖率目标**: > 70%

### ✅ 已完成

- [x] 实现混合模式权限中间件（`OptionalAuthMiddleware`）
- [x] Timeline 聚合查询使用 Goroutine 并发 + Merge Sort
- [x] 实现 Page 分享接口

---

## 🎯 优化路线图

### Phase 1: 稳定性优化（1-2 周）
1. 雪花 ID 生成器
2. 游标分页
3. 错误处理和日志优化

### Phase 2: 性能优化（2-3 周）
4. Redis 缓存层
5. Timeline 查询优化
6. 请求限流

### Phase 3: 功能增强（3-4 周）
7. 全文搜索
8. Block 增量更新
9. 标签系统

### Phase 4: 高级功能（可选）
10. WebSocket 实时同步
11. 数据导出
12. 统计和分析

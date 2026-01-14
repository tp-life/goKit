# 架构分析与可复用工具设计

## 一、可复用工具分析

### 1.1 WebSocket 客户端工具
**位置**: `pkg/kit/websocket/`

**功能**:
- 统一的 WebSocket 连接管理
- 自动重连机制（指数退避）
- 消息订阅/取消订阅
- 连接健康检查
- 支持代理配置

**复用场景**:
- Binance WebSocket 连接
- Lighter WebSocket 连接
- Hyperliquid WebSocket 连接
- 后续可能的其他交易所

### 1.2 HTTP 客户端工具
**位置**: `pkg/kit/http/`

**功能**:
- 统一的 HTTP 客户端封装
- 请求重试机制
- 速率限制（Rate Limiting）
- 代理支持
- 超时控制
- 错误处理

**复用场景**:
- 所有交易所的 REST API 调用
- 资金费率查询
- 价格数据获取（备用）

### 1.3 重连管理器
**位置**: `pkg/kit/reconnect/`

**功能**:
- 指数退避算法
- 最大重试次数控制
- 重连状态回调
- 连接健康监控

**复用场景**:
- WebSocket 重连
- HTTP 客户端重连（可选）

### 1.4 速率限制器
**位置**: `pkg/kit/ratelimit/`

**功能**:
- Token Bucket 算法
- 滑动窗口算法
- 多级限流（全局/交易所级别）
- 限流状态查询

**复用场景**:
- 交易所 API 调用限流
- WebSocket 订阅限流

### 1.5 数据验证器
**位置**: `pkg/kit/validator/`

**功能**:
- 价格合理性验证
- 数据时效性检查
- 数据一致性验证
- 异常值检测

**复用场景**:
- 市场数据验证
- 资金费率验证
- 订单数据验证

### 1.6 配置加载器
**位置**: `pkg/kit/config/` (扩展现有)

**功能**:
- 多格式配置支持（YAML/JSON）
- 环境变量覆盖
- 配置验证
- 配置热重载（可选）

**复用场景**:
- 应用配置
- 交易所配置
- 套利阈值配置

### 1.7 缓存工具
**位置**: `pkg/kit/cache/`

**功能**:
- 内存缓存（TTL 支持）
- 缓存失效策略
- 缓存统计

**复用场景**:
- 市场数据缓存
- 资金费率缓存
- 配置缓存

### 1.8 错误处理工具
**位置**: `pkg/kit/errors/`

**功能**:
- 统一错误码
- 错误包装和上下文
- 错误分类（网络错误、业务错误等）
- 错误恢复策略

**复用场景**:
- 所有层的错误处理

### 1.9 时间工具
**位置**: `pkg/kit/timeutil/`

**功能**:
- 时间格式化
- 时区转换
- 时间窗口计算
- 定时任务工具

**复用场景**:
- 数据时间戳处理
- 定时任务调度

### 1.10 数学计算工具
**位置**: `pkg/kit/math/`

**功能**:
- 精度计算（decimal）
- 百分比计算
- 价格差异计算
- 收益率计算

**复用场景**:
- 套利计算
- 费率差异计算
- 盈亏计算

## 二、技术方案确认

### 2.1 Web UI Embed 方案
**方案**: Vue3 + Tailwind CSS + Go embed

**实现步骤**:
1. 前端项目独立开发（`web/` 目录）
2. 使用 Vite 构建，输出到 `web/dist/`
3. Go 使用 `embed.FS` 嵌入 `web/dist/`
4. Fiber 静态文件服务

**优点**:
- 前后端分离开发
- 生产环境单二进制部署
- 支持热重载开发

### 2.2 实时通信方案
**选择**: WebSocket

**原因**:
1. 后续下单功能需要双向通信
2. 低延迟要求
3. 支持二进制数据（可选）
4. 更好的错误处理和重连控制

**实现**:
- 服务端: Fiber WebSocket 支持
- 客户端: 统一 WebSocket 客户端工具

### 2.3 代理支持
**位置**: `pkg/kit/proxy/`

**功能**:
- HTTP 代理配置
- WebSocket 代理支持
- 代理健康检查
- 代理轮换（可选）

## 三、公共工具层设计

### 3.1 目录结构
```
pkg/kit/
├── websocket/      # WebSocket 客户端
│   ├── client.go
│   ├── config.go
│   └── reconnect.go
├── http/           # HTTP 客户端
│   ├── client.go
│   ├── retry.go
│   └── proxy.go
├── reconnect/      # 重连管理
│   ├── manager.go
│   └── strategy.go
├── ratelimit/      # 速率限制
│   ├── limiter.go
│   └── tokenbucket.go
├── validator/      # 数据验证
│   ├── market_data.go
│   └── price.go
├── cache/          # 缓存
│   ├── memory.go
│   └── ttl.go
├── errors/         # 错误处理
│   ├── codes.go
│   └── wrapper.go
├── timeutil/       # 时间工具
│   └── util.go
├── math/           # 数学计算
│   ├── decimal.go
│   └── percentage.go
└── proxy/          # 代理支持
    ├── config.go
    └── dialer.go
```

### 3.2 接口设计原则
1. **依赖注入友好**: 所有工具都支持通过构造函数注入依赖
2. **Context 支持**: 所有异步操作都支持 Context 取消
3. **可测试性**: 接口抽象，便于 mock 测试
4. **可观测性**: 集成日志和指标（可选）

## 四、交易所客户端抽象

### 4.1 统一接口
```go
type ExchangeClient interface {
    // 市场数据
    GetMarketData(ctx context.Context, symbol string) (*MarketData, error)
    SubscribeMarketData(ctx context.Context, symbols []string) (<-chan *MarketData, error)
    
    // 资金费率
    GetFundingRate(ctx context.Context, symbol string) (*FundingRate, error)
    
    // 交易（预留）
    PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error)
    GetBalance(ctx context.Context, asset string) (*Balance, error)
    
    // 连接管理
    Connect(ctx context.Context) error
    Close() error
    IsConnected() bool
}
```

### 4.2 工厂模式
```go
type ExchangeFactory struct {
    clients map[string]ExchangeClient
}

func (f *ExchangeFactory) GetClient(exchange string) (ExchangeClient, error) {
    // 返回对应交易所的客户端
}
```

## 五、实施优先级

### Phase 1: 基础设施
1. ✅ WebSocket 客户端工具
2. ✅ HTTP 客户端工具
3. ✅ 重连管理器
4. ✅ 速率限制器
5. ✅ 代理支持

### Phase 2: 数据层
1. ✅ SQLite 支持
2. ✅ Domain 实体
3. ✅ Repository 接口

### Phase 3: 业务层
1. ✅ 交易所客户端实现
2. ✅ 数据收集服务
3. ✅ 对比服务

### Phase 4: 接口层
1. ✅ HTTP API
2. ✅ WebSocket 推送
3. ✅ Web UI

### Phase 5: 高级功能
1. ⏳ 模拟交易
2. ⏳ 风险控制
3. ⏳ 监控告警

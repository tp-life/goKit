# Funding Arbitrage Monitor (Binance + Aster + Hyperliquid)

基于 `goKit` 模板改造的多交易所资金费率套利监控 / 计划 / 执行原型。

当前版本的目标不是只监控 Binance + Aster，而是把整个仓库改造成可以继续接入更多交易所的结构：

- 市场侧抽象：`MarketAdapter`
- 交易侧抽象：`TradeAdapter`
- 规范化交易对：`canonical symbol`
- 执行记录 / 订单记录落库
- 手动与自动开平仓接口

## 当前已经包含

- SQLite 持久化
- Binance / Aster / Hyperliquid 公共行情接入
- 多交易所共同交易对自动发现
- 规范化交易对映射（如 `BTCUSDT -> BTC`，`BTC -> BTC`）
- 任意两两交易所组合的 funding 套利机会计算
- 执行计划生成（按最小下单量 / 最小名义价值 / 入场窗口）
- 真实执行层抽象：
  - Binance 下单 / 平仓 / 查持仓
  - Aster 下单 / 平仓 / 查持仓
  - Hyperliquid 下单 / 平仓 / 查持仓
- 执行记录表 / 订单记录表
- 自动开仓 / 自动平仓循环
- 手动 HTTP 接口：开仓 / 平仓 / 查执行记录 / 查订单记录
- 前端页面改为动态展示多交易所数据

## 当前仍然不包含

- 私有用户流 / 订单状态 websocket 回补
- 完整的撤单 / 改单 / 重试编排
- 双腿成交一致性校验与对冲补单
- 更细的风控（最大持仓、单交易所熔断、账户权益检查、黑名单 symbol 等）
- 完整的别名映射表后台维护页面

所以这版已经不是纯 scanner，但也还不是完全体生产 bot。

## 目录重点

- `internal/infrastructure/exchange`
  - `interfaces.go`：统一市场/交易接口
  - `cex_market.go`：Binance/Aster 公共行情适配器
  - `cex_trade.go`：Binance/Aster 下单适配器
  - `hyperliquid_market.go`：Hyperliquid 行情适配器
  - `hyperliquid_trade.go`：Hyperliquid 交易适配器
- `internal/application/service`
  - `strategy_runner.go`：多交易所机会计算
  - `planning.go`：生成执行计划
  - `execution_service.go`：自动/手动执行开平仓
- `internal/domain/entity`
  - `symbol.go`
  - `opportunity.go`
  - `execution_plan.go`
  - `execution_record.go`
  - `order_record.go`

## 设计文档

- `docs/multi_exchange_architecture.md`：多交易所架构设计，解释 Market/Trade adapter、canonical symbol、venue profile、execution 边界与后续演进原则。
- `docs/unified_exchange_and_arbitrage_architecture_v1.md`：统一交易所接入与统一套利计算架构设计文档（v1），更聚焦“统一接入 + 统一套利计算主链”的专项设计。
- `docs/arbitrage_algorithm_and_funding_timeline.md`：套利算法、funding 时间轴、机会计算与自动执行时序说明。
- `docs/task_breakdown_v1.md`：实施任务拆解（Task Breakdown v1），记录工作包、验收标准、当前完成度与后续优先级。
- `docs/code_design_v1_scanner_to_planner.md`：代码设计文档 v1，记录从 scanner 到 planner 的第一阶段设计。
- `docs/code_design_v2_execution_safety.md`：代码设计文档 v2，记录执行安全、回滚与 auto-close safety 的设计。
- `docs/code_design_v3_registry_and_state_machine.md`：代码设计文档 v3，记录 registry 化与双腿状态机的后续演进方向。

## API

默认监听 `:8080`：

- `GET /api/v1/health`
- `GET /api/v1/symbols`
- `GET /api/v1/opportunities?limit=200`
- `GET /api/v1/plans?limit=100`
- `GET /api/v1/executions?limit=50`
- `GET /api/v1/executions/:planKey/orders`
- `POST /api/v1/executions/:planKey/open`
- `POST /api/v1/executions/:planKey/close`
- `GET /api/v1/market/:symbol`
- `GET /api/v1/system/status`
- `GET /api/v1/snapshot-stats`

## 配置

配置文件见 `configs/config.yaml.example`。

重点参数：

- `strategy.allowed_symbols`：规范化 symbol 列表，建议写基础币 `BTC/ETH/SOL`
- `strategy.entry_mode`：`maker / mixed / taker`
- `strategy.exit_mode`：`maker / mixed / taker`
- `strategy.execution.enabled`：是否允许真实下单；`false` 时只会生成 dry-run 执行记录
- `strategy.execution.auto_entry`：是否自动开仓
- `strategy.execution.auto_close`：是否自动平仓
- `strategy.execution.close_grace_period`：最后一腿 funding 结算后等待多久再平仓
- `exchanges.*.auth.*`：各交易所密钥环境变量名

## 数据表

主要表：

- `symbols`
- `funding_snapshots`
- `book_top_snapshots`
- `opportunities`
- `execution_plans`
- `execution_records`
- `order_records`
- `strategy_runs`

## 运行

```bash
go mod tidy
go run ./cmd/server
```

SQLite 默认文件：

```text
data/arbitrage.db
```

## 当前环境说明

当前容器环境无法联网拉取 Go 模块，因此这里没法完成最终 `go build` 验证。

你需要在本地联网环境执行：

```bash
go mod tidy
go build ./...
```

如果你继续往下做，下一步最值得补的是：

1. 订单状态 websocket 与成交回补
2. 双腿执行的一致性状态机
3. 风控与余额/权益校验
4. 更完整的交易对 alias 管理

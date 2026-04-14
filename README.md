# Funding Arbitrage Monitor (Binance + Aster + Hyperliquid)

基于 `goKit` 模板改造的多交易所资金费率套利监控 / 计划 / 执行原型。

当前版本的目标不是只监控 Binance + Aster，而是把整个仓库改造成可以继续接入更多交易所的结构：

- 市场侧抽象：`MarketAdapter`
- 交易侧抽象：`TradeAdapter`
- 规范化交易对：`canonical symbol`
- 交易所接入注册表：`adapter registry`（通过 `adapter_kind + exchanges.<name>` 组装）
- 执行记录 / 订单记录落库
- 手动与自动开平仓接口

## 当前已经包含

- SQLite 持久化
- Binance / Aster / Hyperliquid 公共行情接入
- Binance Spot 公共行情接入
- Bybit V5 public websocket 行情接入（`tickers.{symbol}` + `orderbook.1.{symbol}`）
- 多交易所共同交易对自动发现
- 规范化交易对映射（如 `BTCUSDT -> BTC`，`BTC -> BTC`）
- 任意两两交易所组合的 funding 套利机会计算
- 可按配置切换 `cross_exchange` 与 `same_exchange_spot_perp`
- 执行计划生成（按最小下单量 / 最小名义价值 / 入场窗口）
- 真实执行层抽象：
  - Binance Spot 下单 / 平仓 / 查持仓
  - Binance 下单 / 平仓 / 查持仓
  - Aster 下单 / 平仓 / 查持仓
  - Bybit 下单 / 平仓 / 查持仓
  - Hyperliquid 下单 / 平仓 / 查持仓
- 执行记录表 / 订单记录表
- 自动开仓 / 自动平仓循环
- 运行中仓位监听（单腿、反向、数量漂移）与主动换仓
- Binance / Bybit 私有订单事件流基础接入（最小可用版）
- 手动 HTTP 接口：开仓 / 平仓 / 查执行记录 / 查订单记录
- 前端页面改为动态展示多交易所数据
- 基于 TUI 的终端控制台（机会列表 / 详情 / 执行 / 系统状态）

## 当前仍然不包含

- 更多交易所的私有用户流 / 完整订单状态 websocket 回补
- 完整的撤单 / 改单 / 重试编排
- 双腿成交一致性校验与对冲补单
- 更细的风控（最大持仓、单交易所熔断、账户权益检查、黑名单 symbol 等）
- 完整的别名映射表后台维护页面

所以这版已经不是纯 scanner，但也还不是完全体生产 bot。

## 目录重点

- `internal/infrastructure/exchange`
  - `interfaces.go`：统一市场/交易接口
  - `registry.go`：交易所适配器注册表，负责按配置组装 Market/Trade adapter
  - `binance_spot_market.go`：Binance Spot 行情适配器
  - `binance_spot_trade.go`：Binance Spot 下单适配器
  - `cex_market.go`：Binance-like 公共行情适配器（文件名保留历史命名）
  - `cex_trade.go`：Binance-like 下单适配器（文件名保留历史命名）
  - `bybit_market.go`：Bybit V5 perpetual 行情适配器
  - `bybit_trade.go`：Bybit V5 perpetual 交易适配器
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
- `docs/rolling_cycle_aligned_strategy_design.md`：按结算段滚动决策的 funding 套利设计，包含持续监控、续持、翻仓与新配置结构。
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
- `POST /api/v1/executions/events/order`
- `GET /api/v1/market/:symbol`
- `GET /api/v1/system/status`
- `GET /api/v1/snapshot-stats`

`POST /api/v1/executions/events/order` 可用于：

- 本地调试回放某笔订单事件
- 给尚未接入私有 websocket 的交易所临时做人工补录
- 验证 execution 状态机在异步订单更新下是否按预期推进

示例：

```json
{
  "source": "debug_http",
  "exchange": "binance",
  "client_order_id": "cid-123",
  "status": "FILLED",
  "executed_qty": 1,
  "average_price": 101.5,
  "terminal": true,
  "occurred_at_ms": 1710000000123
}
```

## 配置

配置文件见 `configs/config.yaml.example`。

重点参数：

- `strategy.arbitrage_mode`：`cross_exchange` 或 `same_exchange_spot_perp`
- `strategy.hold_selection_mode` / `strategy.opportunity.legacy_hold_selection_mode`：支持 `dynamic_profit`；该模式会按收益自动搜索更优兑现点，不再受 `hold_hours` 截断
- `exchanges.<name>.adapter_kind`：声明该交易所复用哪类接入协议族；当前内置 `binance_like`、`binance_spot`、`bybit_v5` 与 `hyperliquid`
- `exchanges.<name>.arbitrage_group`：同所套利分组；`same_exchange_spot_perp` 模式下，只有同组 spot/perp 会被配对
- `exchanges.<name>.venue_kind`：声明该交易所在策略层复用哪类 venue profile；为空时按交易所名或 `adapter_kind` 推断
- `exchanges.<name>.auth.passphrase_env` / `auth.extra_env` / `adapter_options`：留给 OKX、Bybit 这类协议族的扩展配置位
- `exchanges.<name>.private_ws_base_url`：显式指定私有订单/用户流 websocket 地址；像 Bybit 这类公私有流不共址的交易所应优先配置它
- 当前私有订单事件流最小实现已在 `binance` 与 `bybit_v5` 上启用；其他协议族仍可沿同一 `TradeOrderEventStreamer` 接口继续扩展
- 自定义交易所必须显式配置 `adapter_kind`；系统不再把未知交易所默认归到某个“通用 cex”实现

- `strategy.allowed_symbols`：规范化 symbol 列表，建议写基础币 `BTC/ETH/SOL`
- `strategy.entry_mode`：`maker / mixed / taker`
- `strategy.exit_mode`：`maker / mixed / taker`
- `strategy.capital.*`：`same_exchange_spot_perp` 模式下默认按“现货腿全额占资”推导 effective notional，不再把 leverage 额外乘进可开名义
- `strategy.same_exchange.exit.*`：同所模式下的提前离场护栏；可按“永续 funding 转负 + 历史负费率占比 + 当前平仓盈利”联合决定是否提前退出
- `strategy.execution.enabled`：是否允许真实下单；`false` 时只会生成 dry-run 执行记录
- `strategy.execution.auto_entry`：是否自动开仓
- `strategy.execution.auto_close`：是否自动平仓
- `strategy.execution.position_monitor.*`：运行中仓位监听与异常仓位自动收口；可控制单腿、方向错位、数量漂移的处理方式
- `strategy.execution.replacement.*`：主动换仓配置；系统会用“候选机会净收益 - 当前平仓成本 - 当前继续持有净值”做比较，只有净增益过门槛才切
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

启动 TUI：

```bash
go run ./cmd/tui
```

也可以使用 Makefile：

```bash
make run
make run-tui
```

说明：

- `cmd/tui` 当前复用现有 HTTP API，因此需要先启动 `cmd/server`。
- TUI 默认连接 `http://127.0.0.1:8080`，可通过 `FUNDING_TUI_BASE_URL` 或 `-base-url` 覆盖。
- TUI 默认每轮只拉取前 `200` 条机会，避免终端首屏被超大批次数据拖慢；可通过 `-opportunity-limit` 调整。
- 若要在 TUI 中执行手动开/平仓，需提供 `EXECUTION_API_TOKEN` 或 `-token`。

TUI 主要交互：

- `j/k` 或方向键：移动选择
- `tab`：切换详情 Tab / 执行视图模式
- `/`：搜索 symbol / exchange / venue symbol
- `f` / `F`：切换交易所组合过滤
- `s`：切换排序（`net / score / edge`）
- `o`：开仓确认
- `c`：平仓确认
- `1` / `2` / `3`：切换 `Scanner / Execution / System`
- `r`：刷新
- `?`：帮助
- `q`：退出

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

## 新增交易所的最小接入方式

如果新交易所与现有接入族兼容，通常只需要下面这几步：

1. 在 `configs/config.yaml` 的 `exchanges.<new_name>` 下新增配置，并设置 `adapter_kind`。
2. 若它在策略层能复用现有规则族，可直接补 `venue_kind`；否则再补新的 `VenueProfile`。
3. 如果它的协议与现有族不兼容，再新增一个 `AdapterFactory`，实现 `MarketAdapter/TradeAdapter` 后通过 `exchange_adapter_factories` group 注入。

其中 `adapter_kind` 的语义要尽量具体，例如：

- `binance_like`
- `bybit_v5`
- `hyperliquid`

不要再把它理解成宽泛的 `cex / dex` 场所分类，否则会重新回到“接口长得不一样却被误判为可复用”的问题。

这意味着策略层、机会计算、执行计划与执行服务都不需要为“第 N 家交易所”新增分支。

# 实施任务拆解（Task Breakdown v1）

本文把“多交易所资金费率套利系统从原型继续推进”的实施工作拆成可以执行、可以验收、可以回顾的任务清单。

它的目标不是替代 issue tracker，而是提供一份：

- **面向阶段的任务地图**
- **每阶段的验收标准**
- **当前仓库的完成度评估**
- **继续开发时的优先级依据**

---

## 1. 背景与目标

当前仓库已经不只是 scanner：

- 可以接入多交易所公共行情；
- 可以算 funding arbitrage；
- 可以生成 execution plan；
- 可以落 execution / order 记录；
- 可以手动 / 自动开平仓。


但离“更稳健的生产化套利 bot”还有明显差距，README 里也已经列出了当前未完成的方向。

因此 Task Breakdown v1 的核心目标是：

1. 先把 **架构骨架** 搭对；
2. 再把 **执行安全性** 做到可用；
3. 再逐步补 **状态机 / websocket / alias / 风控 / 预测升级**。

---

## 2. 工作包（Work Packages）

---

### WP1. 多交易所统一架构骨架

#### 目标

建立“继续接更多交易所也不会把策略主链写乱”的基础架构。

#### 主要任务

1. 抽象 `MarketAdapter` 与 `TradeAdapter`
2. 建立 canonical symbol 机制
3. 建立 symbol / opportunity / execution / order 的统一实体
4. 建立 `MarketStore` 作为统一最新快照来源
5. 把机会计算、计划生成、执行服务拆分成独立 service

#### 验收标准

- 接入新交易所时，不需要把策略逻辑复制一份；
- 公共行情与真实交易可以分别迭代；
- plan / execution / order 语义清晰且能落库。

#### 当前状态

**已完成。**

对应落地位置：

- `interfaces.go`
- `strategy_runner.go`
- `planning.go`
- `execution_service.go`
- 各 domain entities。


---

### WP2. Venue 规则统一收口

#### 目标

把交易所特定规则从策略主链中抽离出来，避免“每支持一家交易所就多几段 switch-case”。

#### 主要任务

1. 引入 `VenueProfileRegistry`
2. 统一 funding clamp 与 execution multiplier 来源
3. 补测试验证默认 venue 行为不漂移

#### 验收标准

- funding forecaster 与 execution penalty 使用同一套 venue taxonomy；
- 新 venue 接入时，优先补 profile，而不是改主逻辑。

#### 当前状态

**已完成 v1。**

对应落地位置：

- `venue_profile.go`
- `funding_forecaster.go`
- `execution_penalty.go`。


---

### WP3. 机会计算与执行计划落地

#### 目标

让“套利机会”与“可执行计划”成为两层不同语义，而不是一个混合结构。

#### 主要任务

1. 使用 event-based funding timeline 估算 carry
2. 机会与执行计划分离
3. 在 plan 层加入 min qty / min notional / entry window / basis threshold 约束

#### 验收标准

- `Opportunity` 代表策略上值得关注；
- `ExecutionPlan` 代表当前具备执行条件；
- plan 可以独立查询、展示和执行。

#### 当前状态

**已完成。**

对应落地位置：

- `strategy_runner.go`
- `planning.go`
- `execution_plan.go`。


---

### WP4. 执行安全基础版

#### 目标

先把 execution 做到“不会明显失控”，即使还没有完整状态机。

#### 主要任务

1. 增加 execution 状态常量
2. 开仓前增加账户级风控
3. 增加 API failure circuit breaker
4. 增加 order reconciliation
5. 发生单腿失败时进行 `hedge_close`
6. 禁止异常 open 状态进入 auto-close

#### 验收标准

- live open 前会做基础账户检查；
- 多次 API 失败后 venue 会短暂熔断；
- order status 不再只依赖下单响应；
- `open_partial_failed` 不会再走正常 auto-close。

#### 当前状态

**已完成 v1。**

对应落地位置：

- `config.go`
- `execution_service.go`
- `interfaces.go`
- `cex_trade.go`
- `hyperliquid_trade.go`。

#### 设计备注

WP4 的完成标准不是“执行层已经生产可用”，而是：

- 在没有完整状态机的前提下，尽量把明显高风险路径挡住；
- 通过 risk check / circuit breaker / reconcile / hedge rollback 让系统具备基础自我保护能力；
- 明确 auto-close 只处理正常 opened 记录，不误吃异常路径。


---

### WP5. 双腿执行状态机

#### 目标

把 execution 从“若干状态常量 + 补救逻辑”升级成真正的状态机。

#### 主要任务

1. 明确定义状态图（pending/opened/partial_failed/hedging/closed/...）
2. 为每个状态定义允许的下一步动作
3. 明确 auto-open / auto-close / manual-open / manual-close 的幂等规则
4. 明确 hedge rollback 完成后如何确认净仓位归零

#### 验收标准

- `ExecutionRecord` 状态迁移图在文档中可追踪；
- auto-open / auto-close 的行为不再依赖隐式约定；
- 异常路径不会被误当成正常仓位继续执行。

#### 当前状态

**部分完成。**

当前已有：

- 一批 execution state 常量；
- `open_partial_failed -> hedge_close` 的补救；
- auto-close 对异常状态的排除。


但仍未完成：

- 完整状态转移图；
- 明确幂等与恢复规则；
- hedge 后净仓位确认来源。

#### 建议拆法

若继续做 WP5，建议再拆成：

1. **WP5-A：状态图文档化**
   先把 `pending/opened/partial_failed/hedging/closed/...` 的允许转移画清楚；
2. **WP5-B：状态推进函数**
   把状态转移从 scattered if/switch 收敛到统一入口；
3. **WP5-C：恢复与幂等**
   明确 auto-open / auto-close / manual action 冲突时的处理规则；
4. **WP5-D：净仓位确认**
   明确 hedge 后如何确认真正回到平仓状态。

---

### WP6. 订单状态 websocket 与成交回补

#### 目标

把 execution 状态判断从“轮询推断”升级为“事件驱动 + 对账回补”。

#### 主要任务

1. 接入用户流 / 私有订单 websocket
2. 用成交回报更新 order / execution 状态
3. 让 `reconcileOrder()` 退化为兜底，而不是主来源

#### 验收标准

- execution state 主要由 websocket 事件驱动；
- 轮询只用于丢包、重启恢复或兜底；
- 部分成交 / 撤单 / 补单状态可以被及时识别。

#### 当前状态

**未完成。**

当前仓库仍明确把它列为“当前仍然不包含 / 下一步最值得补”。

#### 设计备注

WP6 和 WP5 实际上是强耦合关系：

- 没有 websocket 主状态源，状态机很多判断只能靠推断；
- 没有状态机，websocket 事件也很难稳定落成 execution 状态。

因此建议把 WP5 / WP6 视为一个大主题下的前后两步，而不是完全独立的两块。

---

### WP7. 账户级风控增强

#### 目标

从“基础风控可用”升级到“更接近生产”的风控体系。

#### 主要任务

1. 黑名单 symbol / venue 支持
2. 更精细的保证金 / 权益 / available ratio / leverage 检查
3. 更稳健的 per-symbol / per-exchange exposure 计算
4. API circuit breaker 持久化 / 可观测化

#### 验收标准

- 风控参数可配置、可解释、可观测；
- 风控拒单会留下清晰 execution record / reason；
- exchange circuit 状态可在系统状态页中查看。

#### 当前状态

**部分完成。**

当前已具备基础版账户风控和 circuit breaker。

---

### WP8. Alias 管理与 symbol family 建模

#### 目标

让跨所 symbol 对齐不只停留在少量硬编码 alias。

#### 主要任务

1. 扩展 alias registry
2. 把 allowlist / canonicalFrom / symbol ingestion 都统一到 alias 层
3. 区分 asset alias 与 contract family alias
4. 如有需要，补后台维护入口

#### 验收标准

- `BTC` / `XBT` 这类 alias 可统一归一；
- alias 规则在一个地方定义，不在多个适配器散落；
- 为更复杂 symbol family 预留扩展点。

#### 当前状态

**部分完成。**

当前仅有基础 alias 层和 `XBT -> BTC`。

---

## 3. 当前完成度总览

| 工作包 | 状态 | 说明 |
|---|---|---|
| WP1 多交易所统一架构骨架 | 已完成 | 基础架构已成型 |
| WP2 Venue 规则统一收口 | 已完成 v1 | venue profile 已引入 |
| WP3 机会计算与执行计划 | 已完成 | opportunity / plan 分层已落地 |
| WP4 执行安全基础版 | 已完成 v1 | 风控 / circuit / reconciliation / auto-close safety 已有 |
| WP5 双腿执行状态机 | 部分完成 | 仍缺完整状态迁移图 |
| WP6 订单状态 websocket 与成交回补 | 未完成 | 目前仍以轮询为主 |
| WP7 账户级风控增强 | 部分完成 | 只有基础版 |
| WP8 Alias 管理与 symbol family | 部分完成 | 仅有基础 alias |

---

## 4. 建议优先级（v1 之后）

若继续按收益 / 风险比排序，建议优先级如下：

1. **WP5 双腿执行状态机**
2. **WP6 订单状态 websocket 与成交回补**
3. **WP7 账户级风控增强**
4. **WP8 Alias 管理扩展**

原因：

- WP5 + WP6 直接决定 execution 是否足够可靠；
- WP7 决定 live 环境的安全边界；
- WP8 决定跨所覆盖率和 symbol 对齐精度。

---

## 5. 一句版本演进摘要

为了方便快速理解，可以把当前系统版本演进概括成：

- **v1**：先把 scanner 变成 planner
- **v2**：把执行层做出基础安全护栏
- **v3**：把 registry 化与状态机补完整

这也是为什么仓库里会同时保留：

- `code_design_v1_scanner_to_planner.md`
- `code_design_v2_execution_safety.md`
- `code_design_v3_registry_and_state_machine.md`

它们共同组成了当前代码设计的阶段性演进记录。

---

## 6. 与其他文档的关系

- `multi_exchange_architecture.md`：解释系统为什么这样分层、模块如何协作；
- `arbitrage_algorithm_and_funding_timeline.md`：解释套利算法、资金时间轴与自动执行逻辑；
- 本文：解释“任务怎么拆、做到哪一步算完成、当前做到哪一步”。

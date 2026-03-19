# 统一交易所接入与统一套利计算架构设计文档（v1）

本文聚焦回答一个更具体的问题：

> 当系统要同时支持多家交易所，并在这些交易所之上做统一套利计算时，接入层、数据层、策略层、执行层应该如何抽象，才能既不把代码写散，也不把策略逻辑绑死在某一家交易所上？

这份文档与 `multi_exchange_architecture.md` 的区别在于：

- `multi_exchange_architecture.md` 更偏 **整体系统分层与演进原则**；
- 本文更偏 **统一接入 + 统一套利计算链路** 的专项设计。

---

## 1. 问题定义

多交易所资金费率套利系统里，最难的不是“多接一个 REST API”，而是：

1. **如何让不同交易所的 symbol 可以统一比较；**
2. **如何让不同交易所的 funding / book / fee / execution friction 进入同一套套利计算模型；**
3. **如何在不破坏主流程的前提下继续接入新 venue。**

如果没有统一架构，常见后果是：

- symbol 对不齐，机会计算大量漏配；
- funding clamp / penalty / fee 规则散落在不同文件；
- 新交易所接入只能复制已有分支，再继续堆 if/switch。

---

## 2. 统一接入目标

v1 版本定义四个统一目标：

### 2.1 统一 symbol 视角

系统内部必须有一层 **canonical symbol**：

- 策略层只看 canonical asset，例如 `BTC`
- 适配器与下单时再回到 `VenueSymbol`，例如 `BTCUSDT` / `XBTUSD`

### 2.2 统一市场数据语义

无论交易所原始返回结构如何不同，进入策略层之后都应统一为：

- `Symbol`
- `FundingSnapshot`
- `BookTopSnapshot`

### 2.3 统一套利计算入口

套利计算必须只依赖统一结构，不直接依赖某个交易所原始 payload。

也就是说，计算链路应尽量写成：

```text
canonical symbol
  -> funding snapshots by exchange
  -> book top snapshots by exchange
  -> pairwise direction evaluation
  -> opportunity
  -> execution plan
```

而不是：

```text
if exchange == binance { ... }
if exchange == aster { ... }
if exchange == hyperliquid { ... }
```

### 2.4 统一 venue-specific rule 来源

虽然主计算流程要统一，但某些 venue-specific 规则仍然客观存在，例如：

- funding clamp
- adaptive short-interval cap
- execution penalty multiplier

因此需要把“venue 差异”收敛到单独规则层，而不是把它散落进机会计算主链。

---

## 3. 统一接入分层

### 3.1 Exchange Capability Layer

这一层负责描述交易所到底能提供什么能力，至少分成两类：

1. `MarketAdapter`
2. `TradeAdapter`

这样系统可以支持：

- 只接行情，不接交易；
- 先接公共能力，后补私有能力；
- 行情与交易分别演进。

### 3.2 Canonicalization Layer

这一层负责解决跨所 symbol 对齐问题，包括：

- raw symbol -> canonical symbol
- alias 归一化
- allowlist 匹配统一

v1 当前只完成了基础 alias 层，例如 `XBT -> BTC`。
后续仍要升级为更完整的 alias registry / symbol family 模型。

### 3.3 Strategy Input Layer

这一层把所有 adapter 输出收口到统一输入：

- tradable symbol inventory
- latest funding snapshots
- latest top-of-book snapshots

`MarketStore` 是当前这层的内存态载体。

它的设计目的不是单纯做 cache，而是作为：

- 机会计算输入源
- execution plan 生成输入源
- 前端查询输入源

的统一快照视图。

### 3.4 Opportunity Engine Layer

这一层负责：

1. pairwise venue 组合
2. 双方向比较（Long A/Short B 与 Long B/Short A）
3. funding carry projection
4. execution penalty / fee / basis / entry window 评估
5. 产出 `Opportunity`

在 v1 当前实现里，这一层实际上又可分成两段：

- **Funding 粗筛段**：优先依赖 funding / funding time / mark 这类相对便宜的数据；
- **盘口精算段**：只在需要时叠加 best bid/ask、最小下单量、名义价值、执行惩罚等昂贵约束。

这样做的目的，是在“尽量别漏机会”和“别把 websocket / snapshot 存储打爆”之间取平衡。

### 3.5 Execution Planning Layer

这一层把 `Opportunity` 转成可执行的 `ExecutionPlan`，处理：

- qty rounding
- min qty / min notional
- capital allocation
- target leverage
- entry / close timing

### 3.6 Execution Layer

这一层只消费 `ExecutionPlan`，不重新理解套利逻辑本身。

它负责：

- auto open / auto close
- manual open / close
- risk check
- circuit breaker
- order reconciliation
- hedge rollback

---

## 4. 统一套利计算设计

### 4.1 核心原则：统一计算主链 + 独立 venue rule layer

统一套利计算不等于“所有交易所完全一样”。

系统应采用下面的结构：

```text
统一主链：
  symbol normalization
  pair generation
  funding projection
  basis / fee / penalty / entry evaluation

venue rule layer：
  funding clamp
  short-interval widening
  execution multiplier
  (future) venue family metadata
```

这样做的好处是：

1. 主链保持可读；
2. venue 差异有统一落点；
3. 新交易所接入时优先补 registry / profile。

### 4.2 方向选择

每个交易所对都必须同时计算：

- Long A / Short B
- Long B / Short A

选择 carry 更优的方向，而不是把方向硬编码死。

### 4.3 时间轴优先于简单小时化

funding 计算必须优先基于 **事件时间轴**，而不是简单按小时化 spread 线性外推。

这是统一套利计算最关键的一条设计约束，因为不同 venue 的 funding interval 本来就可能不同。

更具体地说，统一套利计算链路在 funding 上应遵循：

1. 候选退出点来自两腿真实 funding 事件时间轴；
2. 同一交易所对的两个方向都要完整计算；
3. 选择 carry 更优、而不是“写死方向”的那一边；
4. 真实 funding 事件计数优先于简单小时线性近似。

这也是为什么系统里会强调：

> **direction selection（方向选择）与 funding projection（时间轴收益投影）必须是统一主链中的显式步骤。**

### 4.4 机会与执行计划分层

统一套利计算输出 `Opportunity`；
统一执行入口消费的是 `ExecutionPlan`。

这两层不能混在一起，否则：

- 查询展示语义不清；
- 交易约束会污染机会评分；
- 自动执行很难做幂等与复盘。

### 4.5 v1 的统一成本模型

统一套利计算在收益/成本上，至少要把下面几项都纳入：

1. gross funding carry
2. entry fee / exit fee
3. basis
4. execution penalty
5. safety buffer

其中 execution penalty 在 v1 里已经不再只是固定 `slippage_bps`，而是被拆成：

- entry penalty
- exit penalty
- hedge rollback penalty
- exchange / symbol / time-bucket multiplier

这意味着统一套利计算链路虽然对外表现为一个 `netExpectedPNL`，但内部已经开始显式区分不同摩擦来源。

### 4.6 v1 的执行计划生成约束

统一套利计算并不会直接输出“立即下单”指令，而是先经过 planning 层做一轮交易约束过滤，包括：

- step size round-down
- min qty
- min notional
- basis threshold
- entry window open / close
- target close time

因此系统语义是：

```text
Opportunity = 值不值得看
ExecutionPlan = 当前能不能按交易所约束去做
```

---

## 5. v1 当前已落地的关键点

### 已落地

1. `MarketAdapter` / `TradeAdapter` 分离
2. canonical symbol 与基础 alias
3. `MarketStore`
4. `Opportunity` / `ExecutionPlan` / `ExecutionRecord` / `OrderRecord` 分层
5. venue profile 规则层
6. 风控 / circuit breaker / reconciliation 基础版

### 未完成

1. 完整 alias registry
2. 订单状态 websocket / 用户流
3. 完整双腿执行状态机
4. 更完整的 venue family / contract family 抽象

---

## 6. 接入新交易所时的规范

若继续接新 venue，建议遵循以下顺序：

1. 实现 `MarketAdapter`
2. 若需要真实执行，再实现 `TradeAdapter`
3. 确认 canonical symbol / alias 行为
4. 若有特殊规则，补 `VenueProfile`
5. 补对应测试
6. 最后再讨论是否需要改主流程

换句话说：

> **默认先补适配器和 profile，最后才考虑碰策略主链。**

---

## 7. v1 之后的演进建议

统一交易所接入与统一套利计算架构在 v1 之后，建议继续演进到：

### v2

- 完整 alias registry
- venue family / contract family 抽象
- 更清晰的 opportunity engine 子模块拆分

### v3

- 私有订单 websocket / 成交回补
- 真正的双腿执行状态机
- execution 与 reconciliation 的事件驱动化

### v4

- 资本效率约束
- 借贷成本 / 保证金占用
- 更细粒度 execution quality model

---

## 8. 与仓库内其他文档的关系

- `multi_exchange_architecture.md`：更偏系统总览；
- `task_breakdown_v1.md`：更偏任务拆解和完成度；
- 本文：更偏“统一接入 + 统一套利计算”的专项架构。

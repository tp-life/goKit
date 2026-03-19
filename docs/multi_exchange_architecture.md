# 多交易所架构设计（Architecture Design v1）

本文把当前仓库的“多交易所资金费率套利系统”抽象成一份可持续演进的架构设计文档，补齐：

- 为什么仓库要按 **多交易所 / 多适配器 / 规范化 symbol** 的方式组织；
- 当前各模块的边界、输入输出、依赖方向；
- 已经落地的能力与仍未完成的能力；
- 后续继续接入新交易所、新执行策略、新风控能力时应遵循的约束。

这份文档的定位不是“功能说明书”，而是 **系统设计蓝图**。

---

## 1. 设计目标

系统要解决的核心问题不是“接 Binance/Aster/Hyperliquid 三家交易所”，而是：

1. **把任意交易所纳入统一的行情 / 交易 / 规则模型；**
2. **把机会计算与交易执行从具体 venue 细节里剥离出来；**
3. **让系统能从 scanner 平滑演进到 plan/execution bot，而不是推倒重来。**

因此当前架构优先满足下面几个目标：

- **可扩展**：继续加第 4、第 5 家交易所时，尽量不碰策略主链；
- **可比较**：同一个 canonical symbol 能跨 venue 正确聚合；
- **可执行**：机会不仅能算，还能生成 plan、落 execution record、查 order record；
- **可演进**：先支持轮询与基础风控，后续再升级到 websocket 回报、状态机、复杂风控。

---

## 2. 逻辑分层

当前仓库可以抽象成四层：

### 2.1 Exchange Adapter Layer

目录：`internal/infrastructure/exchange`

职责：

- 屏蔽各交易所 REST / WS / 签名 / symbol 格式差异；
- 向上暴露统一的市场侧与交易侧接口；
- 把“交易所原始返回”转换成仓库内部的通用结构。

核心接口：

- `MarketAdapter`
- `TradeAdapter`。  


这层的原则是：

> **适配器只负责“怎么和交易所说话”，不负责“为什么这样订阅/这样下单”。**

---

### 2.2 Domain / Persistence Layer

目录：

- `internal/domain/entity`
- `internal/domain/repository`
- `internal/infrastructure/persistence`

职责：

- 保存统一的 symbol / snapshot / opportunity / execution / order 数据模型；
- 定义仓储接口，让应用服务不直接依赖具体 DB 实现；
- 让策略层与执行层共享同一套持久化语义。

典型实体包括：

- `Symbol`
- `FundingSnapshot`
- `BookTopSnapshot`
- `Opportunity`
- `ExecutionPlan`
- `ExecutionRecord`
- `OrderRecord`。  


---

### 2.3 Application Service Layer

目录：`internal/application/service`

职责：

- 组织市场数据；
- 计算机会；
- 生成执行计划；
- 驱动自动/手动执行；
- 管理执行状态、风险控制与执行记录。

当前最核心的服务：

- `StrategyRunner`：负责市场扫描、机会精算、execution plan 生成；
- `ExecutionService`：负责自动开平仓、风控、对冲回滚、订单/执行记录更新；
- `MarketStore`：负责最新 symbol/funding/book snapshot 的内存态。  


---

### 2.4 Interface Layer

目录：`internal/interface/http`

职责：

- 把 application service 暴露成 HTTP API；
- 给前端页面与外部调用方提供查询/执行入口。

---

## 3. 为什么要把 MarketAdapter 和 TradeAdapter 分开

这是当前多交易所架构最重要的设计之一。

### 3.1 原因

不同交易所常见的现实差异是：

1. **行情能力** 与 **交易能力** 的接入方式不同；
2. 有些交易所行情很好接，但真实交易接口认证、签名、规则完全不同；
3. 后续可能出现“只接行情，不接交易”或“交易能力后补”的阶段。

因此系统把交易所集成拆成两块：

- `MarketAdapter`：只关心 symbol 抓取、行情订阅、市场数据落地；
- `TradeAdapter`：只关心下单、平仓、查仓位、查订单状态、查账户快照。  


### 3.2 带来的好处

- 行情与交易各自演进，不互相绑死；
- 新增交易所时，可以先接公共行情，后补私有交易；
- 风控、执行状态机、订单对账都可以只依赖 `TradeAdapter`。

---

## 4. Canonical Symbol 设计

### 4.1 为什么需要 canonical symbol

同一个资产在不同交易所的命名可能不同：

- Binance: `BTCUSDT`
- Hyperliquid: `BTC`
- 某些 venue: `XBTUSD`

如果没有 canonical symbol，策略层就无法判断这些是不是“同一个标的”。

所以系统内部统一保存：

- `Symbol`：规范化的 canonical asset key，例如 `BTC`
- `VenueSymbol`：交易所原始 symbol，例如 `BTCUSDT` / `XBTUSD`。  


### 4.2 当前 alias 管理策略

当前实现已经有一层基础 alias 归一化：

- `XBT -> BTC`
- `canonicalFrom()` 与 `normalizeAllowed()` 共用同一套 alias 规则。  


但这只是 **第一阶段**，不是完整 alias 平台。后续仍需要：

- 更完整的 alias registry；
- 后台维护能力；
- 更复杂的跨所 symbol family 建模（例如杠杆/乘数/合约族差异）。

---

## 5. 市场数据流

### 5.1 启动阶段

`StrategyRunner.Start()` 会：

1. 让各 `MarketAdapter` 抓取 tradable symbols；
2. 把各交易所实际可交易 symbol 全量入库；
3. 构建“至少在两家交易所共同存在”的 canonical watchlist；
4. 初始化 funding 订阅集与 book 深扫池。  


### 5.2 运行阶段

市场数据进入系统后：

- `MarketAdapter` 把 funding/book snapshot 写入 `MarketStore`；
- `StrategyRunner` 周期性读取 `MarketStore` 进行机会计算；
- 机会再进入 plan 生成和持久化。

这意味着：

> `MarketStore` 是当前“机会计算 / 执行计划 / 前端查询”的共同数据源。



---

## 6. 机会计算与执行计划

### 6.1 StrategyRunner 的职责

`StrategyRunner` 不只是在“算套利”，它实际上做三件事：

1. 组织全市场 symbol 与订阅范围；
2. 计算机会（opportunity）；
3. 生成可执行的 `ExecutionPlan`。  


### 6.2 为什么要区分 Opportunity 和 ExecutionPlan

原因是：

- `Opportunity` 是“从策略角度看是否值得关注”；
- `ExecutionPlan` 是“从交易约束角度看现在能不能下单”。

一个机会可以存在，但不一定满足：

- 最小下单量；
- 最小名义价值；
- entry window；
- 当前 basis 阈值。

所以 plan 是 opportunity 的 **可执行子集**。  


---

## 7. Venue Profile 设计

### 7.1 问题背景

随着支持的交易所增加，策略层逐渐出现大量 venue-specific 规则，例如：

- funding clamp；
- short-interval adaptive clamp；
- execution penalty multiplier。

如果这些规则散落在不同 service 文件里，后果是：

- 规则来源不一致；
- 新 venue 接入时容易漏改；
- 资金预测与执行惩罚可能对同一交易所使用不同 taxonomy。

### 7.2 当前方案

为此系统引入：

- `VenueProfile`
- `VenueProfileRegistry`

由它统一承载每个 venue 的规则画像。  


当前：

- `FundingForecaster` 使用 venue profile 获取 funding clamp；
- `StrategyRunner` / execution penalty 使用同一 registry 获取 execution multiplier。  


这意味着：

> funding 模型与 execution friction 模型已经开始共享同一套 venue taxonomy。

---

## 8. 执行层架构

### 8.1 执行记录与订单记录

系统区分：

- `ExecutionRecord`：记录一次 plan 级别执行的总体状态；
- `OrderRecord`：记录具体某条腿、某个 phase 的订单明细。  


这样做的目的是：

- 从 plan 维度看一次执行是否成功；
- 从 order 维度追踪每条腿到底发生了什么。

### 8.2 当前执行流程

`ExecutionService` 负责：

- auto open / auto close loop；
- manual open / close；
- risk check；
- API circuit breaker；
- order reconciliation；
- hedge rollback。  


### 8.3 当前状态机完成度

当前执行层已经有一批明确状态：

- `pending_open`
- `opened`
- `open_partial_failed`
- `open_hedging`
- `pending_close`
- `closed`
- `close_partial_failed`
- `close_hedging`
- `risk_blocked`
- `api_circuit_open` 等。  


但它还不是“完整的双腿执行状态机”。

当前实现的保守原则是：

- `open_partial_failed` 发生后，优先 `hedge_close` 回滚；
- `open_partial_failed` / `open_hedging` 等异常状态 **不允许**再进入 auto-close 扫描。  


---

## 9. 当前已落地能力 vs 未完成能力

### 9.1 已落地

- 多交易所公共行情抽象；
- 多交易所交易抽象；
- canonical symbol 与基础 alias；
- funding arbitrage 机会计算；
- execution plan；
- 自动/手动开平仓；
- execution/order 持久化；
- 基础账户风控；
- API failure circuit breaker；
- 订单轮询式 reconciliation；
- venue profile 统一规则来源。

### 9.2 尚未完成

- 私有用户流 / 订单状态 websocket 回补；
- 完整双腿执行状态机；
- 撤单 / 改单 / 重试编排；
- 更细粒度风控；
- 完整 alias 管理后台；
- 更复杂的 venue family / contract family 抽象。

---

## 10. 后续演进约束

后续继续开发时，建议遵守以下架构约束：

1. **不要把交易所名硬编码重新散落回策略/执行主链；**  
   venue-specific 规则优先进入 `VenueProfileRegistry`。

2. **不要让 `ExecutionService` 直接依赖某家交易所私有结构；**  
   统一通过 `TradeAdapter` 扩展能力。

3. **不要跳过 canonical symbol 层直接在策略里拼 venue symbol；**  
   venue symbol 只用于适配器与下单明细。

4. **新增复杂执行状态时，先补设计文档，再补状态转移与测试；**  
   避免 execution record 与文档语义脱节。

5. **若要接 websocket 用户流，优先把它作为 execution state source 引入，**
   而不是继续把更多推断逻辑堆进 `reconcileOrder()`。

---

## 11. 与仓库内其他文档的关系

- 本文：解释 **系统架构、模块边界、演进原则**；
- `arbitrage_algorithm_and_funding_timeline.md`：解释 **套利算法、资金费率时间轴、自动执行时序**；
- `task_breakdown_v1.md`：解释 **实施任务拆解、阶段目标、完成标准与当前进度**。


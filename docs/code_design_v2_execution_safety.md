# 代码设计文档 v2：执行安全与回滚

本文记录第二阶段代码设计思路：在已有 Planner 基础上，如何把执行层做成“基础可用、出现异常时不至于明显失控”。

---

## 1. 目标

v2 关注“执行安全基础版”，包括：

1. 开仓前基础风控
2. API failure 熔断
3. 订单状态 reconciliation
4. open partial failure 时的紧急 hedge rollback
5. auto-close 的保守状态过滤

---

## 2. 关键设计

### 2.1 `ExecutionService`

执行层集中在 `ExecutionService`，统一处理：

- auto open
- auto close
- manual open / close
- risk check
- circuit breaker
- reconcile

### 2.2 `ExecutionRecord` 与 `OrderRecord`

执行层必须同时保留：

- plan 级状态
- order 级明细

否则只看一个 execution status 很难复盘异常路径。

### 2.3 先做保守安全，再做完整状态机

v2 的思路不是一步到位实现完整双腿状态机，而是先用保守策略把高风险路径挡住，例如：

- partial open 立即 hedge
- `open_partial_failed` 不进入 auto-close

### 2.4 为什么 `ExecutionRecord` 不能只看一个最终状态

在执行安全阶段，最重要的经验之一是：

> **单看 execution 的最终 status，不足以解释异常执行路径。**

例如：

- 一条腿成功、一条腿失败；
- 补救 hedge_close 又失败；
- 下单返回不明确，但持仓已存在；
- auto-close 在错误状态下被触发。

因此 v2 必须同时保存：

- `ExecutionRecord`：plan 级语义
- `OrderRecord`：腿级 / phase 级语义

这样才能在出现 partial / hedge / reconcile 问题时复盘“到底哪条腿发生了什么”。

### 2.5 v2 的 auto-close 保守原则

在没有完整双腿状态机之前，auto-close 只能采用保守策略。

具体规则应当是：

1. 只允许真正完整打开的 `opened`
2. 允许 dry-run 的 `dry_run_opened`
3. `open_partial_failed` / `open_hedging` 等异常中间态不进入 auto-close

原因不是“这些状态永远不需要平仓”，而是：

- 它们已经进入异常路径；
- 系统是否仍然残留净仓位，需要更完整状态机或 websocket 事件才能安全判定；
- 在判定能力不完整时，保守跳过比误发 close 更安全。

### 2.6 v2 的 reconciliation 设计边界

`reconcileOrder` 在 v2 的定位是：

1. 先轮询 `GetOrderStatus`
2. 再 fallback 到 `GetPosition`
3. 最后把执行结果回写到 `OrderRecord`

它的目标是“减少执行歧义”，而不是“替代用户流 websocket”。

因此 v2 必须明确：

- polling reconciliation 是过渡能力；
- websocket / user stream 才是后续应当成为主状态源的方案。

---

## 3. v2 完成边界

### 已完成

- 基础风控
- circuit breaker
- reconcileOrder
- hedge rollback
- auto-close safety filter

### 未完成

- 完整双腿执行状态机
- websocket 驱动的 execution 状态源
- 更细粒度的恢复 / 幂等设计
- cancel / amend / retry orchestration

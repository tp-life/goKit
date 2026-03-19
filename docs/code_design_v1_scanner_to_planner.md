# 代码设计文档 v1：从 Scanner 到 Planner

本文记录第一阶段代码设计思路：如何把“只做市场扫描”的原型，演化成“能生成执行计划”的系统。

---

## 1. 目标

v1 的重点不是直接做真实交易，而是先完成三件事：

1. 多交易所 symbol / funding / book 数据统一收集；
2. 统一套利机会计算；
3. 从机会计算进入 `ExecutionPlan`。

---

## 2. 关键设计

### 2.1 `StrategyRunner` 作为 orchestrator

`StrategyRunner` 统一负责：

- 初始化 tradable symbols
- 构建 watchlist
- 维护 funding / deep-scan 订阅集合
- 计算 `Opportunity`
- 生成 `ExecutionPlan`

### 2.2 `MarketStore` 作为统一最新快照

为避免 adapter 之间互相耦合，引入 `MarketStore` 作为最新 snapshot 的统一来源。

### 2.3 `Opportunity` 与 `ExecutionPlan` 分层

v1 最重要的设计决策之一是：

- `Opportunity`：从策略视角判断值不值得做
- `ExecutionPlan`：从交易约束视角判断能不能做

---

## 3. v1 完成边界

### 已完成

- 统一 market ingestion
- 统一机会计算
- 计划生成

### 未完成

- 执行状态机
- 用户流 websocket
- 完整风控


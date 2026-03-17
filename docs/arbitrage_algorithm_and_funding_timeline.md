# 套利机会计算算法与策略说明（Binance / Aster / Hyperliquid）

本文解释当前 `StrategyRunner` 在“资金费率套利监控”中的完整计算链路，并重点说明“资金费率结算时间不一致”问题的修复方法。

## 1. 整体策略分层

系统采用三层结构，兼顾全市场覆盖与资源成本：

1. **全市场基础池**：同一 canonical symbol 至少在两家交易所可交易。
2. **Funding 粗筛层**：用 funding/时间信息轻量打分，筛选值得深扫的币。
3. **盘口深扫 + 精算层**：结合实时盘口、手续费、滑点、安全垫做净收益评估。

这样可以避免把所有 symbol 全量高频深扫，同时不轻易漏掉潜在机会。

---

## 2. 粗筛层算法（不依赖盘口）

对于每个 symbol、每个交易所对：

- 读取双方 funding 快照；
- 通过 `bestFundingDirection` 同时比较双向（Long A/Short B 与 Long B/Short A）；
- 若最优方向的 `CarryRate > 0`，按如下打分：

```text
score = CarryRate * 10000 * fundingTimeWeight(projectedFundingTime)
score *= (1 + 0.05 * (exchangeCount - 2))
score += CarryRateHourlyEquivalent * 1000   (若 > 0)
```

含义：
- `CarryRate` 是事件模型下的真实资金收益率；
- `fundingTimeWeight` 偏好更接近结算的机会；
- 交易所覆盖更多时略微加权。

---

## 3. 精算层算法（机会落地）

对于每个 symbol、每个交易所对：

1. 拉取双方 funding、book top、symbol metadata（任一缺失即跳过）。
2. 调用 `bestFundingDirection` 选择方向及最优 projection。
3. 计算收益与成本：

```text
grossFundingPNL = notional * projection.CarryRate
entryFeePNL     = longEntryFee + shortEntryFee
exitFeePNL      = longExitFee + shortExitFee
slippagePNL     = notional * slippageBps / 10000
netExpectedPNL  = grossFundingPNL - entryFeePNL - exitFeePNL - slippagePNL - safetyBuffer
```

4. 计算基差：

```text
basisBps = |shortBid - longAsk| / longAsk * 10000
```

5. 状态判定顺序：
- 数据是否过期；
- funding carry 是否正；
- basis 是否超过“动态阈值”（基础阈值按持有时长可放宽）；
- net pnl 是否过最小门槛；
- 是否在 entry window。

---


## 3.1 动态 Basis 阈值（持有越久，允许略大价差）

为贴合 carry 策略，系统将 basis 阈值从固定值升级为动态值：

```text
allowedBasisBps = max_spread_bps * (1 + (dynamic_max_spread_multiplier - 1) * clamp(funding_window_hours / dynamic_max_spread_reference_hours, 0, 1))
```

含义：
- 短窗口机会接近基础阈值 `max_spread_bps`；
- 持有窗口越长，阈值可线性放宽；
- 但不会超过 `max_spread_bps * dynamic_max_spread_multiplier`。

## 4. 方向选择逻辑（bestFundingDirection）

每个交易所对都会**双向完整计算**：

- 方向 1：Long A / Short B
- 方向 2：Long B / Short A

比较优先级：
1. `CarryRate` 更高；
2. 若相等，`CarryRateHourlyEquivalent` 更高；
3. 若仍相等，`ProjectedFundingTime` 更早。

因此方向不是固定的，会随 funding 快照变化自动切换。

---

## 5. 关键修复：不同结算时间的事件时间轴建模

## 5.1 旧问题

旧实现只把候选时点限制在 `{nextLong, nextShort}`，并且每条腿在窗口内最多计 1 次 funding。

在 `1h vs 4h/8h` 的场景下，会出现：
- 短周期腿在长周期腿第一次结算前实际上发生了多次 funding；
- 但模型只算 1 次，导致 carry 被低估甚至方向判断错误。

## 5.2 新实现

### A) 构建候选时点 `buildFundingCandidateTimes`

- 从双方 `nextFundingTime` 出发，按 `FundingIntervalHours` 生成时间轴；
- 合并去重；
- 截断到 `horizon = max(max(nextLong, nextShort), now + hold_hours)`。

### B) 事件计数 `fundingEventCountUntil`

对每个候选时点 `T`，分别计算多腿/空腿在 `[now, T]` 内发生的 funding 次数：

```text
count = 0, if nextFunding > T
count = 1 + floor((T - nextFunding)/interval), if interval > 0 and nextFunding <= T
count = 1, if interval <= 0 and nextFunding <= T
```

### C) 逐候选点评估 carry

```text
shortMultiplier = 1 + decay + decay^2 + ... (共 shortCount 项)
longMultiplier  = 1 + decay + decay^2 + ... (共 longCount 项)
carryRate       = shortMultiplier * shortFundingRate - longMultiplier * longFundingRate
hourlyEq        = carryRate / windowHours
```

在所有候选点中选择最优 projection。
其中 `decay` 来自配置 `funding_rate_continuation_decay`（默认 0.6）：
- `1.0` 表示不衰减（线性乘次数，激进）
- `(0,1)` 表示越远期结算贡献越小（保守，降低对当前 1h 极端费率的过拟合）


> 这使得模型可以在高建仓成本场景下评估“更远退出点”（例如 24h）是否能靠多轮 funding 覆盖成本，而不是只盯最近结算点。

---

## 6. 修复后带来的效果

在“结算时间错位”场景中：
- 能正确反映一侧多次结算的累计收益/成本；
- 能更准确地比较双向与退出时点；
- 粗筛与精算的排序一致性更高，减少误判。

---

## 7. 当前模型边界

当前是“已知 nextFundingRate + interval 的事件外推”模型，优点是稳定、低成本、实时；
但它不预测未来 funding rate 变化（只对次数做时间轴累加）。

若后续要进一步提升，可扩展：
- 按历史序列做 funding term-structure 预测；
- 对不同交易所的 funding cap/floor 规则做更细建模；
- 引入资金利用率、保证金占用、借贷成本等资本效率约束。

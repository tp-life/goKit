# 套利机会计算算法与策略说明（Binance / Aster / Hyperliquid）

本文系统梳理当前仓库里的套利策略、机会筛选、执行编排、关键风险点与模型边界，方便你从“交易策略”和“工程实现”两个维度一起理解当前版本。

---

## 0. 一句话概括当前策略

这是一个**跨交易所资金费率套利（funding arbitrage）监控 + 执行原型**：

- 在多个交易所中寻找同一个 canonical symbol；
- 同时比较 `Long A / Short B` 与 `Long B / Short A` 两个方向；
- 用“真实 funding 事件时间轴”而不是简单小时化线性外推来估算持仓收益；
- 再叠加手续费、滑点、基差与入场窗口限制，筛出可执行机会；
- 最后生成执行计划，并支持自动/手动开平仓。

它的核心目标不是押方向，而是赚取**跨平台 funding 差 + 时间窗口中的 carry**。

## 1. 整体策略分层

系统采用三层结构，兼顾全市场覆盖与资源成本：

1. **全市场基础池**：同一 canonical symbol 至少在两家交易所可交易。
2. **Funding 粗筛层**：用 funding/时间信息轻量打分，筛选值得深扫的币。
3. **盘口深扫 + 精算层**：结合实时盘口、手续费、滑点、安全垫做净收益评估。

这样可以避免把所有 symbol 全量高频深扫，同时不轻易漏掉潜在机会。

### 1.1 为什么要分层

如果对所有 symbol、所有交易所、所有盘口都做高频精算，会有几个问题：

1. websocket 连接与订阅压力大；
2. 高频盘口快照落库会膨胀；
3. 很多 symbol 实际上 funding 差很小，不值得消耗资源。

所以现在的实现是：

- **funding/mark 数据尽量全市场覆盖**，因为它便宜；
- **book top 数据优先给深扫池**，因为它贵；
- **机会精算层允许遍历基础池**，但缺失盘口的 pair 会自然跳过。

这是一种“便宜数据做广覆盖，昂贵数据做重点打击”的结构。

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

### 3.2 这里真正衡量的是什么

这套模型评估的不是“价格会不会涨跌”，而是：

> 在当前盘口建仓、持有到某个 funding 结算点、再退出时，是否还有足够的净 carry 覆盖所有交易成本与风险缓冲。

所以它本质上是一个**carry trade / market-neutral spread trade** 模型，而不是方向交易模型。

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

### 5.3 为什么这是策略上的关键点

如果你把所有 funding 都小时化，再假设“未来每小时都一样”，会有两个大问题：

1. **错误地忽略真实结算边界**：真实收入只会在 funding event 发生时兑现；
2. **错误地低估/高估多次结算腿**：尤其是 `1h vs 4h/8h` 的错位场景。

所以当前实现把“下一次 funding 时间 + funding interval”当成最重要的离散事件输入，先保证时间轴正确，再做收益估算。

---

## 6. 修复后带来的效果

在“结算时间错位”场景中：
- 能正确反映一侧多次结算的累计收益/成本；
- 能更准确地比较双向与退出时点；
- 粗筛与精算的排序一致性更高，减少误判。

---

## 7. 执行层风险控制与本次 review 的关键修复

### 7.1 开仓双腿部分成交的真实风险

套利执行最大的工程风险之一，不是“机会算错”，而是：

- 两腿下单并不是原子操作；
- 一条腿成交，另一条腿失败；
- 账户瞬间暴露成单边方向仓位。

这个风险在波动较大的市场里非常致命，因为你本来想做 market-neutral，结果变成了裸露方向风险。

### 7.2 当前实现的补救逻辑

当 `open` 阶段出现部分失败时，系统会：

1. 记录已成功提交的腿；
2. 对成功腿立即发起 `hedge_close`；
3. 该补救单使用：
   - `reduce-only`
   - `IOC`
   - `MARKET`

目的就是尽快把已经打开的单边仓位回滚掉。

### 7.3 本次修复的问题

本次 review 中修复了一个关键执行漏洞：

- **之前的对冲补救单直接复用了原始开仓 side**；
- 这会导致 successful leg 在补救时继续按同方向下单；
- 在单向持仓模式下，这不是“平仓”，而是“继续加仓”，风险会放大。

修复后，`hedge_close` 会显式反转方向：

- 原 `BUY` -> `SELL`
- 原 `SELL` -> `BUY`

这才符合“对冲补救 / 紧急回滚”的真实语义。

---

## 8. 当前模型边界

当前是“已知 nextFundingRate + interval 的事件外推”模型，优点是稳定、低成本、实时；
但它不预测未来 funding rate 变化（只对次数做时间轴累加）。

若后续要进一步提升，可扩展：
- 按历史序列做 funding term-structure 预测；
- 对不同交易所的 funding cap/floor 规则做更细建模；
- 引入资金利用率、保证金占用、借贷成本等资本效率约束。

---

## 9. 你接下来最值得继续加强的方向

如果目标是把这套系统继续往生产套利 bot 演进，优先级建议如下：

1. **订单状态 websocket 与成交回补**
   - 避免只依赖下单响应判断成交；
   - 解决订单异步成交、撤单、部分成交补全问题。
2. **双腿执行状态机**
   - 明确 `pending/opened/partial_failed/hedging/closed/...`；
   - 为每一种执行分叉定义补救动作。
3. **账户级风控**
   - 最大单币敞口；
   - 最大单交易所敞口；
   - 保证金/权益/可用余额检查；
   - API 失败熔断。
4. **资金费率预测升级**
   - 让第 2 次及之后的 funding 不是简单 decay 平滑，而是带历史结构信息。
5. **执行质量评估**
   - 对比预估 basis / slippage 与真实成交；
   - 给每个交易所、symbol、时段建立执行画像。

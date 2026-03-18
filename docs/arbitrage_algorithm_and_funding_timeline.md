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
slippagePNL     = notional * projectedExecutionPenaltyBps / 10000
netExpectedPNL  = grossFundingPNL - entryFeePNL - exitFeePNL - slippagePNL - safetyBuffer
```

其中 `projectedExecutionPenaltyBps` / `execution penalty model` 不再只是固定 `slippage_bps`，而是组合了：

- `entry penalty`：入场滑点 / 盘口冲击；
- `exit penalty`：退出时残留 basis 与执行摩擦；
- `hedge rollback penalty`：路径更长、单腿异常时的对冲回滚冗余；
- exchange / symbol / time-bucket 经验乘子。

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
event1_rate          = current funding snapshot
event2+ future_rate  = regime-aware predictor(current, recentAvg, historyMean, zscore, reversion)
carryRate            = Σ short_leg(event_k * decay_weight_k) - Σ long_leg(event_k * decay_weight_k)
hourlyEq             = carryRate / windowHours
```

在所有候选点中选择最优 projection。
其中 `decay` 来自配置 `funding_rate_continuation_decay`（默认 0.6）：
- `1.0` 表示不衰减（线性乘次数，激进）
- `(0,1)` 表示越远期结算贡献越小（保守，降低对当前 1h 极端费率的过拟合）


> 这使得模型可以在高建仓成本场景下评估“更远退出点”（例如 24h）是否能靠多轮 funding 覆盖成本，而不是只盯最近结算点。

### 5.2.1 当前已升级到 regime-aware forecaster

当前代码不再把“第 2 次及以后 funding”简单视为一个固定的 `futureRate`：

- 会读取历史 funding 序列；
- 计算 `historyMean / stdDev / recentAvg / zscore`；
- 识别当前处于 `stable_carry / elevated_reversion / extreme_reversion / sign_flip_risk` 等状态；
- 再按不同状态给出不同的均值回归速度与 continuation decay；
- 并额外叠加交易所级别的 funding cap / floor clamp，避免把超极端 funding 机械外推到多轮路径。

对于 Binance / Aster 这类 Binance-like venue：

- 常规情况下仍参考 8h cap；
- 但若某个币本身是 `1h / 4h` 这类短周期 funding，不再做“严格线性缩放后再强压交集”；
- 而是进入自适应短周期 clamp：结合当前 funding、历史波动带、完整 8h cap 一起放宽，
  以免把短周期 funding alpha 直接抹掉。

这意味着：

- 极端 funding 不会被机械地线性外推；
- 交易所本身存在的 funding 上下限会参与约束未来路径；
- 多轮 funding 的远期收益会更保守、更贴近真实均值回归过程；
- 同样是 `1h vs 4h/8h`，不同 symbol 的预测路径会因为历史结构不同而不同。

### 5.2.2 形式化规则算法（抽象版）

为了方便后续 review、复盘和扩展，可以把当前实现抽象成下面这套规则：

#### 输入

对于每一条腿（Long leg / Short leg），至少需要：

- `exchange`
- `symbol`
- `currentFundingRate`
- `nextFundingTime`
- `fundingIntervalHours`
- `historyMean / historyStdDev / recentAvg`

并且对于同一个交易所对，要同时计算两个方向：

1. `Long A / Short B`
2. `Long B / Short A`

#### 规则 1：先构建每条腿自己的 funding 预测器

```text
event_1_rate = currentFundingRate
event_k_rate = clamp(
  historyMean + (baselineRate - historyMean) * (1 - reversion)^(k-1),
  effectiveFloorRate,
  effectiveCapRate
), k >= 2
```

其中：

- `baselineRate` 是 `current / recentAvg / historyMean` 的折中；
- `reversion` 来自当前 funding 所处 regime；
- `effectiveFloorRate / effectiveCapRate` 来自交易所机制 + 历史带的联合约束。

#### 规则 2：候选退出点来自两条腿 funding 时间轴的并集

```text
timeline_leg = [nextFundingTime, nextFundingTime + interval, nextFundingTime + 2*interval, ...]
candidateTimes = union(timeline_long, timeline_short)
horizon = max(max(nextLong, nextShort), now + holdHours)
```

也就是说，系统不会假设“最早 funding 点就是最优退出点”，而是会把两边各自可能发生 funding 的时间都拿出来比较。

#### 规则 3：对每个候选退出点，分别计算两边 funding 次数

```text
longCount  = fundingEventCountUntil(now, T, longNextFunding, longInterval)
shortCount = fundingEventCountUntil(now, T, shortNextFunding, shortInterval)
```

这一步是处理 `1h vs 4h/8h` 的关键，因为到同一个退出时点时，两边发生的 funding 次数往往不同。

#### 规则 4：逐腿累计 carry，再做净额

```text
longCarry  = Σ event_k_rate(long)  * decayWeight(k)
shortCarry = Σ event_k_rate(short) * decayWeight(k)
carryRate  = shortCarry - longCarry
hourlyEq   = carryRate / windowHours
```

其中：

- 第 1 次 event 权重固定为 `1.0`；
- 第 2 次及以后按 `continuationDecay^(k-2)` 衰减；
- 这样既保留多轮 funding，又避免把远期收益无限线性外推。

#### 规则 5：在所有候选退出点中选择最优 projection

优先级如下：

1. `carryRate` 更高者优先；
2. 若相同，则 `hourlyEq` 更高者优先；
3. 若仍相同，则更早退出者优先。

#### 规则 6：在两个方向中选择更优方向

对于同一对交易所，系统始终会同时计算：

- `Long A / Short B`
- `Long B / Short A`

哪个方向的最优 projection 更高，就返回哪个方向，而不是预先固定方向。

### 5.2.3 特殊规则：Binance / Aster 的 1h / 4h funding 合约

对于 Binance / Aster 这类 Binance-like venue：

- 常规基础 cap 仍以 `8h ±0.75%` 为基准；
- 若某个 symbol 的 funding interval 为 `1h / 4h`，仍然先得到一个按 interval 缩放后的 `venueCap`；
- 但若当前 funding 与历史波动显示这个 symbol 本身就处在短周期高 funding 状态，
  则不会再被“严格线性 1h/4h cap + 历史带交集”机械压住；
- 系统会进入 `adaptive short-interval clamp`，在完整 `8h cap` 包络内适度放宽。

抽象写法如下：

```text
venueCap   = 0.0075 * intervalHours / 8
dynamicCap = min(
  0.0075,
  max(abs(currentRate) * 1.25, abs(historyMean) + 4*historyStdDev, abs(venueCap))
)
```

若 `dynamicCap > venueCap`，则使用更宽的 `[-dynamicCap, +dynamicCap]` 作为有效约束区间。

### 5.2.4 示例一：Binance 1h vs Aster 4h

假设当前时间是 `10:00`，同一个币种在两边的 funding 信息如下：

- Binance
  - `interval = 1h`
  - `nextFunding = 11:00`
  - `currentFunding = +0.12%`
- Aster
  - `interval = 4h`
  - `nextFunding = 14:00`
  - `currentFunding = -0.20%`

#### 候选退出点

两边 funding 时间轴并集大致为：

```text
11:00, 12:00, 13:00, 14:00, 15:00, 16:00, ...
```

#### 方向 A：Long Binance / Short Aster

到 `14:00` 时：

- Binance 已发生 4 次 funding；
- Aster 已发生 1 次 funding。

若 Binance 后续 3 次预测 funding 逐步均值回归，而 Aster 仍维持 1 次当前 funding，
则系统会按：

```text
carry = shortCarry(Aster) - longCarry(Binance)
```

来计算。由于这个例子里 Binance 为正 funding、Aster 为负 funding，
那么 `Long Binance / Short Aster` 通常会得到较差结果。

#### 方向 B：Long Aster / Short Binance

系统会自动再算一次：

```text
carry = shortCarry(Binance) - longCarry(Aster)
```

在这个例子里，这个方向往往会得到更高的净 carry，因此最终返回的会是：

```text
Long Aster / Short Binance
```

这个例子说明：

- 方向不是预先固定的；
- 同一个 symbol 在不同 venue 的不同结算周期，会直接影响最优方向与最优退出点；
- 只有按事件时间轴逐点计算，才能正确处理这种 `1h vs 4h` 的情况。

### 5.2.5 示例二：为什么“最优退出点”不一定是最近 funding 点

继续用上面的例子。

如果在 `11:00` 退出：

- Binance 只发生 1 次 funding；
- Aster 还没有发生 funding；
- 绝对 carry 可能不高，但窗口更短，因此 `hourlyEq` 可能更高。

如果在 `14:00` 退出：

- Binance 已累计 4 次 funding；
- Aster 已累计 1 次 funding；
- 绝对 carry 更大，但小时等价收益不一定更高。

所以系统在选择 projection 时，不是单纯盯着“最近 funding 时间”，而是：

1. 先比较绝对 carry；
2. 再比较小时等价；
3. 最后才比较退出时点早晚。

这也是为什么当前实现能够支持“多轮 funding 覆盖建仓成本”的场景。

### 5.2.6 示例三：Binance 1h 高 funding 特殊币不应被机械压扁

假设 Binance 某个 `1h` funding symbol 当前数据如下：

- `currentFunding = +0.20%`
- `historyMean = +0.15%`
- `historyStdDev = 0.02%`

若只采用线性 1h cap：

```text
8h cap = ±0.75%
1h cap = ±0.09375%
```

那么会出现一个明显不合理的情况：

- 当前 funding 已经高于线性 1h cap；
- 但模型却强行把未来路径压回 `±0.09375%`；
- 这会让多轮 funding carry 被系统性低估。

当前实现中，这类 symbol 会先参考基础 `venueCap`，
再结合 `currentFunding / historyMean / historyStdDev` 计算 `dynamicCap`。
如果 `dynamicCap` 更宽，则使用 adaptive clamp，
从而避免把真实存在的短周期 funding alpha 直接抹掉。

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

## 8. 自动开单与自动平仓的执行逻辑

这一节专门记录当前代码里“自动开单 / 自动平仓”是如何工作的，方便后续排障、review 和前端展示解释。

### 8.1 执行引擎的启动方式

执行引擎由 `ExecutionService.loop()` 周期性轮询驱动：

- 总开关：`cfg.enabled`
- 自动开仓：`cfg.execution.auto_entry`
- 自动平仓：`cfg.execution.auto_close`
- 轮询周期：`cfg.execution.loop_interval`

也就是说：

- 只要总开关关闭，执行引擎不会跑；
- 总开关开启后，每个 `loop_interval` 周期会分别检查“是否需要自动开仓”和“是否需要自动平仓”。

### 8.2 自动开单（auto entry）的前置条件

自动开单不是对所有机会直接下单，而是只针对已经进入 `execution_plans` 的计划执行。

在策略层，某个机会要先满足下面这些条件，才会被转成 `ExecutionPlan`：

1. `opp.EligibleForExecution = true`
2. `opp.Status == eligible`
3. `opp.NetExpectedPNL >= min_net_pnl`
4. 没有超过 entry cutoff
5. 当前盘口、最小下单量、最小名义价值、basis 阈值等检查都通过

只有通过这些检查，才会生成 `execution_plan`；否则仍然只是普通 opportunity，不会进入自动执行队列。

### 8.3 自动开单（auto entry）的 ready 判定

计划生成后，还要进一步判断“是不是现在就可以开”。

对每个 plan：

```text
entryAnchor        = requiredEntryByFundingTimeMs (若无则退化到 earliestFundingTimeMs)
entryWindowOpenMs  = entryAnchor - entry_lead_time
entryWindowCloseMs = entryAnchor - entry_cutoff_time

readyNow = now ∈ [entryWindowOpenMs, entryWindowCloseMs]
```

于是当前计划会处于三种主要状态之一：

- `ready`：现在就在允许入场窗口内，可以自动开单；
- `watching`：还没到可入场时间，只观察；
- `late`：已经错过入场截止时间，不应再开。

### 8.4 自动开单（runAutoOpen）的执行规则

执行引擎每轮自动开仓时，做的是：

1. 读取最新一批 execution plans；
2. 只处理：
   - `plan.ReadyNow == true`
   - `plan.Status == "ready"`
3. 查询这个 `plan_key` 是否已经存在 execution record；
4. 如果已经有 execution record，则跳过，避免重复开仓；
5. 否则调用 `openPlan(plan, trigger="auto")`。

这意味着当前自动开单逻辑本质上是：

> **“只对 ready 且从未执行过的 plan 自动尝试开仓一次”。**

### 8.5 自动开单时，实际下的是什么单

`openPlan()` 会先创建/复用 `ExecutionRecord`，然后区分两种模式：

#### A) dry-run 模式

当 `cfg.execution.enabled = false` 时：

- 不会向交易所真实发单；
- 只写入 execution record；
- 状态记为 `dry_run_opened`。

#### B) live 模式

当 `cfg.execution.enabled = true` 时：

- 调用 `placePlanOrders(phase="open")`
- 同时给 long / short 两条腿构建下单请求
- long 腿在 open 阶段使用 `BUY`
- short 腿在 open 阶段使用 `SELL`

具体委托类型由 `EntryMode` 决定：

- `maker`
  - 尽量挂盘口对手侧附近的 maker 价
  - TIF 通常为 `GTX` / `ALO`
- `taker`
  - 多数交易所直接用 `MARKET`
  - Hyperliquid 特殊处理为 `LIMIT + IOC + aggressivePrice`
- `mixed`
  - 当前实现近似为 `LIMIT + IOC + aggressivePrice`

### 8.6 自动开单后的状态记录

开单结束后，系统会汇总两腿结果并写入 `ExecutionRecord`：

- 两腿都成功：`opened`
- 部分成功：`open_partial_failed`
- 全部失败：`open_failed`
- dry-run：`dry_run_opened`

同时还会记录：

- `OpenedAtMs`
- `OpenOrderCount`
- `LastError`
- `AutoClose`
- `TargetCloseTimeMs`

这里的 `AutoClose` 与 `TargetCloseTimeMs` 非常重要，因为后续自动平仓就是靠这两个字段驱动的。

### 8.7 开单部分成功时的自动补救（hedge_close）

如果 open 阶段出现：

- 至少一条腿成功；
- 至少一条腿失败；

系统会立即触发补救逻辑：

1. 找出已成功提交的腿；
2. 为每条成功腿发起一个 `hedge_close`；
3. 这个补救单会：
   - 反转原始开仓方向；
   - `reduce-only = true`
   - `orderType = MARKET`
   - `timeInForce = IOC`

目的不是继续完成套利，而是：

> **尽快把已经打开的单边风险腿回滚掉。**

这一步不是“自动平仓计划”的一部分，而是“自动开仓失败后的紧急补救动作”。

### 8.8 自动平仓（auto close）的触发条件

自动平仓由 `runAutoClose()` 周期性执行。每轮会读取最新 execution records，并只处理满足以下条件的记录：

1. 当前 execution status 属于：
   - `opened`
   - `dry_run_opened`
   - `open_partial_failed`
2. `rec.AutoClose == true`
3. `rec.TargetCloseTimeMs > 0`
4. `now >= rec.TargetCloseTimeMs`

如果这些条件都满足，则会：

1. 用 `rec.PlanKey` 回查对应的 `ExecutionPlan`
2. 调用 `closePlan(plan, trigger="auto")`

也就是说，自动平仓的本质是：

> **“对已经开过的记录，在达到计划目标平仓时间后，按 plan 自动发起 close”。**

### 8.9 自动平仓时间是如何确定的

在 plan 生成阶段：

```text
exitAnchor        = projectedFundingTimeMs (若无则退化到 latestFundingTimeMs)
targetCloseTimeMs = exitAnchor + close_grace_period
```

所以自动平仓不是“到 funding 时间立刻平”，而是：

- 先以策略选出的最佳 funding 兑现点为核心；
- 再加一段 `close_grace_period` 作为缓冲；
- 到时再执行自动 close。

这个设计是为了避免：

- 刚到 funding 事件边界就立刻抢平；
- 因接口延迟 / 交易所时间边界 / 数据不同步导致过于激进。

### 8.10 自动平仓时，实际下的是什么单

`closePlan()` 的行为与 `openPlan()` 类似，也分 dry-run / live。

在 live 模式下调用 `placePlanOrders(phase="close")`，此时两腿方向会自动反过来：

- long 腿在 close 阶段使用 `SELL`
- short 腿在 close 阶段使用 `BUY`

同时 `buildTradeRequest()` 会自动设置：

- `ReduceOnly = true`
- 委托模式取 `plan.ExitMode`

因此自动平仓的语义是：

> **严格按已有仓位的反方向、reduce-only 去做退出，而不是重新建仓。**

### 8.11 自动平仓后的状态记录

平仓完成后，系统会更新 execution record：

- 两腿都成功：`closed`
- 部分成功：`close_partial_failed`
- 全部失败：`close_failed`
- dry-run：`dry_run_closed`

并更新：

- `ClosedAtMs`
- `CloseOrderCount`
- `LastError`

### 8.12 抽象成一句规则

当前自动执行链路可以抽象成：

```text
Opportunity
  -> pass eligibility checks
  -> build ExecutionPlan
  -> if now in entry window: auto open once
  -> if open partial failure: emergency hedge_close rollback
  -> after targetCloseTimeMs and auto_close enabled: auto close
```

### 8.13 一个完整例子

假设某个 plan 的时间参数如下：

- `requiredEntryByFundingTime = 13:00`
- `entry_lead_time = 30m`
- `entry_cutoff_time = 5m`
- `projectedFundingTime = 14:00`
- `close_grace_period = 2m`

则：

- `entryWindowOpen = 12:30`
- `entryWindowClose = 12:55`
- `targetCloseTime = 14:02`

系统行为会是：

1. `12:29` 前：状态 `watching`，不会自动开；
2. `12:30 ~ 12:55`：状态 `ready`，若还没有 execution record，则自动尝试开仓一次；
3. 若开仓中一腿成功、一腿失败：立即对成功腿发 `hedge_close`；
4. 若成功开仓且 `auto_close=true`：到 `14:02` 后自动发起 close；
5. 若是 dry-run 模式：整条链路只记状态，不会真实发单。

### 8.14 当前实现的边界与注意事项

当前自动开平仓逻辑有几个重要边界：

1. 自动开仓只避免“同一个 planKey 已有 execution record 时重复开”，
   但并不是完整的多状态执行状态机；
2. 自动平仓依赖 `execution_record + target_close_time_ms`，而不是订单成交回报 websocket；
3. `open_partial_failed` 会进入紧急回滚，但其后续记录仍可能进入 auto-close 扫描；
4. 若要继续提升生产可用性，最值得补的是：
   - 订单状态 websocket；
   - 更完整的双腿执行状态机；
   - 更细粒度的重复执行 / 幂等控制。

---

## 9. 当前模型边界

当前是“已知 nextFundingRate + interval 的事件外推”模型，优点是稳定、低成本、实时；
但它不预测未来 funding rate 变化（只对次数做时间轴累加）。

若后续要进一步提升，可扩展：
- 按历史序列做 funding term-structure 预测；
- 对不同交易所的 funding cap/floor 规则做更细建模；
- 引入资金利用率、保证金占用、借贷成本等资本效率约束。

---

## 10. 你接下来最值得继续加强的方向

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

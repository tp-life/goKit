# Polymarket 策略与风险控制审计说明

## 1. 文档目的

本文档用于说明当前 Go 版 Polymarket 交易系统的实际实现行为，供策略审计、风控审计、执行链路审计和运维审计使用。

本文档描述的是当前仓库内 Go 实现的真实生效逻辑，而不是理想设计稿。审计时应以代码为准，本文档用于帮助审计人员快速建立全局认知、定位关键路径和识别重点风险项。

## 2. 审计范围

本次说明覆盖以下模块：

- 市场发现与 Polymarket SDK：`internal/infrastructure/polymarket`
- 外部参考行情：`internal/infrastructure/marketdata`
- 自动交易主服务：`internal/application/service/polymarket_service.go`
- 账户同步：`internal/application/service/polymarket_account.go`
- 自动兑奖：`internal/application/service/polymarket_redeem.go`
- 状态模型：`internal/domain/entity/polymarket.go`
- 配置来源：`internal/infrastructure/polymarket/config.go`

不属于本文重点的内容：

- TUI 与 Web 展示层的 UI 细节
- 通用 GoKit 基础框架能力
- 第三方平台自身的风控、撮合、链上执行和结算机制

## 3. 系统概述

### 3.1 系统目标

系统面向 Polymarket 的短周期二元市场，核心目标是：

- 按配置自动发现当前活跃市场
- 获取链下与链上参考价格
- 在满足阈值条件时自动发起限价买单
- 在仓位建立后执行止盈或止损
- 同步账户持仓、历史与 PnL
- 对可兑奖仓位执行每日一次统一兑奖

### 3.2 运行形态

系统为单进程多协程架构，由 `PolymarketService` 统一调度。启动后主要后台任务包括：

- Chainlink RTDS 参考价订阅
- Binance WebSocket 订阅与 HTTP 兜底轮询
- 钱包 USDC 余额轮询
- 账户快照同步
- 每日自动兑奖任务
- 自动交易主循环

### 3.3 单一状态源

系统以 `PolymarketService` 内存状态为统一状态源：

- TUI 与 Web 不拥有独立策略逻辑
- 手动下单与 TUI 快捷下单最终复用同一服务层执行链路
- 本地状态通过 JSON 文件持久化，用于重启恢复

## 4. 数据源与优先级

### 4.1 市场元数据

市场元数据来自 Polymarket Gamma API，系统根据当前配置自动生成市场 slug，并优先查找“当前轮”，若当前轮不可用则回退到“下一轮”。

关键信息包括：

- 市场 slug
- 开始时间
- 结束时间
- UP / DOWN token id
- 市场快照概率

### 4.2 Polymarket 盘口数据

UP / DOWN 盘口通过 Polymarket CLOB WebSocket 订阅，系统维护：

- `upBid`
- `upAsk`
- `upPrice`
- `downBid`
- `downAsk`
- `downPrice`

买入侧默认优先使用 `ask`，卖出侧默认优先使用 `bid`，无法获得时再退回中间价或市场快照价。

### 4.3 Chainlink RTDS 价格

Chainlink RTDS 是当前策略的主参考价来源。系统通过 RTDS WebSocket 订阅配置资产的价格，例如：

- `btc/usd`
- `eth/usd`

策略计算中使用的参考价变量为：

`reference_price = Chainlink RTDS price`

### 4.4 Binance 价格

Binance 数据当前主要用于：

- 观测与比对
- 仪表盘展示
- 在 RTDS 之外提供辅助市场参考

当前自动入场逻辑不直接以 Binance 价格作为交易触发变量。Binance 不是当前版本的主信号源。

### 4.5 PTB 价格

系统通过 `crypto-price` 接口获取 PTB 相关价格，当前实现中在每个市场轮次内获取一次：

- 优先使用 `openPrice`
- 若无 `openPrice`，则退回 `closePrice`

策略中使用的 PTB 变量为：

`ptb_price = openPrice or closePrice`

### 4.6 账户数据

账户数据来自 Polymarket Data API，主要包括：

- 当前持仓
- 已关闭仓位
- 活动流水

系统将其聚合为：

- `wallet_positions`
- `wallet_history`
- `live_trades`
- `realized pnl`
- `unrealized pnl`
- `total pnl`

## 5. 市场选择逻辑

### 5.1 市场范围

当前实现面向单一配置资产、单一配置周期市场。例如：

- BTC 15 分钟
- ETH 15 分钟

系统并不是同时交易多个市场，而是始终围绕“当前活跃市场”工作。

### 5.2 市场切换行为

当检测到活跃市场 slug 变化时，系统会：

- 取消上一轮盘口订阅
- 清空上一轮 PTB 与盘口缓存
- 清空本地持仓状态
- 清空最近一次下单状态
- 清空止盈挂单状态
- 重新订阅新市场盘口

该设计避免跨市场状态污染，但也意味着本地状态对“市场轮次”高度敏感。

## 6. 自动入场策略

### 6.1 信号核心公式

当前自动交易的核心比较量为：

`diff = chainlink_price - ptb_price`

即以 Chainlink RTDS 参考价与 PTB 价格之间的差值作为方向性判断依据。

### 6.2 触发窗口

系统仅在市场剩余时间满足条件时才评估入场。默认提供 4 条规则：

| 条件 | 方向 | 剩余时间上限 | diff 阈值 | 概率下限 | 概率上限 |
| --- | --- | --- | --- | --- | --- |
| CONDITION_1 | UP | 120s | >= 30 | 0.80 | 0.92 |
| CONDITION_2 | DOWN | 120s | <= -30 | 0.80 | 0.92 |
| CONDITION_3 | UP | 60s | >= 50 | 0.80 | 0.92 |
| CONDITION_4 | DOWN | 60s | <= -50 | 0.80 | 0.92 |

注意：表格中的 `DOWN` 条件在实现中等价于 `diff <= -threshold`。

### 6.3 入场价格选择

当前逻辑：

- 买 `UP` 时，价格取 `upAsk -> upPrice -> activeMarket.UpPrice`
- 买 `DOWN` 时，价格取 `downAsk -> downPrice -> activeMarket.DownPrice`

即优先选择最接近实际可成交的卖一价格。

### 6.4 入场前置约束

自动入场前，必须同时满足以下条件：

- 当前存在活跃市场
- 市场剩余时间大于 0
- Chainlink 价格有效
- PTB 价格有效
- 对应方向的盘口价格有效
- 价格位于当前条件允许的概率区间内
- 盘口价格更新时间未超出 `MARKET_DATA_MAX_LAG_SEC`
- Chainlink 价格更新时间未超出 `MARKET_DATA_MAX_LAG_SEC`
- 当前没有待成交买单
- 当前没有本地持仓
- 当前方向在本轮市场内的重试次数未超过 `MAX_RETRY_PER_MARKET`

### 6.5 重试追价逻辑

如果当前方向在本市场已经尝试过买入，则会基于 `LastOrder` 限制重试：

- 最多重试 `MAX_RETRY_PER_MARKET`
- 若前次价格存在，则当前重试价格最高只允许提升到：

`min(0.995, last_price + BUY_RETRY_STEP)`

该逻辑用于避免在同一方向上无限抬价。

### 6.6 买入金额与份额

当前系统按固定金额下单：

`trade_amount = TRADE_AMOUNT`

买入份额计算为：

`size_shares = trade_amount / probability`

随后会按市场最小步长做归一化与向下取整。

## 7. 自动出场策略

### 7.1 仓位前提

系统仅维护一个本地主仓位：

- `state.Position`

在存在主仓位时，系统会持续检查止盈与止损条件。

### 7.2 止损公式

若入场价格为 `entry_price`，则止损概率为：

`stop_prob = entry_price * (1 - STOP_LOSS_PROB_PCT)`

默认 `STOP_LOSS_PROB_PCT = 0.15`，即概率相对回撤 15% 时触发止损。

### 7.3 止盈公式

系统先基于风险收益比计算理论止盈点：

`risk_abs = entry_price - stop_prob`

`tp_trigger = min(TAKE_PROFIT_CAP, entry_price + risk_abs * TAKE_PROFIT_RR)`

默认：

- `TAKE_PROFIT_RR = 1.0`
- `TAKE_PROFIT_CAP = 0.99`

这意味着默认止盈目标大体是“与止损风险等距”的 1R 位置，但不超过 0.99。

### 7.4 止盈触发后的行为

当满足以下条件时触发止盈卖单：

- 当前没有止盈挂单
- `current_prob >= tp_trigger`

若 `AUTO_TRADE=false`，仅发出提醒。

若 `AUTO_TRADE=true`，则发起 SELL 限价单，并启用止盈重试机制：

- 最多重试 `TAKE_PROFIT_RETRY_MAX`
- 每次失败将价格提高 `TAKE_PROFIT_RETRY_STEP`
- 最高不超过 `TAKE_PROFIT_CAP`

### 7.5 止损触发后的行为

当满足以下条件时触发止损卖单：

- `current_prob <= stop_prob`

系统会：

- 若存在止盈挂单，先尝试撤销止盈挂单
- 卖出价格优先取对应方向 `bestBid`
- 若无 `bestBid`，退回 `current_prob`
- 提交 SELL 限价单

## 8. 订单生命周期

### 8.1 自动买单生命周期

自动买单提交流程：

1. 依据市场 tick size、fee rate、neg-risk 元数据构造订单
2. 使用 EIP-712 进行订单签名
3. 通过 L2 API Key 鉴权调用 `/order`
4. 在本地记录 `PendingOrder`

### 8.2 买单超时处理

买单提交后，系统不会立即轮询状态，而是等待 `ORDER_TIMEOUT_SEC` 后再查询权威订单状态。

当前默认：

`ORDER_TIMEOUT_SEC = 8`

超时后：

- 若订单已完全成交，则转为本地持仓
- 若订单未完全成交，则尝试撤单

### 8.3 止盈挂单生命周期

止盈挂单会持续查询订单状态：

- 已成交：清空止盈单并清空本地仓位
- 被取消 / 拒绝 / 过期：清空止盈单，等待再次触发

### 8.4 手动下单与 TUI 下单

手动下单、TUI 热键下单和 TUI 交易页下单均复用服务层统一链路：

- 买入：进入 `PendingOrder`
- 卖出：进入 `TakeProfitOrder`

因此人工操作与自动操作共享同一订单执行和状态投影方式。

## 9. 风险控制设计

### 9.1 模式控制

若配置开启自动交易但未配置 `PRIVATE_KEY`，系统会自动降级为监控模式，不执行真实交易。

### 9.2 概率区间控制

每条自动交易条件都包含：

- 最低概率阈值
- 最高概率阈值

用于避免在过低概率或过高概率区间盲目追价。

### 9.3 单仓位 / 单挂单控制

系统在以下任一情形下禁止新开仓：

- 已存在待成交买单
- 已存在本地持仓

这使当前实现天然偏向单标的、单仓位、单方向。

### 9.4 市场数据时效控制

自动入场前会校验：

- 当前方向盘口更新时间
- Chainlink 价格更新时间

超过 `MARKET_DATA_MAX_LAG_SEC` 即不执行交易。

默认：

`MARKET_DATA_MAX_LAG_SEC = 1.2`

### 9.5 价格步长与下单归一化

下单前系统会读取并缓存：

- `tick-size`
- `neg-risk`
- `fee-rate`

随后按市场允许的最小步长处理：

- 概率价格
- 订单份额
- 金额字段

该步骤用于避免构造出服务端拒绝的非法订单参数。

### 9.6 API 鉴权控制

系统支持两种 L2 凭证模式：

- 通过 `PRIVATE_KEY` 自动创建或派生 Polymarket API Key
- 显式配置 `POLYMARKET_API_KEY / SECRET / PASSPHRASE`

请求签名控制包括：

- L1：EIP-712 钱包签名
- L2：HMAC 请求签名

### 9.7 代理控制

系统支持统一代理配置，覆盖：

- Polymarket REST
- Polymarket WebSocket
- RTDS WebSocket
- Binance WebSocket
- Binance HTTP
- Polygon RPC

### 9.8 兑奖控制

自动兑奖为显式受限能力，只有满足以下条件才会启用：

- `AUTO_REDEEM=true`
- 存在可用账户地址
- 已配置 `PRIVATE_KEY`
- 已配置 `POLYGON_RPC_URL`
- `FUNDER_ADDRESS` 与签名地址一致

最后一条是当前实现的关键限制，详见第 12 节。

## 10. 自动兑奖逻辑

### 10.1 调度方式

自动兑奖采用“每天统一跑一次”的调度方式，而不是高频扫描。

配置项包括：

- `AUTO_REDEEM_HOUR_LOCAL`
- `AUTO_REDEEM_MAX_RETRY`
- `AUTO_REDEEM_RETRY_DELAY_SEC`
- `AUTO_REDEEM_MAX_PER_RUN`
- `AUTO_REDEEM_RECEIPT_TIMEOUT_SEC`

### 10.2 可兑奖条件识别

系统通过 Data API `/positions` 扫描：

- `size > 0`
- `redeemable == true` 或 `mergeable == true`

再按 `conditionId` 去重，得到待兑奖条件列表。

### 10.3 执行方式

当前实现直接对 CTF 合约发送 `redeemPositions` 交易，并等待链上回执。

若交易发送成功但回执超时：

- 任务记录交易 hash
- 不再重复发送，避免重复 nonce 与重复交易

### 10.4 每日任务边界

单次兑奖任务会限制：

- 最多处理 `AUTO_REDEEM_MAX_PER_RUN` 个 condition
- 单个 condition 最多重试 `AUTO_REDEEM_MAX_RETRY` 次

## 11. 关键配置参数

下表列出当前策略与风控最关键的参数：

| 参数 | 含义 | 默认值 |
| --- | --- | --- |
| `TRADE_AMOUNT` | 单笔买入金额 | `5` |
| `ORDER_TIMEOUT_SEC` | 买单超时后查询状态的等待秒数 | `8` |
| `SLIPPAGE_THRESHOLD` | 预期滑点阈值 | `0.05` |
| `MAX_RETRY_PER_MARKET` | 单市场单方向最大重试次数 | `2` |
| `BUY_RETRY_STEP` | 自动买单重试追价步长 | `0.01` |
| `STOP_LOSS_PROB_PCT` | 止损相对回撤比例 | `0.15` |
| `TAKE_PROFIT_RR` | 止盈风险收益比 | `1.0` |
| `TAKE_PROFIT_CAP` | 止盈概率上限 | `0.99` |
| `TAKE_PROFIT_RETRY_STEP` | 止盈挂单失败后提价步长 | `0.005` |
| `TAKE_PROFIT_RETRY_MAX` | 止盈最大重试次数 | `3` |
| `MARKET_DATA_MAX_LAG_SEC` | 盘口与参考价最大允许时延 | `1.2` |
| `LOOP_INTERVAL_SEC` | 主循环周期 | `0.25` |
| `MARKET_META_REFRESH_SEC` | 市场元数据刷新周期 | `5` |
| `PRICE_REFRESH_SEC` | Binance HTTP 兜底轮询周期 | `5` |

## 12. 已知限制与审计重点

以下内容不应视为“正常设计的一部分”，而应视为专业审计时的重点核查项。

### 12.1 关键风险项 A：滑点保护当前实现未真正生效

代码中存在 `SLIPPAGE_THRESHOLD` 检查，但当前实现里：

- `currentPrice := price`
- 随后计算 `abs(currentPrice - price) / price`

因此结果恒等于 0，当前滑点校验实际上不会拦截任何订单。

审计建议：

- 应核查是否需要接入“决策时价格”和“下单时价格”的真实偏差比较
- 若希望滑点控制生效，应重新定义滑点测量基准

### 12.2 关键风险项 B：止损卖单提交后，本地仓位会立即清空

当前止损逻辑在提交 SELL 限价单后，无论是否真正成交，都会立即：

- 清空 `state.Position`
- 清空 `state.TakeProfitOrder`

即使 SELL 下单失败，也会清空本地仓位状态。

这会导致：

- 本地状态与真实账户状态失真
- 后续风控失去对真实仓位的持续管理
- 可能重复开仓或漏管已有仓位

审计建议：

- 应将仓位清理动作放在“订单确认成交”之后
- 或至少区分“提交成功待成交”和“实际成交”

### 12.3 关键风险项 C：部分成交未被完整建模

买单超时后，当前仅将“完全成交”视为已建仓：

- `sizeMatched >= originalSize` 才记为 Filled

若发生部分成交，当前处理倾向于：

- 视为未完全成交
- 触发撤单
- 不建立部分仓位状态

这会导致：

- 实际账户存在残余仓位，而本地未记录
- 风控、止盈止损与 PnL 管理出现偏差

### 12.4 关键风险项 D：止损与止盈均为限价单，不保证成交

当前出场均通过 SELL 限价单实现：

- 止盈使用目标价或重试提高后的目标价
- 止损使用 `bestBid` 或 `currentProb`

该设计意味着：

- 在流动性较薄或价格快速跳变时，不保证立即成交
- 极端情况下可能出现风险扩大

### 12.5 关键风险项 E：自动兑奖仅支持直接钱包，不支持代理钱包兑奖

当前 Go 版自动兑奖仅支持：

- 签名地址与 `FUNDER_ADDRESS` 一致

不支持：

- 代理钱包
- 分离 signer / funder 的兑奖链路
- Python 历史版本中可能存在的代理式兑奖路径

### 12.6 关键风险项 F：Binance 当前不参与自动决策

尽管系统接入了 Binance WebSocket 与 HTTP 兜底，但当前自动交易决策变量仍是：

- Chainlink RTDS
- PTB
- Polymarket 盘口概率

Binance 主要用于观测与展示，而非入场触发。

### 12.7 关键风险项 G：每日兑奖使用本地时间调度

自动兑奖调度基于本地时区和本地自然日。跨时区部署时应重点核查：

- 服务器时区配置
- `AUTO_REDEEM_HOUR_LOCAL` 含义
- 夏令时或运维切换导致的实际执行时间偏差

## 13. 审计建议清单

建议专业审计至少覆盖以下方向：

### 13.1 策略正确性审计

- `diff = chainlink - ptb` 是否符合策略原意
- UP / DOWN 条件是否与标的市场方向严格一致
- PTB 在当前市场内只获取一次是否符合预期
- 概率区间与剩余时间阈值是否具备统计支持

### 13.2 执行链路审计

- tick size、fee rate、neg-risk 元数据获取是否与 Polymarket 最新接口一致
- 订单签名、HMAC 签名和请求体序列化是否与官方接口完全兼容
- WebSocket 断线重连与状态恢复是否足够稳健

### 13.3 风控审计

- 止损和止盈均使用限价单是否满足实盘风险要求
- 部分成交处理是否需要补齐
- 本地仓位在失败场景下的状态一致性是否足够
- 单仓位设计是否符合策略容量假设

### 13.4 账户与兑奖审计

- 钱包地址、签名地址、代理钱包地址的边界是否明确
- 自动兑奖条件筛选是否会遗漏或误触发
- 兑奖回执等待与失败重试是否符合链上操作规范

### 13.5 运维与安全审计

- `PRIVATE_KEY`、`POLYMARKET_API_SECRET` 等敏感信息的加载与落盘策略
- 代理配置、RPC 可用性和超时策略
- 本地 JSON 状态文件的完整性与恢复逻辑

## 14. 结论

当前实现已经具备完整的可运行链路：

- 市场发现
- 参考价采集
- 自动买入
- 止盈止损
- 账户同步
- 每日统一兑奖

但从审计角度看，仍存在若干应被明确标注的实现风险，尤其是：

- 滑点阈值当前未实际生效
- 止损提交后过早清空本地仓位
- 部分成交未完整建模
- 自动兑奖仍不支持 signer / funder 分离场景

因此，当前版本适合进入“专业审计与定向修正”阶段，不建议在未完成上述关键风险整改前，直接将其视为完全无缺陷的生产级策略系统。

## 15. 审计代码入口索引

建议审计从以下函数切入：

- 配置加载：`internal/infrastructure/polymarket/config.go` -> `LoadConfig`
- Polymarket SDK 初始化：`internal/infrastructure/polymarket/client.go` -> `NewClient`
- L1 / L2 鉴权：`internal/infrastructure/polymarket/auth.go` -> `CreateOrDeriveAPIKey`、`buildL1Headers`、`buildL2Headers`
- 市场发现：`internal/infrastructure/polymarket/market.go` -> `GetActiveMarket`
- 市场盘口订阅：`internal/infrastructure/polymarket/market.go` -> `SubscribeMarket`
- 订单构造与提交：`internal/infrastructure/polymarket/orders.go` -> `PlaceLimitOrder`、`createSignedLimitOrder`
- 订单状态与撤单：`internal/infrastructure/polymarket/orders.go` -> `GetOrderStatus`、`CancelOrder`
- 外部行情：`internal/infrastructure/marketdata/client.go` -> `SubscribeRTDS`、`SubscribeBinanceBTC`、`GetCryptoPrice`
- 主服务启动：`internal/application/service/polymarket_service.go` -> `Start`
- 主循环：`internal/application/service/polymarket_service.go` -> `runEngine`
- 自动入场决策：`internal/application/service/polymarket_service.go` -> `buildAutoBuyPlanLocked`、`evaluateAutoTrade`
- 挂单处理：`internal/application/service/polymarket_service.go` -> `processPendingBuy`、`processTakeProfitOrder`
- 仓位管理：`internal/application/service/polymarket_service.go` -> `managePosition`
- 账户同步：`internal/application/service/polymarket_account.go` -> `syncAccountSnapshot`
- 自动兑奖：`internal/application/service/polymarket_redeem.go` -> `runAutoRedeemer`、`executeAutoRedeem`、`redeemConditionWithRetry`

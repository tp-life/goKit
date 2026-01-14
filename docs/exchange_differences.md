# 交易所差异处理说明

## 概述

不同交易所在币对表示、数据格式、费率周期等方面存在显著差异。本文档说明如何统一处理这些差异。

## 交易所差异对比

### 1. Binance

**币对格式**:
- 使用交易对符号：`BTCUSDC`, `ETHUSDC`
- 格式：`{BASE}{QUOTE}`

**资金费率**:
- 周期：8 小时
- 直接使用，无需转换

**WebSocket**:
- 现货：`wss://stream.binance.com:9443/ws/{symbol}@ticker`
- 合约：`wss://fstream.binance.com/ws/!markPrice@arr`
- 消息格式：JSON 对象或数组

**REST API**:
- 资金费率：`GET /fapi/v1/premiumIndex?symbol={symbol}`

### 2. Lighter

**币对格式**:
- 使用 Market ID：`0`, `1`, `2` 等（数字）
- 需要映射表：`BTCUSDC -> 1`, `ETHUSDC -> 0`

**资金费率**:
- 周期：1 小时
- **需要转换**：`rate8H = rate * 8`

**WebSocket**:
- 地址：`wss://mainnet.zklighter.elliot.ai/stream`
- 订阅：`{"type": "subscribe", "channel": "market_stats/all"}`
- 消息格式：
  ```json
  {
    "type": "update/market_stats",
    "market_stats": {
      "0": {
        "market_id": 0,
        "mark_price": "3152.27",
        "index_price": "3153.42",
        "last_trade_price": "3152.32",
        "current_funding_rate": "0.0012"
      }
    }
  }
  ```

**REST API**:
- 资金费率：`GET /api/v1/funding-rates?market_id={market_id}`

### 3. Hyperliquid

**币对格式**:
- 使用简化符号：`BTC`, `ETH`（无交易对后缀）
- 需要映射表：`BTCUSDC -> BTC`, `ETHUSDC -> ETH`

**资金费率**:
- 周期：8 小时（需要确认）
- 直接使用，无需转换

**WebSocket**:
- 地址：`wss://api.hyperliquid.xyz/ws`
- 订阅格式：需要根据实际 API 文档调整

**REST API**:
- 使用 POST 请求
- 资金费率：`POST /info` with `{"type": "fundingHistory", "coin": "BTC"}`

## 统一处理方案

### 1. 符号映射器 (`SymbolMapper`)

创建了统一的符号映射器来处理不同交易所的币对差异：

```go
type SymbolMapper struct {
    binanceMap         map[string]string  // 统一符号 -> Binance 符号
    lighterMap         map[string]int      // 统一符号 -> Lighter Market ID
    hyperliquidMap     map[string]string  // 统一符号 -> Hyperliquid 符号
    // 反向映射...
}
```

**使用示例**:
```go
mapper := exchange.NewSymbolMapper()
mapper.RegisterSymbol("BTCUSDC", "BTCUSDC", 1, "BTC")

// 转换
binanceSymbol, _ := mapper.ToBinance("BTCUSDC")      // "BTCUSDC"
lighterMarketID, _ := mapper.ToLighter("BTCUSDC")     // 1
hyperliquidSymbol, _ := mapper.ToHyperliquid("BTCUSDC") // "BTC"
```

### 2. 资金费率转换

在 `LighterClient` 中实现费率转换：

```go
// Lighter 是 1 小时周期，转换为 8 小时
rate8H := math.Mul(rate, 8.0)
```

### 3. 数据格式适配

每个交易所客户端负责：
1. 解析自己的消息格式
2. 转换为统一的 `MarketData` 实体
3. 处理符号映射

## 实现细节

### Binance 客户端

**特点**:
- 需要连接两个 WebSocket（现货和合约）
- 消息格式相对标准
- 费率已经是 8 小时周期

**实现位置**: `internal/infrastructure/exchange/binance/client.go`

### Lighter 客户端

**特点**:
- 使用 Market ID 而非符号
- 需要订阅 `market_stats/all` 获取所有市场数据
- **关键**：费率需要从 1 小时转换为 8 小时

**实现位置**: `internal/infrastructure/exchange/lighter/client.go`

**费率转换**:
```go
// 在 GetFundingRate 中
rate, _ := strconv.ParseFloat(rateStr, 64)
rate8H := math.Mul(rate, 8.0)  // 1小时 -> 8小时
```

### Hyperliquid 客户端

**特点**:
- 使用简化的符号（无 USDC 后缀）
- API 可能需要 POST 请求
- 消息格式需要根据实际 API 文档调整

**实现位置**: `internal/infrastructure/exchange/hyperliquid/client.go`

**注意事项**:
- 部分功能标记为 `TODO`，需要根据实际 API 文档完善
- WebSocket 订阅格式需要确认

## 配置示例

```yaml
arbitrage:
  symbols:
    - symbol: "BTCUSDC"              # 统一符号
      binance_market: "BTCUSDC"      # Binance 符号
      lighter_market_id: 1           # Lighter Market ID
      hyperliquid_market: "BTC"      # Hyperliquid 符号
      enabled: true
    - symbol: "ETHUSDC"
      binance_market: "ETHUSDC"
      lighter_market_id: 0
      hyperliquid_market: "ETH"
      enabled: true
```

## 数据流

```
统一符号 (BTCUSDC)
    ↓
符号映射器
    ↓
┌──────────┬──────────┬──────────────┐
│ Binance  │ Lighter  │ Hyperliquid  │
│ BTCUSDC  │ Market 1 │ BTC          │
└──────────┴──────────┴──────────────┘
    ↓           ↓            ↓
WebSocket/REST API 调用
    ↓           ↓            ↓
数据解析和转换
    ↓           ↓            ↓
统一实体 (MarketData/FundingRate)
    ↓
对比服务
```

## 注意事项

1. **Lighter 费率转换**：必须将 1 小时费率乘以 8
2. **Market ID 映射**：Lighter 使用数字 ID，需要维护映射表
3. **符号格式**：Hyperliquid 使用简化符号，需要映射
4. **API 差异**：Hyperliquid 可能使用 POST 而非 GET
5. **消息格式**：每个交易所的 WebSocket 消息格式不同，需要分别解析

## 扩展新交易所

添加新交易所时：

1. 创建客户端实现 `ExchangeClient` 接口
2. 在 `SymbolMapper` 中添加映射方法
3. 在配置中添加符号映射
4. 在主程序中注册客户端
5. 处理该交易所的特殊差异（费率周期、符号格式等）

## 测试建议

1. **符号映射测试**：验证所有交易所的符号转换正确
2. **费率转换测试**：验证 Lighter 的费率转换（1小时 -> 8小时）
3. **数据解析测试**：验证每个交易所的消息解析正确
4. **集成测试**：验证多交易所对比功能正常

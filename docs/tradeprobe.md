# Trade Probe

`cmd/tradeprobe` 是一个手动交易探针，用来复用仓库里的真实交易适配器做最小闭环验证：

- 检查当前配置和凭证是否足以启用某个交易所
- 读取账户快照 / 订单状态 / 持仓
- 显式确认后发送一笔真实测试单

另外，`preflight` 可以做专项鉴权诊断。

它默认不会下单。只有加上 `--confirm` 才会执行 `place`。

如果还希望下单后自动轮询订单状态，可以额外加：

- `--wait-status`
- `--status-timeout 30s`
- `--status-interval 1500ms`

## Inspect

先看当前哪些交易所在运行时可用：

```bash
go run ./cmd/tradeprobe --action inspect
```

只看某一个交易所：

```bash
go run ./cmd/tradeprobe --action inspect --exchange aster
```

## Binance Preflight

Binance 专项预检查会同时测试：

- 当前推断出的鉴权模式（`legacy_hmac` / `rsa`）
- 出口 IP
- Spot 签名
- Futures 只读权限
- 可能原因和修复建议

```bash
go run ./cmd/tradeprobe --action preflight --exchange binance
```

如果你要测试 Binance RSA key，可以在配置里补：

- `exchanges.binance.auth.private_key_env`
- `exchanges.binance.adapter_options.trade_auth_mode: "rsa"`

## Binance

预览一笔市价测试单：

```bash
go run ./cmd/tradeprobe \
  --action place \
  --exchange binance \
  --symbol BTCUSDT \
  --side BUY \
  --qty 0.001
```

确认真实发送：

```bash
go run ./cmd/tradeprobe \
  --action place \
  --exchange binance \
  --symbol BTCUSDT \
  --side BUY \
  --qty 0.001 \
  --confirm
```

确认发送后自动轮询状态：

```bash
go run ./cmd/tradeprobe \
  --action place \
  --exchange binance \
  --symbol BTCUSDT \
  --side BUY \
  --qty 0.001 \
  --confirm \
  --wait-status
```

## Aster

当前仓库会根据凭证自动判断：

- `ASTER_API_KEY` + `ASTER_API_SECRET` -> `legacy_hmac`
- `ASTER_ACCOUNT_ADDRESS` + `ASTER_SIGNER_PRIVATE_KEY` -> `v3_signer`

预览一笔测试单：

```bash
go run ./cmd/tradeprobe \
  --action place \
  --exchange aster \
  --symbol BTCUSDT \
  --side BUY \
  --qty 0.001
```

确认真实发送：

```bash
go run ./cmd/tradeprobe \
  --action place \
  --exchange aster \
  --symbol BTCUSDT \
  --side BUY \
  --qty 0.001 \
  --confirm
```

确认发送后自动轮询状态：

```bash
go run ./cmd/tradeprobe \
  --action place \
  --exchange aster \
  --symbol BTCUSDT \
  --side BUY \
  --qty 0.001 \
  --confirm \
  --wait-status
```

## Bybit

如果 `configs/config.yaml` 里还是 `enabled: false`，可以只对本次 probe 临时开启：

```bash
go run ./cmd/tradeprobe \
  --action place \
  --exchange bybit \
  --force-enable \
  --symbol BTCUSDT \
  --side BUY \
  --qty 0.001 \
  --type MARKET \
  --confirm
```

## Hyperliquid

Hyperliquid 现在的适配器要求：

- `--asset-id`
- `--price`

也就是更接近 `LIMIT + IOC` 的测试方式：

```bash
go run ./cmd/tradeprobe \
  --action place \
  --exchange hyperliquid \
  --force-enable \
  --symbol BTC \
  --canonical-symbol BTC \
  --asset-id 0 \
  --side BUY \
  --qty 0.001 \
  --type LIMIT \
  --tif IOC \
  --price 60000 \
  --confirm
```

## Query

查账户：

```bash
go run ./cmd/tradeprobe --action account --exchange aster
```

查持仓：

```bash
go run ./cmd/tradeprobe --action position --exchange binance --symbol BTCUSDT
```

查订单状态：

```bash
go run ./cmd/tradeprobe \
  --action status \
  --exchange aster \
  --symbol BTCUSDT \
  --client-order-id probe-aster-1234567890
```

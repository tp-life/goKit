# Exchange Adapter Audit 2026-03-30

## Scope

Reviewed the currently supported exchange families and their trading-critical paths:

- Binance-like: Binance / Aster
- Bybit V5
- Hyperliquid

Focus areas:

- order placement
- order lookup
- cancel path
- position query
- account query
- private order event stream

## Findings

## Status update

The following items have now been fixed in code:

- Hyperliquid `cloid` is now generated/validated as `0x`-prefixed 16-byte hex.
- Hyperliquid order placement now inspects business-level error payloads instead of trusting HTTP 200 alone.
- Hyperliquid `orderStatus` lookup now sends client order id via the documented `oid` field.
- Hyperliquid account / position / order status queries now prefer `vaultAddress` when trading on behalf of a vault/subaccount.
- Hyperliquid now supports cancel by venue order id and cancel by cloid.
- Bybit V5 now supports cancel by `orderId` / `orderLinkId`.
- Binance-like 503 order responses are now surfaced as “unknown execution outcome”, and the execution engine re-runs reconciliation instead of treating them as definite failures.
- Binance / Aster now refuse to place orders when the account is detected in hedge mode; the shared adapter only proceeds in one-way mode.
- Aster private order stream support is now enabled when an API key is present, reusing the documented `listenKey + ORDER_TRADE_UPDATE` flow shared with Binance-like venues.

Still open:

- The shared Binance-like adapter still does not implement full hedge-mode semantics (`positionSide` routing, dual-side position reconciliation). It now fails closed in hedge mode instead of trading.

### 1. High: Hyperliquid `clientOrderId` format is incompatible with the official `cloid` contract

- Official docs: Hyperliquid `cloid` is optional, but when sent it must be a 128-bit hex string with `0x` prefix.
  - Source: https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint
- Current implementation forwards the generic system `ClientOrderID` directly into Hyperliquid order payloads.
  - Code: [hyperliquid_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/hyperliquid_trade.go#L79)
- The generic system id format is optimized for Binance-like / Bybit 36-char ids, not Hyperliquid hex `cloid`.
  - Code: [execution_service.go](/Users/tp/work/person/goKit/internal/application/service/execution_service.go#L1479)

Impact:

- Hyperliquid orders can be rejected whenever we send `ClientOrderID`.
- Even after the Aster 36-char fix, the global id format is still wrong for Hyperliquid.

Recommended fix:

- Make `ClientOrderID` generation exchange-aware.
- For Hyperliquid, generate a 16-byte hex `cloid` with `0x` prefix.

### 2. High: Hyperliquid order placement treats business-level 200 error responses as success

- Official docs explicitly show that the exchange endpoint may return `200 OK Error Response`.
  - Source: https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint
- Current transport only checks HTTP status and then unmarshals JSON.
  - Code: [hyperliquid_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/hyperliquid_trade.go#L400)
- `PlaceOrder` then converts the response into a `TradeOrderResult` without validating whether the business payload is actually an error.
  - Code: [hyperliquid_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/hyperliquid_trade.go#L79)

Impact:

- A rejected Hyperliquid order can be misclassified as `submitted`.
- Execution flow may proceed as if the leg exists, which is dangerous for hedge logic and exposure accounting.

Recommended fix:

- Add explicit Hyperliquid business-response validation before returning success.
- Treat exchange-level rejection payloads as errors, not as successful order submissions.

### 3. High: Hyperliquid status lookup appears to use the wrong request shape for client-order-id lookups

- Official docs say the `orderStatus` info call takes `oid`, and that field may be either:
  - a numeric order id, or
  - a 16-byte hex client order id.
  - Source: https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint
- Current code sends `payload["cloid"] = req.ClientOrderID` instead of passing the client id via `oid`.
  - Code: [hyperliquid_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/hyperliquid_trade.go#L178)

Impact:

- Client-id based order reconciliation may fail even if the order exists.
- This becomes especially risky when venue order id is unavailable or we are recovering after partial failures.

Recommended fix:

- Re-check Hyperliquid’s current request schema against SDK examples.
- Align `GetOrderStatus` to the official request body shape.

### 4. High: Bybit and Hyperliquid lack cancel support, but the execution engine relies on cancel capability for maker safety

- Official docs provide cancel endpoints:
  - Bybit cancel order: https://bybit-exchange.github.io/docs/v5/order/cancel-order
  - Hyperliquid cancel / cancelByCloid: https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint
- Execution engine tries to cancel still-open maker orders after reconcile when the adapter implements `TradeOrderCanceler`.
  - Interface: [interfaces.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/interfaces.go#L154)
  - Engine path: [execution_service.go](/Users/tp/work/person/goKit/internal/application/service/execution_service.go#L1103)
- Binance-like implements `CancelOrder`, but Bybit and Hyperliquid currently do not.
  - Code: [bybit_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/bybit_trade.go)
  - Code: [hyperliquid_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/hyperliquid_trade.go)

Impact:

- On Bybit / Hyperliquid, maker orders can remain resting after the engine has already decided they should no longer stay live.
- This can produce late fills after entry windows or during cleanup paths.

Recommended fix:

- Implement `CancelOrder` for Bybit V5.
- Implement `CancelOrder` and `CancelByCloid` support for Hyperliquid.
- Consider disabling maker mode on venues lacking cancel support until fixed.

### 5. High: Binance-like 503 “execution unknown” is currently treated as a hard failure

- Aster docs explicitly state that HTTP 503 means the API may have sent the message successfully but timed out waiting for a response, so execution status is unknown and should not be treated as a clean failure.
  - Source: https://docs.asterdex.com/product/aster-perpetuals/api/api-documentation
- Current Binance-like adapter treats any HTTP status `>= 300` as an immediate error.
  - Code: [cex_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/cex_trade.go#L363)
- Execution flow turns non-timeout order placement errors into `ERROR` records without a forced exchange-side reconciliation step.
  - Code: [execution_service.go](/Users/tp/work/person/goKit/internal/application/service/execution_service.go#L867)

Impact:

- A leg can actually exist on the exchange while local state classifies it as failed.
- This can cascade into bad rollback / hedge decisions and create exactly the kind of loss-inducing mismatch we want to avoid.

Recommended fix:

- Treat Binance-like 503 as “unknown outcome”, not definitive failure.
- Force `GetOrderStatus` reconciliation by client order id / venue order id before deciding the leg failed.

### 6. Medium: Binance-like adapter does not support hedge-mode requirements and does not validate account mode

- Binance docs say:
  - `positionSide` must be sent in hedge mode
  - `reduceOnly` cannot be sent in hedge mode
  - Source: https://developers.binance.com/docs/derivatives/usds-margined-futures/trade/rest-api/New-Order
- Current Binance-like order builder never sends `positionSide`, and always sends `reduceOnly=true` for close/recovery legs.
  - Code: [cex_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/cex_trade.go#L172)

Impact:

- If a Binance-like account is configured in hedge mode, order opens/closes can be rejected.
- This is confirmed for Binance and is a likely parallel risk for the shared Binance-like adapter family.

Recommended fix:

- Either enforce one-way mode in preflight/runtime checks, or
- add explicit position-mode support and send correct `positionSide`.

### 7. Medium: Aster documents order updates, but private order stream support is disabled in the shared Binance-like adapter

- Aster docs include `ORDER_TRADE_UPDATE` user stream behavior.
  - Source: https://docs.asterdex.com/product/aster-perpetuals/api/api-documentation
- Current shared CEX adapter intentionally enables private order stream only for Binance.
  - Code: [cex_order_event_stream.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/cex_order_event_stream.go#L44)

Impact:

- Aster loses the same late-fill / delayed-status recovery path that Binance currently has.
- This is not necessarily a protocol bug, but it is a resilience gap in live trading.

Recommended fix:

- Validate Aster listenKey + user stream behavior against current docs.
- If compatible, enable Aster private order stream support.

### 8. Medium: Hyperliquid vault/subaccount reads ignore `vaultAddress`

- Hyperliquid docs say that when acting on behalf of a subaccount or vault, the actual account address must be used for account data queries, and `vaultAddress` must be set for trade actions.
  - Exchange endpoint: https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/exchange-endpoint
  - Info endpoint: https://hyperliquid.gitbook.io/hyperliquid-docs/for-developers/api/info-endpoint
- Current code correctly sends `vaultAddress` during order placement, but still queries account/position/order status using `accountAddress`.
  - Code: [hyperliquid_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/hyperliquid_trade.go#L178)
  - Code: [hyperliquid_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/hyperliquid_trade.go#L216)
  - Code: [hyperliquid_trade.go](/Users/tp/work/person/goKit/internal/infrastructure/exchange/hyperliquid_trade.go#L249)

Impact:

- When using vault/subaccount mode, balances, positions, and order reconciliation can target the wrong account.

Recommended fix:

- Introduce a helper that resolves the effective query address:
  - `vaultAddress` when trading on behalf of a vault/subaccount
  - otherwise `accountAddress`

## Suggested remediation order

1. Freeze or severely limit Hyperliquid live trading until findings 1-4 are fixed.
2. Fix Binance-like 503 reconciliation semantics before continuing production auto-trading on Aster/Binance-like venues.
3. Decide whether the system officially supports hedge-mode accounts. If not, enforce one-way mode in preflight and runtime.
4. Add missing cancel support for Bybit and Hyperliquid before enabling maker mode there.
5. Add Aster private order stream only after protocol-level verification.

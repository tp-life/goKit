package exchange

import "math"

// closeSideAndQuantity 把“账户当前净仓位”和“本次请求希望关闭的数量”合成一笔 reduce-only 平仓请求。
//
// 这里刻意不再简单地“看到当前仓位有多少，就整仓平多少”，原因是：
//  1. execution plan 已经明确知道这条计划自己的 long/short qty；
//  2. 如果 adapter 在 ClosePosition 里无视 req.Quantity，直接按账户净仓整仓 flatten，
//     就会把“关闭这条 plan”错误地扩大成“清空这个 symbol 在该账户上的全部仓位”；
//  3. 对于未来同账户多策略、手工残仓、恢复流程等场景，这是非常危险的。
//
// 因此这里遵循一个更保守的规则：
// - 平仓方向永远以“当前真实仓位的方向”推导，避免方向写反后继续加仓；
// - 平仓数量默认等于当前绝对仓位；
// - 如果调用方显式给了 requestedQty，则把这次关闭量 cap 到 `min(abs(position), abs(requestedQty))`。
//
// 这还不是完整的“plan-owned inventory accounting”，但至少先修掉了最危险的
// “一条 plan 的 close 把整仓都关掉”的问题。
func closeSideAndQuantity(positionQty, requestedQty float64) (string, float64, bool) {
	if positionQty == 0 {
		return "", 0, false
	}

	side := "SELL"
	absPositionQty := math.Abs(positionQty)
	if positionQty < 0 {
		side = "BUY"
	}

	absRequestedQty := math.Abs(requestedQty)
	closeQty := absPositionQty
	if absRequestedQty > 0 && absRequestedQty < closeQty {
		closeQty = absRequestedQty
	}
	return side, closeQty, closeQty > 0
}

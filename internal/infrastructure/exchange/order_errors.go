package exchange

import "fmt"

// UnknownExecutionOutcomeError 表示“请求已发出，但我们还不能可靠断定订单最终是否落地”。
//
// 典型场景：
// - Binance-like venue 返回 503，官方文档明确说执行状态是 UNKNOWN；
// - 网络抖动导致客户端只知道“没拿到最终响应”，但交易所不一定真的没接单。
//
// 这类错误和普通参数错误/权限错误不同，执行层不应该直接把它当成“确定失败”，
// 而应该尽快走一轮订单状态或仓位对账。
type UnknownExecutionOutcomeError struct {
	Exchange   string
	Operation  string
	StatusCode int
	Body       string
}

func (e *UnknownExecutionOutcomeError) Error() string {
	if e == nil {
		return "unknown execution outcome"
	}
	if e.StatusCode > 0 {
		return fmt.Sprintf("%s %s returned unknown execution outcome status=%d body=%s", e.Exchange, e.Operation, e.StatusCode, e.Body)
	}
	return fmt.Sprintf("%s %s returned unknown execution outcome", e.Exchange, e.Operation)
}

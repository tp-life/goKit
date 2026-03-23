package service

import (
	"fmt"
	"math"
	"strings"

	"goKit/internal/domain/entity"
)

const (
	orderCompletionRatio = 0.995
	orderQtyEpsilon      = 1e-9
)

func requiredSatisfiedQty(requestedQty float64) float64 {
	if requestedQty <= 0 {
		return 0
	}
	return requestedQty * orderCompletionRatio
}

func qtyTolerance(requestedQty float64) float64 {
	if requestedQty <= 0 {
		return orderQtyEpsilon
	}
	return math.Max(requestedQty*(1-orderCompletionRatio), orderQtyEpsilon)
}

func hasMeaningfulExecutedQty(executedQty, requestedQty float64) bool {
	return executedQty > qtyTolerance(requestedQty)
}

func isOrderFullySatisfied(rec entity.OrderRecord) bool {
	switch strings.ToUpper(strings.TrimSpace(rec.Status)) {
	case "FILLED":
		return true
	case "NO_POSITION":
		return rec.ReduceOnly
	}
	if rec.RequestedQty <= 0 || isFailedOrderStatus(rec.Status) {
		return false
	}
	return rec.ExecutedQty >= requiredSatisfiedQty(rec.RequestedQty)
}

func orderHasOpenExposure(rec entity.OrderRecord) bool {
	return !rec.ReduceOnly && hasMeaningfulExecutedQty(rec.ExecutedQty, rec.RequestedQty)
}

func openedPositionSatisfiesRequest(positionQty, requestedQty float64) bool {
	if requestedQty <= 0 {
		return math.Abs(positionQty) > orderQtyEpsilon
	}
	return math.Abs(positionQty) >= requiredSatisfiedQty(requestedQty)
}

func closedPositionSatisfiesRequest(positionQty, requestedQty float64) bool {
	return math.Abs(positionQty) <= qtyTolerance(requestedQty)
}

func describeOrderAttention(rec entity.OrderRecord) string {
	status := strings.ToUpper(strings.TrimSpace(rec.Status))
	if status == "" {
		status = "UNKNOWN"
	}
	return fmt.Sprintf(
		"%s order not fully satisfied: status=%s executed=%.8f requested=%.8f",
		pickNonEmpty(rec.Exchange, "unknown_exchange"),
		status,
		rec.ExecutedQty,
		rec.RequestedQty,
	)
}

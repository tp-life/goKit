package service

import (
	"context"
	"strings"
	"time"

	"goKit/internal/domain/entity"
)

func (s *ExecutionService) inspectSameExchangeLiveRisk(ctx context.Context, rec entity.ExecutionRecord, plan *entity.ExecutionPlan, longLeg, shortLeg LivePositionLegInspection) SameExchangeLiveRiskInspection {
	if s == nil || plan == nil {
		return SameExchangeLiveRiskInspection{}
	}
	arbitrageMode := normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange))
	if arbitrageMode != ArbitrageModeSameExchangeSpotPerp {
		return SameExchangeLiveRiskInspection{}
	}

	risk := SameExchangeLiveRiskInspection{
		Enabled:                   true,
		PriceShockAllowed:         true,
		PriceShockThresholdRatio:  s.cfg.SameExchangeMax1hPriceShockRatio,
		MinLiqDistanceRatio:       s.cfg.SameExchangeMinLiqDistanceRatio,
		ReduceLiqDistanceRatio:    s.cfg.SameExchangeReduceLiqDistanceRatio,
		EmergencyLiqDistanceRatio: s.cfg.SameExchangeEmergencyLiqDistanceRatio,
	}

	perpLeg, perpExchange, perpFunding, ok := s.sameExchangePerpRiskLeg(plan, longLeg, shortLeg)
	if ok {
		if perpLeg.Position.LiquidationPrice > 0 {
			risk.LiquidationPrice = perpLeg.Position.LiquidationPrice
			risk.LiquidationDistanceRatio = positionLiquidationDistanceRatio(perpLeg.Position)
		}
		assessment := assessSameExchangePriceRisk(ctx, s.cfg, s.marketRepo, arbitrageMode, perpExchange, plan.Symbol, perpFunding, time.Now().UTC())
		risk.PriceShockAllowed = assessment.Allowed
		risk.PriceShockReason = assessment.Reason
		risk.PriceShockCurrentMarkPrice = assessment.CurrentMarkPrice
		risk.PriceShockBaselineMarkPrice = assessment.BaselineMarkPrice
		risk.PriceShockRatio = assessment.PriceShockRatio
	}

	s.applyProtectiveOrderRisk(ctx, rec.PlanKey, &risk)
	return risk
}

func (s *ExecutionService) sameExchangePerpRiskLeg(plan *entity.ExecutionPlan, longLeg, shortLeg LivePositionLegInspection) (LivePositionLegInspection, string, entity.FundingSnapshot, bool) {
	if s == nil || plan == nil {
		return LivePositionLegInspection{}, "", entity.FundingSnapshot{}, false
	}
	perpMeta, _, perpFunding, _, _, ok := s.sameExchangePerpLegContext(plan)
	if !ok {
		return LivePositionLegInspection{}, "", entity.FundingSnapshot{}, false
	}
	if strings.EqualFold(perpMeta.Exchange, longLeg.Exchange) && strings.EqualFold(perpMeta.VenueSymbol, longLeg.VenueSymbol) {
		return longLeg, perpMeta.Exchange, perpFunding, true
	}
	if strings.EqualFold(perpMeta.Exchange, shortLeg.Exchange) && strings.EqualFold(perpMeta.VenueSymbol, shortLeg.VenueSymbol) {
		return shortLeg, perpMeta.Exchange, perpFunding, true
	}
	if strings.EqualFold(perpMeta.Exchange, shortLeg.Exchange) {
		return shortLeg, perpMeta.Exchange, perpFunding, true
	}
	if strings.EqualFold(perpMeta.Exchange, longLeg.Exchange) {
		return longLeg, perpMeta.Exchange, perpFunding, true
	}
	return LivePositionLegInspection{}, "", entity.FundingSnapshot{}, false
}

func (s *ExecutionService) applyProtectiveOrderRisk(ctx context.Context, planKey string, risk *SameExchangeLiveRiskInspection) {
	if s == nil || risk == nil || strings.TrimSpace(planKey) == "" || s.orderRepo == nil {
		return
	}
	orders, err := s.orderRepo.ListByPlanKey(ctx, planKey)
	if err != nil {
		return
	}
	order, ok := latestSameExchangeProtectiveOrder(orders)
	if !ok {
		return
	}
	risk.ProtectiveOrderStatus = order.Status
	risk.ProtectiveOrderStopPrice = order.RequestedPrice
	risk.ProtectiveOrderExchange = order.Exchange
	risk.ProtectiveOrderVenueSymbol = order.VenueSymbol
	risk.ProtectiveOrderClientOrderID = order.ClientOrderID
	risk.ProtectiveOrderUpdatedAtMs = order.UpdatedAt.UnixMilli()
	risk.ProtectiveOrderErrorMessage = order.ErrorMessage
	risk.ProtectiveOrderArmed = sameExchangeProtectiveOrderActive(order.Status)
}

func latestSameExchangeProtectiveOrder(items []entity.OrderRecord) (entity.OrderRecord, bool) {
	var best entity.OrderRecord
	found := false
	for _, order := range items {
		if normalizeExecutionStatus(order.Phase) != sameExchangeProtectPhase {
			continue
		}
		if !found || order.UpdatedAt.After(best.UpdatedAt) || (order.UpdatedAt.Equal(best.UpdatedAt) && order.ID > best.ID) {
			best = order
			found = true
		}
	}
	return best, found
}

func sameExchangeProtectiveOrderActive(status string) bool {
	switch normalizeExecutionStatus(status) {
	case "", "error", "filled", "canceled", "cancelled", "expired", "rejected":
		return false
	default:
		return true
	}
}

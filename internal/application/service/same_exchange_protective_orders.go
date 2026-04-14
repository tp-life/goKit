package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

const (
	sameExchangeProtectPhase   = "protect"
	sameExchangeProtectLegRole = "perp_protect"
)

func (s *ExecutionService) armSameExchangeProtection(ctx context.Context, plan *entity.ExecutionPlan, rec *entity.ExecutionRecord) {
	if s == nil || plan == nil || rec == nil || !rec.LiveTrading {
		return
	}
	if normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange)) != ArbitrageModeSameExchangeSpotPerp {
		return
	}

	req, record, err := s.buildSameExchangeProtectiveStop(ctx, plan, rec)
	if err != nil {
		s.logSameExchangeProtectionIssue("execution_same_exchange_protection_build_failed", plan, err)
		return
	}
	if req == nil || record == nil {
		return
	}

	adapter := s.trades[strings.ToLower(record.Exchange)]
	stopPlacer, ok := adapter.(exchange.TradeProtectiveStopPlacer)
	if !ok || stopPlacer == nil {
		s.logSameExchangeProtectionIssue("execution_same_exchange_protection_unsupported", plan, fmt.Errorf("%s does not support native protective stop", record.Exchange))
		return
	}

	resp, err := stopPlacer.PlaceProtectiveStop(ctx, *req)
	if err != nil {
		record.Status = "ERROR"
		record.ErrorMessage = err.Error()
		_ = s.orderRepo.Create(ctx, record)
		s.logSameExchangeProtectionIssue("execution_same_exchange_protection_place_failed", plan, err)
		return
	}
	record.Status = pickNonEmpty(resp.Status, "SUBMITTED")
	record.VenueOrderID = resp.VenueOrderID
	record.ClientOrderID = pickNonEmpty(resp.ClientOrderID, record.ClientOrderID)
	record.ExecutedQty = resp.ExecutedQty
	record.AvgPrice = firstPositiveFloat(resp.AveragePrice, req.StopPrice)
	record.RawResponse = resp.RawResponse
	_ = s.orderRepo.Create(ctx, record)
	if s.logger != nil {
		s.logger.Info(
			"execution_same_exchange_protection_armed",
			slog.String("plan_key", plan.PlanKey),
			slog.String("exchange", record.Exchange),
			slog.String("symbol", plan.Symbol),
			slog.Float64("qty", req.Quantity),
			slog.Float64("stop_price", req.StopPrice),
			slog.String("client_order_id", record.ClientOrderID),
		)
	}
}

func (s *ExecutionService) cancelSameExchangeProtection(ctx context.Context, plan *entity.ExecutionPlan) {
	if s == nil || plan == nil {
		return
	}
	orders, err := s.orderRepo.ListByPlanKey(ctx, plan.PlanKey)
	if err != nil {
		s.logSameExchangeProtectionIssue("execution_same_exchange_protection_list_failed", plan, err)
		return
	}
	for i := range orders {
		order := orders[i]
		if normalizeExecutionStatus(order.Phase) != sameExchangeProtectPhase {
			continue
		}
		if isServiceTerminalOrderStatus(order.Status) {
			continue
		}
		adapter := s.trades[strings.ToLower(order.Exchange)]
		canceler, ok := adapter.(exchange.TradeOrderCanceler)
		if !ok || canceler == nil {
			continue
		}
		lookup := exchange.OrderLookupRequest{
			CanonicalSymbol: plan.Symbol,
			VenueSymbol:     order.VenueSymbol,
			ClientOrderID:   order.ClientOrderID,
			VenueOrderID:    order.VenueOrderID,
		}
		if err := canceler.CancelOrder(ctx, lookup); err != nil {
			s.logSameExchangeProtectionIssue("execution_same_exchange_protection_cancel_failed", plan, err)
			continue
		}
		order.Status = "CANCELED"
		order.ErrorMessage = pickNonEmpty(order.ErrorMessage, "protective stop canceled after close")
		_ = s.orderRepo.Update(ctx, &order)
	}
}

func (s *ExecutionService) buildSameExchangeProtectiveStop(ctx context.Context, plan *entity.ExecutionPlan, rec *entity.ExecutionRecord) (*exchange.TradeOrderRequest, *entity.OrderRecord, error) {
	perpMeta, perpBook, perpFunding, closeSide, closeQty, ok := s.sameExchangePerpLegContext(plan)
	if !ok {
		return nil, nil, fmt.Errorf("missing same-exchange perp leg context")
	}
	adapter := s.trades[strings.ToLower(perpMeta.Exchange)]
	if adapter == nil || !adapter.Enabled() {
		return nil, nil, fmt.Errorf("trade adapter %s disabled", perpMeta.Exchange)
	}

	pos, err := adapter.GetPosition(ctx, plan.Symbol, perpMeta.VenueSymbol, perpMeta.VenueAssetID)
	if err != nil {
		return nil, nil, err
	}
	if math.Abs(pos.Quantity) > 1e-9 {
		closeQty = math.Abs(pos.Quantity)
	}
	if closeQty <= 0 {
		return nil, nil, fmt.Errorf("protective stop quantity is zero")
	}

	currentMark := firstPositiveFloat(pos.MarkPrice, perpFunding.MarkPrice, perpBook.AskPrice, perpBook.BidPrice, referencePriceForLeg(plan, "short_leg"))
	if currentMark <= 0 {
		return nil, nil, fmt.Errorf("missing mark price for protective stop")
	}
	stopPrice := sameExchangeProtectiveStopPrice(closeSide, currentMark, pos.LiquidationPrice, s.cfg)
	if stopPrice <= 0 {
		return nil, nil, fmt.Errorf("unable to derive stop price")
	}
	if strings.EqualFold(closeSide, "BUY") && stopPrice <= currentMark {
		return nil, nil, fmt.Errorf("protective stop %.4f must stay above current mark %.4f", stopPrice, currentMark)
	}
	if strings.EqualFold(closeSide, "SELL") && stopPrice >= currentMark {
		return nil, nil, fmt.Errorf("protective stop %.4f must stay below current mark %.4f", stopPrice, currentMark)
	}

	req := &exchange.TradeOrderRequest{
		CanonicalSymbol: plan.Symbol,
		VenueSymbol:     perpMeta.VenueSymbol,
		AssetID:         perpMeta.VenueAssetID,
		Side:            closeSide,
		OrderType:       "STOP_MARKET",
		Quantity:        round8(roundDownStep(closeQty, perpMeta.StepSize)),
		StopPrice:       roundPriceToTick(stopPrice, perpMeta.TickSize, closeSide),
		ReduceOnly:      true,
		WorkingType:     "MARK_PRICE",
		PriceProtect:    true,
		ClientOrderID:   buildClientOrderIDForExchange(perpMeta.Exchange, plan, sameExchangeProtectPhase, sameExchangeProtectLegRole),
		Reason:          sameExchangeProtectPhase,
	}
	record := &entity.OrderRecord{
		PlanKey:         plan.PlanKey,
		ExecutionStatus: pickExecutionStatus(rec),
		Phase:           sameExchangeProtectPhase,
		LegRole:         sameExchangeProtectLegRole,
		Exchange:        perpMeta.Exchange,
		Symbol:          plan.Symbol,
		VenueSymbol:     req.VenueSymbol,
		ClientOrderID:   req.ClientOrderID,
		Side:            req.Side,
		OrderType:       req.OrderType,
		ReduceOnly:      true,
		RequestedQty:    req.Quantity,
		RequestedPrice:  req.StopPrice,
		Status:          "PENDING",
	}
	return req, record, nil
}

func (s *ExecutionService) sameExchangePerpLegContext(plan *entity.ExecutionPlan) (entity.Symbol, entity.BookTopSnapshot, entity.FundingSnapshot, string, float64, bool) {
	if s == nil || plan == nil {
		return entity.Symbol{}, entity.BookTopSnapshot{}, entity.FundingSnapshot{}, "", 0, false
	}
	guardCfg := s.cfg
	guardCfg.ArbitrageMode = normalizeArbitrageMode(plan.ArbitrageMode, normalizeArbitrageMode(s.cfg.ArbitrageMode, ArbitrageModeCrossExchange))
	snapshots, ok := loadPairMarketSnapshot(s.store, guardCfg, plan.Symbol, plan.LongExchange, plan.ShortExchange)
	if !ok {
		return entity.Symbol{}, entity.BookTopSnapshot{}, entity.FundingSnapshot{}, "", 0, false
	}
	if isPerpetualSymbol(snapshots.ShortMeta) && !isPerpetualSymbol(snapshots.LongMeta) {
		return snapshots.ShortMeta, snapshots.ShortBook, snapshots.ShortFunding, "BUY", plan.ShortQty, true
	}
	if isPerpetualSymbol(snapshots.LongMeta) && !isPerpetualSymbol(snapshots.ShortMeta) {
		return snapshots.LongMeta, snapshots.LongBook, snapshots.LongFunding, "SELL", plan.LongQty, true
	}
	return entity.Symbol{}, entity.BookTopSnapshot{}, entity.FundingSnapshot{}, "", 0, false
}

func sameExchangeProtectiveStopPrice(closeSide string, currentMark, liqPrice float64, cfg Config) float64 {
	candidates := make([]float64, 0, 2)
	switch strings.ToUpper(strings.TrimSpace(closeSide)) {
	case "BUY":
		if currentMark > 0 && cfg.SameExchangeMax1hPriceShockRatio > 0 {
			candidates = append(candidates, currentMark*(1+cfg.SameExchangeMax1hPriceShockRatio))
		}
		if currentMark > 0 && liqPrice > currentMark && cfg.SameExchangeReduceLiqDistanceRatio > 0 {
			candidates = append(candidates, liqPrice/(1+cfg.SameExchangeReduceLiqDistanceRatio))
		}
		return smallestPositive(candidates...)
	case "SELL":
		if currentMark > 0 && cfg.SameExchangeMax1hPriceShockRatio > 0 {
			candidates = append(candidates, currentMark*(1-cfg.SameExchangeMax1hPriceShockRatio))
		}
		if liqPrice > 0 && currentMark > liqPrice && cfg.SameExchangeReduceLiqDistanceRatio > 0 && cfg.SameExchangeReduceLiqDistanceRatio < 1 {
			candidates = append(candidates, liqPrice/(1-cfg.SameExchangeReduceLiqDistanceRatio))
		}
		best := 0.0
		for _, item := range candidates {
			if item <= 0 {
				continue
			}
			if best == 0 || item > best {
				best = item
			}
		}
		return best
	default:
		return 0
	}
}

func smallestPositive(values ...float64) float64 {
	best := 0.0
	for _, item := range values {
		if item <= 0 {
			continue
		}
		if best == 0 || item < best {
			best = item
		}
	}
	return best
}

func (s *ExecutionService) logSameExchangeProtectionIssue(msg string, plan *entity.ExecutionPlan, err error) {
	if s == nil || s.logger == nil || err == nil || plan == nil {
		return
	}
	s.logger.Warn(msg, slog.String("plan_key", plan.PlanKey), slog.String("symbol", plan.Symbol), slog.Any("err", err))
}

func isServiceTerminalOrderStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FILLED", "NO_POSITION", "CANCELED", "CANCELLED", "REJECTED", "EXPIRED", "EXPIRED_IN_MATCH", "ERROR", "SKIPPED":
		return true
	default:
		return false
	}
}

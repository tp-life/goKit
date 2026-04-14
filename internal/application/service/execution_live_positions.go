package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

type LivePositionInspection struct {
	EvaluatedAtMs int64                   `json:"evaluated_at_ms"`
	Total         int                     `json:"total"`
	InSync        int                     `json:"in_sync"`
	AwaitingFill  int                     `json:"awaiting_fill"`
	SingleLeg     int                     `json:"single_leg"`
	SizeMismatch  int                     `json:"size_mismatch"`
	Flat          int                     `json:"flat"`
	SideMismatch  int                     `json:"side_mismatch"`
	Errors        int                     `json:"errors"`
	Candidates    []LivePositionCandidate `json:"candidates"`
}

type LivePositionCandidate struct {
	Execution  entity.ExecutionRecord         `json:"execution"`
	Plan       *entity.ExecutionPlan          `json:"plan,omitempty"`
	LongLeg    LivePositionLegInspection      `json:"long_leg"`
	ShortLeg   LivePositionLegInspection      `json:"short_leg"`
	Risk       SameExchangeLiveRiskInspection `json:"risk,omitempty"`
	SyncStatus string                         `json:"sync_status"`
	Summary    string                         `json:"summary"`
}

type LivePositionLegInspection struct {
	Role          string            `json:"role"`
	Exchange      string            `json:"exchange"`
	VenueSymbol   string            `json:"venue_symbol"`
	ExpectedSide  string            `json:"expected_side"`
	ExpectedQty   float64           `json:"expected_qty"`
	ObservedQty   float64           `json:"observed_qty"`
	QtyDelta      float64           `json:"qty_delta"`
	QtyDeltaRatio float64           `json:"qty_delta_ratio"`
	Position      exchange.Position `json:"position"`
	HasPosition   bool              `json:"has_position"`
	DirectionOK   bool              `json:"direction_ok"`
	SizeOK        bool              `json:"size_ok"`
	Error         string            `json:"error,omitempty"`
}

type SameExchangeLiveRiskInspection struct {
	Enabled                      bool    `json:"enabled"`
	PriceShockAllowed            bool    `json:"price_shock_allowed"`
	PriceShockReason             string  `json:"price_shock_reason,omitempty"`
	PriceShockRatio              float64 `json:"price_shock_ratio"`
	PriceShockThresholdRatio     float64 `json:"price_shock_threshold_ratio"`
	PriceShockCurrentMarkPrice   float64 `json:"price_shock_current_mark_price"`
	PriceShockBaselineMarkPrice  float64 `json:"price_shock_baseline_mark_price"`
	LiquidationPrice             float64 `json:"liquidation_price"`
	LiquidationDistanceRatio     float64 `json:"liquidation_distance_ratio"`
	MinLiqDistanceRatio          float64 `json:"min_liq_distance_ratio"`
	ReduceLiqDistanceRatio       float64 `json:"reduce_liq_distance_ratio"`
	EmergencyLiqDistanceRatio    float64 `json:"emergency_liq_distance_ratio"`
	ProtectiveOrderArmed         bool    `json:"protective_order_armed"`
	ProtectiveOrderStatus        string  `json:"protective_order_status,omitempty"`
	ProtectiveOrderStopPrice     float64 `json:"protective_order_stop_price"`
	ProtectiveOrderExchange      string  `json:"protective_order_exchange,omitempty"`
	ProtectiveOrderVenueSymbol   string  `json:"protective_order_venue_symbol,omitempty"`
	ProtectiveOrderClientOrderID string  `json:"protective_order_client_order_id,omitempty"`
	ProtectiveOrderUpdatedAtMs   int64   `json:"protective_order_updated_at_ms"`
	ProtectiveOrderErrorMessage  string  `json:"protective_order_error_message,omitempty"`
}

func (s *ExecutionService) InspectLivePositions(ctx context.Context) (LivePositionInspection, error) {
	records, err := s.execRepo.ListActiveLive(ctx)
	if err != nil {
		return LivePositionInspection{}, err
	}
	now := time.Now().UTC()
	candidates := make([]LivePositionCandidate, 0, len(records))
	for _, rec := range records {
		candidates = append(candidates, s.evaluateLivePositionCandidate(ctx, rec))
	}
	sortLivePositionCandidates(candidates)
	return summarizeLivePositionInspection(now, candidates), nil
}

func (s *ExecutionService) ReconcileLivePositions(ctx context.Context) (int, error) {
	inspection, err := s.InspectLivePositions(ctx)
	if err != nil {
		return 0, err
	}
	reconciled := 0
	for _, candidate := range inspection.Candidates {
		switch {
		case shouldReconcileFlatLivePosition(candidate):
			rec := candidate.Execution
			if err := applyExecutionEvent(&rec, executionEvent{
				Name:         "external_positions_flat",
				TargetStatus: executionStateClosed,
				Reason:       buildExternalFlatPositionReason(candidate),
				OccurredAtMs: inspection.EvaluatedAtMs,
			}, externalFlatCloseTransitions); err != nil {
				if s.logger != nil {
					s.logger.Error(
						"execution_live_position_reconcile_failed",
						slog.String("plan_key", rec.PlanKey),
						slog.String("status", rec.Status),
						slog.String("sync_status", candidate.SyncStatus),
						slog.Any("err", err),
					)
				}
				continue
			}
			if rec.ClosedAtMs == 0 {
				rec.ClosedAtMs = inspection.EvaluatedAtMs
			}
			rec.LastError = ""
			if err := s.execRepo.Upsert(ctx, &rec); err != nil {
				return reconciled, err
			}
			reconciled++
			if s.logger != nil {
				s.logger.Info(
					"execution_live_position_reconciled_closed",
					slog.String("plan_key", rec.PlanKey),
					slog.String("symbol", rec.Symbol),
					slog.String("previous_status", candidate.Execution.Status),
					slog.String("sync_status", candidate.SyncStatus),
					slog.String("reason", rec.StatusReason),
				)
			}
		case shouldForceCloseAbnormalLivePosition(s.cfg, candidate):
			if candidate.Plan == nil || !candidate.Execution.LiveTrading || !candidate.Execution.AutoClose {
				continue
			}
			closedRec, closeErr := s.closePlan(ctx, candidate.Plan, "live_position_monitor", candidate.Execution.LiveTrading)
			if closeErr != nil {
				if s.logger != nil {
					s.logger.Error(
						"execution_live_position_force_close_failed",
						slog.String("plan_key", candidate.Execution.PlanKey),
						slog.String("sync_status", candidate.SyncStatus),
						slog.Any("err", closeErr),
					)
				}
				continue
			}
			if closedRec == nil {
				continue
			}
			closedRec.StatusReason = buildAbnormalLivePositionReason(candidate)
			closedRec.LastError = ""
			if err := s.execRepo.Upsert(ctx, closedRec); err != nil {
				return reconciled, err
			}
			reconciled++
			if s.logger != nil {
				s.logger.Warn(
					"execution_live_position_force_close_triggered",
					slog.String("plan_key", candidate.Execution.PlanKey),
					slog.String("symbol", candidate.Execution.Symbol),
					slog.String("sync_status", candidate.SyncStatus),
					slog.String("reason", closedRec.StatusReason),
				)
			}
		}
	}
	return reconciled, nil
}

func (s *ExecutionService) evaluateLivePositionCandidate(ctx context.Context, rec entity.ExecutionRecord) LivePositionCandidate {
	candidate := LivePositionCandidate{Execution: rec}

	plan, err := s.planRepo.FindByPlanKey(ctx, rec.PlanKey)
	if err != nil {
		message := fmt.Sprintf("load execution plan failed: %v", err)
		candidate.LongLeg = LivePositionLegInspection{
			Role:         "long_leg",
			Exchange:     rec.LongExchange,
			ExpectedSide: "LONG",
			Error:        message,
		}
		candidate.ShortLeg = LivePositionLegInspection{
			Role:         "short_leg",
			Exchange:     rec.ShortExchange,
			ExpectedSide: "SHORT",
			Error:        message,
		}
		candidate.SyncStatus = "error"
		candidate.Summary = message
		return candidate
	}
	if plan == nil {
		message := "execution plan not found"
		candidate.LongLeg = LivePositionLegInspection{
			Role:         "long_leg",
			Exchange:     rec.LongExchange,
			ExpectedSide: "LONG",
			Error:        message,
		}
		candidate.ShortLeg = LivePositionLegInspection{
			Role:         "short_leg",
			Exchange:     rec.ShortExchange,
			ExpectedSide: "SHORT",
			Error:        message,
		}
		candidate.SyncStatus = "error"
		candidate.Summary = message
		return candidate
	}

	candidate.Plan = plan
	candidate.LongLeg = s.inspectLivePositionLeg(ctx, rec.Symbol, rec.LongExchange, plan.LongVenueSymbol, "LONG", plan.LongQty)
	candidate.ShortLeg = s.inspectLivePositionLeg(ctx, rec.Symbol, rec.ShortExchange, plan.ShortVenueSymbol, "SHORT", plan.ShortQty)
	candidate.Risk = s.inspectSameExchangeLiveRisk(ctx, rec, plan, candidate.LongLeg, candidate.ShortLeg)

	switch {
	case candidate.LongLeg.Error != "" || candidate.ShortLeg.Error != "":
		candidate.SyncStatus = "error"
		candidate.Summary = joinLivePositionErrors(candidate.LongLeg.Error, candidate.ShortLeg.Error)
	case !candidate.LongLeg.HasPosition && !candidate.ShortLeg.HasPosition && normalizeExecutionStatus(rec.Status) == executionStatePendingOpen:
		candidate.SyncStatus = "awaiting_fill"
		candidate.Summary = "execution is still pending open; no venue position is visible yet"
	case !candidate.LongLeg.HasPosition && !candidate.ShortLeg.HasPosition:
		candidate.SyncStatus = "flat"
		candidate.Summary = "execution is marked live, but both venue legs are flat on exchange"
	case candidate.LongLeg.HasPosition != candidate.ShortLeg.HasPosition:
		candidate.SyncStatus = "single_leg"
		candidate.Summary = "only one venue leg currently has live exposure"
	case !candidate.LongLeg.DirectionOK || !candidate.ShortLeg.DirectionOK:
		candidate.SyncStatus = "side_mismatch"
		candidate.Summary = "venue exposure exists, but at least one leg direction differs from the expected side"
	case !candidate.LongLeg.SizeOK || !candidate.ShortLeg.SizeOK:
		candidate.SyncStatus = "size_mismatch"
		candidate.Summary = "venue exposure exists, but at least one leg quantity deviates materially from the planned hedge size"
	case candidate.LongLeg.DirectionOK && candidate.ShortLeg.DirectionOK:
		candidate.SyncStatus = "in_sync"
		candidate.Summary = "both venue legs have exposure matching the expected direction"
	default:
		candidate.SyncStatus = "error"
		candidate.Summary = "live position state cannot be classified"
	}
	return candidate
}

func (s *ExecutionService) inspectLivePositionLeg(ctx context.Context, canonicalSymbol, exchangeName, venueSymbol, expectedSide string, expectedQty float64) LivePositionLegInspection {
	leg := LivePositionLegInspection{
		Exchange:     exchangeName,
		VenueSymbol:  venueSymbol,
		ExpectedSide: expectedSide,
		ExpectedQty:  expectedQty,
	}
	switch expectedSide {
	case "SHORT":
		leg.Role = "short_leg"
	default:
		leg.Role = "long_leg"
	}
	if strings.TrimSpace(venueSymbol) == "" {
		leg.Error = "missing venue symbol"
		return leg
	}
	adapter := s.trades[strings.ToLower(strings.TrimSpace(exchangeName))]
	if adapter == nil {
		leg.Error = fmt.Sprintf("trade adapter %s not found", exchangeName)
		return leg
	}
	if !adapter.Enabled() {
		leg.Error = fmt.Sprintf("trade adapter %s disabled", exchangeName)
		return leg
	}
	pos, err := adapter.GetPosition(ctx, canonicalSymbol, venueSymbol, "")
	if err != nil {
		leg.Error = err.Error()
		return leg
	}
	leg.Position = pos
	leg.HasPosition = math.Abs(pos.Quantity) > 1e-9
	leg.ObservedQty = math.Abs(pos.Quantity)
	leg.DirectionOK = !leg.HasPosition || positionMatchesExpectedDirection(expectedSide, pos.Quantity)
	leg.SizeOK = true
	if expectedQty > 0 {
		leg.QtyDelta = math.Abs(leg.ObservedQty - expectedQty)
		leg.QtyDeltaRatio = leg.QtyDelta / expectedQty
		leg.SizeOK = leg.QtyDeltaRatio <= s.cfg.ExecutionPositionMonitorMaxQtyDeviationRatio+1e-9
	}
	return leg
}

func positionMatchesExpectedDirection(expectedSide string, quantity float64) bool {
	switch strings.ToUpper(strings.TrimSpace(expectedSide)) {
	case "SHORT":
		return quantity < 0
	default:
		return quantity > 0
	}
}

func shouldReconcileFlatLivePosition(candidate LivePositionCandidate) bool {
	return strings.EqualFold(strings.TrimSpace(candidate.SyncStatus), "flat")
}

func shouldForceCloseAbnormalLivePosition(cfg Config, candidate LivePositionCandidate) bool {
	switch strings.ToLower(strings.TrimSpace(candidate.SyncStatus)) {
	case "single_leg":
		return cfg.ExecutionPositionMonitorForceCloseOnSingleLeg
	case "side_mismatch":
		return cfg.ExecutionPositionMonitorForceCloseOnSideMismatch
	case "size_mismatch":
		return cfg.ExecutionPositionMonitorForceCloseOnSizeMismatch
	default:
		return false
	}
}

func buildExternalFlatPositionReason(candidate LivePositionCandidate) string {
	parts := []string{
		"both venue legs are flat on exchange",
	}
	if strings.TrimSpace(candidate.Execution.Status) != "" {
		parts = append(parts, fmt.Sprintf("previous_status=%s", normalizeExecutionStatus(candidate.Execution.Status)))
	}
	if strings.TrimSpace(candidate.Execution.LongExchange) != "" || strings.TrimSpace(candidate.Execution.ShortExchange) != "" {
		parts = append(parts, fmt.Sprintf("venues=%s/%s", candidate.Execution.LongExchange, candidate.Execution.ShortExchange))
	}
	parts = append(parts, "marking execution as externally closed")
	return strings.Join(parts, "; ")
}

func buildAbnormalLivePositionReason(candidate LivePositionCandidate) string {
	parts := []string{
		fmt.Sprintf("live position monitor detected %s", strings.TrimSpace(candidate.SyncStatus)),
		strings.TrimSpace(candidate.Summary),
	}
	if candidate.LongLeg.ExpectedQty > 0 && !candidate.LongLeg.SizeOK {
		parts = append(parts, fmt.Sprintf("long qty deviation %.2f%%", candidate.LongLeg.QtyDeltaRatio*100))
	}
	if candidate.ShortLeg.ExpectedQty > 0 && !candidate.ShortLeg.SizeOK {
		parts = append(parts, fmt.Sprintf("short qty deviation %.2f%%", candidate.ShortLeg.QtyDeltaRatio*100))
	}
	return joinLivePositionErrors(parts...)
}

func joinLivePositionErrors(values ...string) string {
	items := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		items = append(items, value)
	}
	return strings.Join(items, " | ")
}

func summarizeLivePositionInspection(now time.Time, candidates []LivePositionCandidate) LivePositionInspection {
	out := LivePositionInspection{
		EvaluatedAtMs: now.UnixMilli(),
		Total:         len(candidates),
		Candidates:    candidates,
	}
	for _, item := range candidates {
		switch strings.ToLower(strings.TrimSpace(item.SyncStatus)) {
		case "in_sync":
			out.InSync++
		case "awaiting_fill":
			out.AwaitingFill++
		case "single_leg":
			out.SingleLeg++
		case "size_mismatch":
			out.SizeMismatch++
		case "flat":
			out.Flat++
		case "side_mismatch":
			out.SideMismatch++
		case "error":
			out.Errors++
		}
	}
	return out
}

func sortLivePositionCandidates(items []LivePositionCandidate) {
	sort.Slice(items, func(i, j int) bool {
		left := items[i]
		right := items[j]
		if livePositionSeverity(left.SyncStatus) != livePositionSeverity(right.SyncStatus) {
			return livePositionSeverity(left.SyncStatus) < livePositionSeverity(right.SyncStatus)
		}
		if left.Execution.TargetCloseTimeMs != right.Execution.TargetCloseTimeMs {
			return left.Execution.TargetCloseTimeMs < right.Execution.TargetCloseTimeMs
		}
		if left.Execution.OpenedAtMs != right.Execution.OpenedAtMs {
			return left.Execution.OpenedAtMs > right.Execution.OpenedAtMs
		}
		return left.Execution.PlanKey < right.Execution.PlanKey
	})
}

func livePositionSeverity(status string) int {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "error":
		return 0
	case "single_leg":
		return 1
	case "side_mismatch":
		return 2
	case "size_mismatch":
		return 3
	case "flat":
		return 4
	case "awaiting_fill":
		return 5
	default:
		return 6
	}
}

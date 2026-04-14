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
)

type activeReplacementTarget struct {
	Record            entity.ExecutionRecord
	Plan              entity.ExecutionPlan
	AllocatedNotional float64
	HoldNetPNL        float64
	CloseCostPNL      float64
}

func (s *ExecutionService) runActiveReplacement(ctx context.Context) {
	if !s.cfg.ExecutionReplacementEnabled || !s.cfg.Execution.Enabled || !s.cfg.Execution.AutoEntry || !s.cfg.Execution.AutoClose {
		return
	}

	activeRecords, err := s.execRepo.ListActiveLive(ctx)
	if err != nil {
		s.logger.Error("execution_active_replacement_list_records_failed", slog.Any("err", err))
		return
	}
	if len(activeRecords) == 0 {
		return
	}
	if s.cfg.ExecutionReplacementOnlyWhenConstrained && !s.activeReplacementNeedsCapacity(activeRecords) {
		return
	}

	now := time.Now().UTC()
	targets := s.buildActiveReplacementTargets(ctx, now, activeRecords)
	if len(targets) == 0 {
		return
	}

	plans, err := s.planRepo.ListLatest(ctx, s.cfg.Execution.MaxLatestPlans)
	if err != nil {
		s.logger.Error("execution_active_replacement_list_plans_failed", slog.Any("err", err))
		return
	}
	sortExecutionPlansByPriority(plans)

	activePlanKeys := make(map[string]struct{}, len(activeRecords))
	activeRollingGroups := make(map[string]int)
	for _, rec := range activeRecords {
		activePlanKeys[rec.PlanKey] = struct{}{}
		key := strings.TrimSpace(rec.RollingGroupKey)
		if key != "" {
			activeRollingGroups[key]++
		}
	}

	for _, target := range targets {
		successor, improvement, err := s.findActiveReplacementSuccessor(ctx, target, plans, activePlanKeys, activeRollingGroups)
		if err != nil {
			s.logger.Error(
				"execution_active_replacement_candidate_failed",
				slog.String("plan_key", target.Record.PlanKey),
				slog.Any("err", err),
			)
			continue
		}
		if successor == nil {
			continue
		}

		reason := fmt.Sprintf(
			"better opportunity selected; replacement net improvement %.4f USDT after close/open costs",
			improvement,
		)
		if err := s.applyActiveReplacement(ctx, now, target, successor, reason); err != nil {
			s.logger.Error(
				"execution_active_replacement_apply_failed",
				slog.String("plan_key", target.Record.PlanKey),
				slog.String("successor_plan_key", successor.PlanKey),
				slog.Any("err", err),
			)
			return
		}
		s.logger.Info(
			"execution_active_replacement_applied",
			slog.String("plan_key", target.Record.PlanKey),
			slog.String("successor_plan_key", successor.PlanKey),
			slog.Float64("hold_net_pnl", target.HoldNetPNL),
			slog.Float64("close_cost_pnl", target.CloseCostPNL),
			slog.Float64("successor_net_pnl", successor.NetExpectedPNL),
			slog.Float64("net_improvement_pnl", improvement),
		)
		return
	}
}

func (s *ExecutionService) activeReplacementNeedsCapacity(activeRecords []entity.ExecutionRecord) bool {
	if s.cfg.Execution.MaxLivePlans > 0 && len(activeRecords) >= s.cfg.Execution.MaxLivePlans {
		return true
	}
	effectiveNotional := s.cfg.EffectiveNotional()
	if effectiveNotional <= 0 {
		return false
	}
	allocatedNotional := 0.0
	for _, rec := range activeRecords {
		allocatedNotional += rec.AllocatedNotionalUSDT
	}
	return allocatedNotional >= effectiveNotional-1e-9
}

func (s *ExecutionService) buildActiveReplacementTargets(ctx context.Context, now time.Time, activeRecords []entity.ExecutionRecord) []activeReplacementTarget {
	targets := make([]activeReplacementTarget, 0, len(activeRecords))
	for _, rec := range activeRecords {
		if !shouldConsiderActiveReplacementRecord(rec) {
			continue
		}
		plan, err := s.planRepo.FindByPlanKey(ctx, rec.PlanKey)
		if err != nil || plan == nil {
			continue
		}
		holdNetPNL, closeCostPNL, err := s.currentHoldNetForReplacement(now, rec, *plan)
		if err != nil {
			if s.logger != nil {
				s.logger.Warn(
					"execution_active_replacement_hold_value_skipped",
					slog.String("plan_key", rec.PlanKey),
					slog.Any("err", err),
				)
			}
			continue
		}
		targets = append(targets, activeReplacementTarget{
			Record:            rec,
			Plan:              *plan,
			AllocatedNotional: firstPositiveFloat(rec.AllocatedNotionalUSDT, plan.RoundedNotionalUSDT, plan.TargetNotionalUSDT),
			HoldNetPNL:        holdNetPNL,
			CloseCostPNL:      closeCostPNL,
		})
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].HoldNetPNL != targets[j].HoldNetPNL {
			return targets[i].HoldNetPNL < targets[j].HoldNetPNL
		}
		if targets[i].AllocatedNotional != targets[j].AllocatedNotional {
			return targets[i].AllocatedNotional > targets[j].AllocatedNotional
		}
		return targets[i].Record.PlanKey < targets[j].Record.PlanKey
	})
	return targets
}

func shouldConsiderActiveReplacementRecord(rec entity.ExecutionRecord) bool {
	if !rec.LiveTrading || !rec.AutoClose {
		return false
	}
	return normalizeExecutionStatus(rec.Status) == executionStateOpened
}

func (s *ExecutionService) currentHoldNetForReplacement(now time.Time, rec entity.ExecutionRecord, plan entity.ExecutionPlan) (float64, float64, error) {
	snapshots, ok := loadPairFundingSnapshot(s.store, s.cfg, plan.Symbol, plan.LongExchange, plan.ShortExchange)
	if !ok {
		return 0, 0, fmt.Errorf("missing funding snapshot for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}
	if isSnapshotStaleForConfig(s.cfg, now, snapshots.LongFunding.EventTimeMs) ||
		isSnapshotStaleForConfig(s.cfg, now, snapshots.ShortFunding.EventTimeMs) {
		return 0, 0, fmt.Errorf("stale funding snapshot for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}
	projection, ok := projectFundingCarryForPlanRevalidation(s.cfg, now, &plan, snapshots.LongFunding, snapshots.ShortFunding)
	if !ok {
		return 0, 0, fmt.Errorf("no funding projection for %s %s/%s", plan.Symbol, plan.LongExchange, plan.ShortExchange)
	}
	notional := firstPositiveFloat(rec.AllocatedNotionalUSDT, plan.RoundedNotionalUSDT, plan.TargetNotionalUSDT)
	if notional <= 0 {
		return 0, 0, fmt.Errorf("invalid allocated notional for %s", plan.PlanKey)
	}
	grossFundingPNL := notional * projection.CarryRate
	closeCostPNL := replacementImmediateCloseCostPNL(notional, plan, s.cfg)
	holdReservePNL := remainingHoldReservePNL(notional, plan)
	return grossFundingPNL - holdReservePNL, closeCostPNL, nil
}

func remainingHoldReservePNL(notional float64, plan entity.ExecutionPlan) float64 {
	if notional <= 0 {
		return 0
	}
	exitSlippagePNL := notional * plan.ExitPenaltyBps / 10000
	return plan.ExitFeePNL + exitSlippagePNL + plan.SafetyBufferPNL
}

func replacementImmediateCloseCostPNL(notional float64, plan entity.ExecutionPlan, cfg Config) float64 {
	if notional <= 0 {
		return 0
	}
	exitSlippagePNL := notional * plan.ExitPenaltyBps / 10000
	return plan.ExitFeePNL + exitSlippagePNL + cfg.ExecutionReplacementExtraSafetyBufferUSDT
}

func (s *ExecutionService) findActiveReplacementSuccessor(
	ctx context.Context,
	target activeReplacementTarget,
	plans []entity.ExecutionPlan,
	activePlanKeys map[string]struct{},
	activeRollingGroups map[string]int,
) (*entity.ExecutionPlan, float64, error) {
	currentGroupKey := strings.TrimSpace(target.Plan.RollingGroupKey)
	for i := range plans {
		candidate := plans[i]
		if !candidate.ReadyNow || !strings.EqualFold(strings.TrimSpace(candidate.Status), "ready") {
			continue
		}
		if candidate.PlanKey == target.Record.PlanKey {
			continue
		}
		if _, exists := activePlanKeys[candidate.PlanKey]; exists {
			continue
		}
		if isSameExecutionPair(target.Plan, candidate) {
			continue
		}
		candidateGroupKey := strings.TrimSpace(candidate.RollingGroupKey)
		if candidateGroupKey != "" {
			if candidateGroupKey == currentGroupKey {
				continue
			}
			if activeRollingGroups[candidateGroupKey] > 0 {
				continue
			}
		}
		existing, err := s.execRepo.FindByPlanKey(ctx, candidate.PlanKey)
		if err != nil {
			return nil, 0, err
		}
		if existing != nil {
			continue
		}

		successor, err := s.prepareActiveReplacementPlan(ctx, candidate, target.AllocatedNotional)
		if err != nil {
			continue
		}
		replacementNetPNL := successor.NetExpectedPNL - target.CloseCostPNL
		improvement := replacementNetPNL - target.HoldNetPNL
		if s.cfg.ExecutionReplacementRequireNetImprovement && improvement < s.cfg.ExecutionReplacementMinNetImprovementPNL {
			continue
		}
		return successor, improvement, nil
	}
	return nil, 0, nil
}

func isSameExecutionPair(left, right entity.ExecutionPlan) bool {
	return strings.EqualFold(left.Symbol, right.Symbol) &&
		strings.EqualFold(left.LongExchange, right.LongExchange) &&
		strings.EqualFold(left.ShortExchange, right.ShortExchange)
}

func (s *ExecutionService) prepareActiveReplacementPlan(ctx context.Context, candidate entity.ExecutionPlan, targetNotional float64) (*entity.ExecutionPlan, error) {
	baseNotional := firstPositiveFloat(candidate.RoundedNotionalUSDT, candidate.TargetNotionalUSDT)
	if targetNotional <= 0 || baseNotional <= 0 || baseNotional <= targetNotional+1e-9 {
		cp := candidate
		return &cp, nil
	}

	scaled, err := s.scalePlanForAutoBudget(candidate, targetNotional)
	if err != nil {
		return nil, err
	}
	if err := s.planRepo.SaveBatch(ctx, scaled.BatchID, scaled.OpportunityBatchID, []entity.ExecutionPlan{scaled}); err != nil {
		return nil, err
	}
	return &scaled, nil
}

func (s *ExecutionService) applyActiveReplacement(ctx context.Context, now time.Time, target activeReplacementTarget, successor *entity.ExecutionPlan, reason string) error {
	if successor == nil {
		return fmt.Errorf("nil successor plan")
	}

	closedRec, err := s.closePlan(ctx, &target.Plan, "active_replacement", target.Record.LiveTrading)
	if err != nil {
		return err
	}
	if closedRec == nil || normalizeExecutionStatus(closedRec.Status) != executionStateClosed {
		return fmt.Errorf("active replacement close did not finish cleanly for plan %s", target.Plan.PlanKey)
	}
	closedRec.SuccessorPlanKey = successor.PlanKey
	closedRec.StatusReason = reason
	if err := s.execRepo.Upsert(ctx, closedRec); err != nil {
		return err
	}

	nextRec, err := s.openPlan(ctx, successor, "active_replacement_reopen", true)
	if err != nil {
		return err
	}
	if nextRec == nil {
		return nil
	}
	nextRec.PredecessorPlanKey = target.Plan.PlanKey
	nextRec.StatusReason = reason
	nextRec.LastReviewAtMs = now.UnixMilli()
	nextRec.LastReviewReason = fmt.Sprintf("opened as active replacement successor for %s", target.Plan.PlanKey)
	return s.execRepo.Upsert(ctx, nextRec)
}

func sortExecutionPlansByPriority(items []entity.ExecutionPlan) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].ReadyNow != items[j].ReadyNow {
			return items[i].ReadyNow
		}
		if !nearlyEqual(items[i].Score, items[j].Score) {
			return items[i].Score > items[j].Score
		}
		if !nearlyEqual(items[i].NetExpectedPNL, items[j].NetExpectedPNL) {
			return items[i].NetExpectedPNL > items[j].NetExpectedPNL
		}
		return items[i].PlanKey < items[j].PlanKey
	})
}

func nearlyEqual(left, right float64) bool {
	return math.Abs(left-right) <= 1e-9
}

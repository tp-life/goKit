package service

import (
	"context"
	"fmt"
	"sort"
	"time"

	"goKit/internal/domain/entity"
)

type AutoCloseInspection struct {
	EvaluatedAtMs     int64                `json:"evaluated_at_ms"`
	Total             int                  `json:"total"`
	Eligible          int                  `json:"eligible"`
	ShouldClose       int                  `json:"should_close"`
	AutoCloseDisabled int                  `json:"auto_close_disabled"`
	DecisionErrors    int                  `json:"decision_errors"`
	Candidates        []AutoCloseCandidate `json:"candidates"`
}

type AutoCloseCandidate struct {
	Execution entity.ExecutionRecord     `json:"execution"`
	Plan      *entity.ExecutionPlan      `json:"plan,omitempty"`
	Decision  AutoCloseCandidateDecision `json:"decision"`
}

type AutoCloseCandidateDecision struct {
	Eligible         bool   `json:"eligible"`
	AutoCloseEnabled bool   `json:"auto_close_enabled"`
	ShouldClose      bool   `json:"should_close"`
	Trigger          string `json:"trigger,omitempty"`
	Reason           string `json:"reason,omitempty"`
	Error            string `json:"error,omitempty"`
}

type AutoCloseSweepReport struct {
	EvaluatedAtMs int64                  `json:"evaluated_at_ms"`
	Total         int                    `json:"total"`
	Eligible      int                    `json:"eligible"`
	ShouldClose   int                    `json:"should_close"`
	Attempted     int                    `json:"attempted"`
	Closed        int                    `json:"closed"`
	Failed        int                    `json:"failed"`
	Skipped       int                    `json:"skipped"`
	Results       []AutoCloseSweepResult `json:"results"`
}

type AutoCloseSweepResult struct {
	Candidate AutoCloseCandidate      `json:"candidate"`
	Attempted bool                    `json:"attempted"`
	Success   bool                    `json:"success"`
	Result    *entity.ExecutionRecord `json:"result,omitempty"`
	Error     string                  `json:"error,omitempty"`
}

func (s *ExecutionService) InspectLiveAutoClose(ctx context.Context) (AutoCloseInspection, error) {
	records, err := s.execRepo.ListActiveLive(ctx)
	if err != nil {
		return AutoCloseInspection{}, err
	}
	now := time.Now().UTC()
	candidates := s.inspectAutoCloseCandidates(ctx, now, records)
	return summarizeAutoCloseInspection(now, candidates), nil
}

func (s *ExecutionService) SweepLiveAutoClose(ctx context.Context) (AutoCloseSweepReport, error) {
	inspection, err := s.InspectLiveAutoClose(ctx)
	if err != nil {
		return AutoCloseSweepReport{}, err
	}

	report := AutoCloseSweepReport{
		EvaluatedAtMs: inspection.EvaluatedAtMs,
		Total:         inspection.Total,
		Eligible:      inspection.Eligible,
		ShouldClose:   inspection.ShouldClose,
		Results:       make([]AutoCloseSweepResult, 0, len(inspection.Candidates)),
	}

	for _, candidate := range inspection.Candidates {
		item := AutoCloseSweepResult{
			Candidate: candidate,
		}
		if !candidate.Decision.ShouldClose || candidate.Plan == nil {
			report.Results = append(report.Results, item)
			continue
		}

		item.Attempted = true
		report.Attempted++

		rec, err := s.closePlan(ctx, candidate.Plan, candidate.Decision.Trigger, candidate.Execution.LiveTrading)
		if err != nil {
			item.Error = err.Error()
			report.Failed++
			report.Results = append(report.Results, item)
			continue
		}

		item.Success = true
		item.Result = rec
		report.Closed++
		report.Results = append(report.Results, item)
	}

	report.Skipped = report.Total - report.Attempted
	if report.Skipped < 0 {
		report.Skipped = 0
	}
	return report, nil
}

func (s *ExecutionService) inspectAutoCloseCandidates(ctx context.Context, now time.Time, records []entity.ExecutionRecord) []AutoCloseCandidate {
	candidates := make([]AutoCloseCandidate, 0, len(records))
	for _, rec := range records {
		candidates = append(candidates, s.evaluateAutoCloseCandidate(ctx, now, rec))
	}
	sortAutoCloseCandidates(candidates)
	return candidates
}

func (s *ExecutionService) evaluateAutoCloseCandidate(ctx context.Context, now time.Time, rec entity.ExecutionRecord) AutoCloseCandidate {
	candidate := AutoCloseCandidate{
		Execution: rec,
		Decision: AutoCloseCandidateDecision{
			Eligible:         shouldAutoCloseRecord(rec),
			AutoCloseEnabled: rec.AutoClose,
		},
	}

	if !candidate.Decision.Eligible {
		candidate.Decision.Reason = fmt.Sprintf("execution status %s is outside the auto-close scan set", normalizeExecutionStatus(rec.Status))
		return candidate
	}
	if !candidate.Decision.AutoCloseEnabled {
		candidate.Decision.Reason = "execution record has auto_close disabled"
		return candidate
	}

	plan, err := s.planRepo.FindByPlanKey(ctx, rec.PlanKey)
	if err != nil {
		candidate.Decision.Error = fmt.Sprintf("load execution plan failed: %v", err)
		return candidate
	}
	if plan == nil {
		candidate.Decision.Error = fmt.Sprintf("execution plan %s not found", rec.PlanKey)
		return candidate
	}
	candidate.Plan = plan

	decision, err := s.evaluateCloseDecision(ctx, now, rec, plan)
	if err != nil {
		candidate.Decision.Error = err.Error()
		return candidate
	}
	candidate.Decision.ShouldClose = decision.shouldClose
	candidate.Decision.Trigger = decision.trigger
	if decision.reason != "" {
		candidate.Decision.Reason = decision.reason
	} else {
		candidate.Decision.Reason = describeIdleAutoCloseReason(now, rec)
	}
	return candidate
}

func describeIdleAutoCloseReason(now time.Time, rec entity.ExecutionRecord) string {
	if normalizeStrategyMode(rec.StrategyMode, StrategyModeLegacyProjection) == StrategyModeRollingCycleAligned {
		if rec.NextReviewTimeMs > 0 && now.UnixMilli() < rec.NextReviewTimeMs {
			return fmt.Sprintf("rolling review is waiting until %s; only safety guards are active", time.UnixMilli(rec.NextReviewTimeMs).Local().Format("2006-01-02 15:04:05"))
		}
		if rec.NextReviewTimeMs > 0 {
			return fmt.Sprintf("rolling review point %s has been reached; waiting for rolling monitor decision", time.UnixMilli(rec.NextReviewTimeMs).Local().Format("2006-01-02 15:04:05"))
		}
	}
	if rec.TargetCloseTimeMs > 0 && now.UnixMilli() < rec.TargetCloseTimeMs {
		return fmt.Sprintf("waiting until target close time %s; safety guards are not triggered", time.UnixMilli(rec.TargetCloseTimeMs).Local().Format("2006-01-02 15:04:05"))
	}
	return "no auto-close trigger is active"
}

func summarizeAutoCloseInspection(now time.Time, candidates []AutoCloseCandidate) AutoCloseInspection {
	out := AutoCloseInspection{
		EvaluatedAtMs: now.UnixMilli(),
		Total:         len(candidates),
		Candidates:    candidates,
	}
	for _, item := range candidates {
		if item.Decision.Eligible {
			out.Eligible++
		}
		if item.Decision.ShouldClose {
			out.ShouldClose++
		}
		if !item.Decision.AutoCloseEnabled {
			out.AutoCloseDisabled++
		}
		if item.Decision.Error != "" {
			out.DecisionErrors++
		}
	}
	return out
}

func sortAutoCloseCandidates(items []AutoCloseCandidate) {
	sort.SliceStable(items, func(i, j int) bool {
		left := items[i]
		right := items[j]

		if left.Decision.ShouldClose != right.Decision.ShouldClose {
			return left.Decision.ShouldClose
		}
		leftErr := left.Decision.Error != ""
		rightErr := right.Decision.Error != ""
		if leftErr != rightErr {
			return leftErr
		}
		if left.Decision.Eligible != right.Decision.Eligible {
			return left.Decision.Eligible
		}
		if left.Decision.AutoCloseEnabled != right.Decision.AutoCloseEnabled {
			return left.Decision.AutoCloseEnabled
		}

		leftClose := left.Execution.TargetCloseTimeMs
		rightClose := right.Execution.TargetCloseTimeMs
		switch {
		case leftClose > 0 && rightClose > 0 && leftClose != rightClose:
			return leftClose < rightClose
		case leftClose > 0 && rightClose <= 0:
			return true
		case leftClose <= 0 && rightClose > 0:
			return false
		}

		leftUpdated := left.Execution.UpdatedAt.UnixMilli()
		rightUpdated := right.Execution.UpdatedAt.UnixMilli()
		if leftUpdated != rightUpdated {
			return leftUpdated > rightUpdated
		}
		return left.Execution.PlanKey < right.Execution.PlanKey
	})
}

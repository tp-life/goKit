package persistence

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"goKit/internal/domain/entity"
	"goKit/pkg/kit/db"
)

func newTestDBClient(t *testing.T) *db.Client {
	t.Helper()

	client, err := db.NewClient(db.Config{
		Driver:       "sqlite",
		DSN:          filepath.Join(t.TempDir(), "test.db"),
		MaxIdleConns: 1,
		MaxOpenConns: 1,
		LogMode:      "silent",
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("expected test db client to initialize, got %v", err)
	}
	if err := AutoMigrate(client); err != nil {
		t.Fatalf("expected automigrate to succeed, got %v", err)
	}
	return client
}

func TestExecutionRepo_UpsertPersistsTransitionMetadata(t *testing.T) {
	ctx := context.Background()
	repo := &ExecutionRepo{client: newTestDBClient(t)}

	item := &entity.ExecutionRecord{
		PlanKey:             "plan-1",
		Status:              "pending_open",
		LastTransitionAtMs:  111,
		LastTransitionEvent: "open_requested",
		StatusReason:        "waiting for legs",
	}
	if err := repo.Upsert(ctx, item); err != nil {
		t.Fatalf("expected initial upsert to succeed, got %v", err)
	}

	item.Status = "opened"
	item.LastTransitionAtMs = 222
	item.LastTransitionEvent = "open_results_applied"
	item.StatusReason = "both legs filled"
	if err := repo.Upsert(ctx, item); err != nil {
		t.Fatalf("expected second upsert to succeed, got %v", err)
	}

	got, err := repo.FindByPlanKey(ctx, "plan-1")
	if err != nil {
		t.Fatalf("expected lookup to succeed, got %v", err)
	}
	if got == nil {
		t.Fatal("expected execution record to exist")
	}
	if got.LastTransitionAtMs != 222 {
		t.Fatalf("expected last_transition_at_ms to persist, got %d", got.LastTransitionAtMs)
	}
	if got.LastTransitionEvent != "open_results_applied" {
		t.Fatalf("expected last_transition_event to persist, got %s", got.LastTransitionEvent)
	}
	if got.StatusReason != "both legs filled" {
		t.Fatalf("expected status_reason to persist, got %s", got.StatusReason)
	}
}

func TestExecutionRepo_TryClaimActionClaimsNewRecordOnlyOnce(t *testing.T) {
	ctx := context.Background()
	repo := &ExecutionRepo{client: newTestDBClient(t)}

	requested := &entity.ExecutionRecord{
		PlanKey:             "plan-claim-open",
		Status:              "pending_open",
		LastTransitionAtMs:  111,
		LastTransitionEvent: "open_requested",
		StatusReason:        "claiming live open",
	}
	got, claimed, err := repo.TryClaimAction(ctx, requested, []string{""})
	if err != nil {
		t.Fatalf("expected first claim to succeed, got %v", err)
	}
	if !claimed {
		t.Fatal("expected first claim attempt to claim new record")
	}
	if got == nil || got.Status != "pending_open" {
		t.Fatalf("expected claimed record to persist pending_open, got %#v", got)
	}

	got, claimed, err = repo.TryClaimAction(ctx, &entity.ExecutionRecord{
		PlanKey:             "plan-claim-open",
		Status:              "pending_open",
		LastTransitionEvent: "open_requested",
		StatusReason:        "duplicate claim",
	}, []string{""})
	if err != nil {
		t.Fatalf("expected duplicate claim attempt to return current record, got %v", err)
	}
	if claimed {
		t.Fatal("expected duplicate claim attempt to be rejected")
	}
	if got == nil || got.Status != "pending_open" {
		t.Fatalf("expected duplicate claim to return existing pending record, got %#v", got)
	}
}

func TestExecutionRepo_TryClaimActionUpdatesAllowedExistingStatus(t *testing.T) {
	ctx := context.Background()
	repo := &ExecutionRepo{client: newTestDBClient(t)}

	if err := repo.Upsert(ctx, &entity.ExecutionRecord{
		PlanKey:             "plan-claim-close",
		Status:              "opened",
		LastTransitionEvent: "open_results_applied",
		StatusReason:        "position live",
	}); err != nil {
		t.Fatalf("expected seed record to upsert, got %v", err)
	}

	got, claimed, err := repo.TryClaimAction(ctx, &entity.ExecutionRecord{
		PlanKey:             "plan-claim-close",
		Status:              "pending_close",
		LastTransitionAtMs:  222,
		LastTransitionEvent: "close_requested",
		StatusReason:        "manual close accepted",
	}, []string{"opened"})
	if err != nil {
		t.Fatalf("expected allowed close claim to succeed, got %v", err)
	}
	if !claimed {
		t.Fatal("expected allowed close claim to update existing record")
	}
	if got == nil || got.Status != "pending_close" {
		t.Fatalf("expected claim result to move record into pending_close, got %#v", got)
	}

	persisted, err := repo.FindByPlanKey(ctx, "plan-claim-close")
	if err != nil {
		t.Fatalf("expected lookup after claim to succeed, got %v", err)
	}
	if persisted == nil {
		t.Fatal("expected claimed record to exist")
	}
	if persisted.Status != "pending_close" {
		t.Fatalf("expected persisted status pending_close, got %s", persisted.Status)
	}
	if persisted.LastTransitionEvent != "close_requested" {
		t.Fatalf("expected persisted transition event close_requested, got %s", persisted.LastTransitionEvent)
	}
	if persisted.StatusReason != "manual close accepted" {
		t.Fatalf("expected persisted status reason to update, got %s", persisted.StatusReason)
	}
}

func TestExecutionPlanRepo_SaveBatchUpsertsPenaltyFields(t *testing.T) {
	ctx := context.Background()
	repo := &ExecutionPlanRepo{client: newTestDBClient(t)}

	item := entity.ExecutionPlan{
		PlanKey:                "plan-penalty",
		Symbol:                 "BTC",
		Status:                 "ready",
		EntryPenaltyBps:        1.1,
		ExitPenaltyBps:         1.2,
		HedgePenaltyBps:        0.3,
		ExecutionPenaltyBps:    2.6,
		ExecutionPenaltyModel:  "model-v1",
		ExecutionPenaltyBucket: "normal",
	}
	if err := repo.SaveBatch(ctx, "batch-1", "opp-1", []entity.ExecutionPlan{item}); err != nil {
		t.Fatalf("expected initial save batch to succeed, got %v", err)
	}

	item.EntryPenaltyBps = 3.1
	item.ExitPenaltyBps = 3.2
	item.HedgePenaltyBps = 0.8
	item.ExecutionPenaltyBps = 7.1
	item.ExecutionPenaltyModel = "model-v2"
	item.ExecutionPenaltyBucket = "hot"
	if err := repo.SaveBatch(ctx, "batch-2", "opp-2", []entity.ExecutionPlan{item}); err != nil {
		t.Fatalf("expected second save batch to succeed, got %v", err)
	}

	got, err := repo.FindByPlanKey(ctx, "plan-penalty")
	if err != nil {
		t.Fatalf("expected lookup to succeed, got %v", err)
	}
	if got == nil {
		t.Fatal("expected execution plan to exist")
	}
	if got.EntryPenaltyBps != 3.1 || got.ExitPenaltyBps != 3.2 || got.HedgePenaltyBps != 0.8 {
		t.Fatalf("expected penalty bps fields to upsert, got entry=%.4f exit=%.4f hedge=%.4f", got.EntryPenaltyBps, got.ExitPenaltyBps, got.HedgePenaltyBps)
	}
	if got.ExecutionPenaltyBps != 7.1 {
		t.Fatalf("expected execution penalty bps to upsert, got %.4f", got.ExecutionPenaltyBps)
	}
	if got.ExecutionPenaltyModel != "model-v2" || got.ExecutionPenaltyBucket != "hot" {
		t.Fatalf("expected execution penalty metadata to upsert, got model=%s bucket=%s", got.ExecutionPenaltyModel, got.ExecutionPenaltyBucket)
	}
}

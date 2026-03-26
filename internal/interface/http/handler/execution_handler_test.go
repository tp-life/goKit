package handler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goKit/internal/application/service"
	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
	"goKit/internal/interface/http/middleware"

	"github.com/gofiber/fiber/v2"
)

type executionHandlerTestOrderRepo struct {
	items []entity.OrderRecord
}

type executionHandlerTestExecRepo struct {
	items []entity.ExecutionRecord
}

type executionHandlerTestPlanRepo struct {
	items []entity.ExecutionPlan
}

func (r *executionHandlerTestOrderRepo) Create(_ context.Context, item *entity.OrderRecord) error {
	if item != nil {
		r.items = append(r.items, *item)
	}
	return nil
}

func (r *executionHandlerTestOrderRepo) Update(_ context.Context, item *entity.OrderRecord) error {
	for i := range r.items {
		if r.items[i].ID == item.ID && item.ID != 0 {
			r.items[i] = *item
			return nil
		}
		if r.items[i].PlanKey == item.PlanKey &&
			r.items[i].Exchange == item.Exchange &&
			r.items[i].ClientOrderID == item.ClientOrderID &&
			r.items[i].Phase == item.Phase {
			r.items[i] = *item
			return nil
		}
	}
	return nil
}

func (r *executionHandlerTestOrderRepo) FindByExternalOrderID(_ context.Context, exchangeName, clientOrderID, venueOrderID string) (*entity.OrderRecord, error) {
	for i := range r.items {
		if r.items[i].Exchange != exchangeName {
			continue
		}
		if clientOrderID != "" && r.items[i].ClientOrderID == clientOrderID {
			cp := r.items[i]
			return &cp, nil
		}
		if venueOrderID != "" && r.items[i].VenueOrderID == venueOrderID {
			cp := r.items[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *executionHandlerTestOrderRepo) ListByPlanKey(_ context.Context, planKey string) ([]entity.OrderRecord, error) {
	out := make([]entity.OrderRecord, 0, len(r.items))
	for _, item := range r.items {
		if item.PlanKey == planKey {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *executionHandlerTestOrderRepo) ListLatest(_ context.Context, _ int) ([]entity.OrderRecord, error) {
	return append([]entity.OrderRecord(nil), r.items...), nil
}

func (r *executionHandlerTestExecRepo) Upsert(_ context.Context, item *entity.ExecutionRecord) error {
	if item == nil {
		return nil
	}
	for i := range r.items {
		if r.items[i].PlanKey == item.PlanKey {
			r.items[i] = *item
			return nil
		}
	}
	r.items = append(r.items, *item)
	return nil
}

func (r *executionHandlerTestExecRepo) TryClaimAction(_ context.Context, item *entity.ExecutionRecord, allowedCurrentStatuses []string) (*entity.ExecutionRecord, bool, error) {
	if item == nil {
		return nil, false, nil
	}
	allowed := make(map[string]struct{}, len(allowedCurrentStatuses))
	for _, status := range allowedCurrentStatuses {
		allowed[strings.ToLower(strings.TrimSpace(status))] = struct{}{}
	}
	for i := range r.items {
		if r.items[i].PlanKey != item.PlanKey {
			continue
		}
		current := r.items[i]
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(current.Status))]; !ok {
			cp := current
			return &cp, false, nil
		}
		if item.ID == 0 {
			item.ID = current.ID
		}
		r.items[i] = *item
		cp := r.items[i]
		return &cp, true, nil
	}
	if _, ok := allowed[""]; !ok {
		return nil, false, nil
	}
	if item.ID == 0 {
		item.ID = uint(len(r.items) + 1)
	}
	r.items = append(r.items, *item)
	cp := r.items[len(r.items)-1]
	return &cp, true, nil
}

func (r *executionHandlerTestExecRepo) FindByPlanKey(_ context.Context, planKey string) (*entity.ExecutionRecord, error) {
	for i := range r.items {
		if r.items[i].PlanKey == planKey {
			cp := r.items[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *executionHandlerTestExecRepo) ListLatest(_ context.Context, _ int) ([]entity.ExecutionRecord, error) {
	return append([]entity.ExecutionRecord(nil), r.items...), nil
}

func (r *executionHandlerTestExecRepo) ListActiveLive(_ context.Context) ([]entity.ExecutionRecord, error) {
	out := make([]entity.ExecutionRecord, 0, len(r.items))
	for _, item := range r.items {
		if item.LiveTrading {
			out = append(out, item)
		}
	}
	return out, nil
}

func (*executionHandlerTestPlanRepo) SaveBatch(context.Context, string, string, []entity.ExecutionPlan) error {
	return nil
}

func (*executionHandlerTestPlanRepo) ListLatest(context.Context, int) ([]entity.ExecutionPlan, error) {
	return nil, nil
}

func (*executionHandlerTestPlanRepo) ListByOpportunityBatch(context.Context, string, int) ([]entity.ExecutionPlan, error) {
	return nil, nil
}

func (r *executionHandlerTestPlanRepo) FindByPlanKey(_ context.Context, planKey string) (*entity.ExecutionPlan, error) {
	for i := range r.items {
		if r.items[i].PlanKey == planKey {
			cp := r.items[i]
			return &cp, nil
		}
	}
	return nil, nil
}

func newExecutionHandlerTestApp(orderRepo *executionHandlerTestOrderRepo, execRepo *executionHandlerTestExecRepo, planRepo *executionHandlerTestPlanRepo) *fiber.App {
	if planRepo == nil {
		planRepo = &executionHandlerTestPlanRepo{}
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	svc := service.NewExecutionService(service.ExecutionServiceParams{
		Cfg:       service.Config{},
		Logger:    logger,
		Store:     service.NewMarketStore(),
		PlanRepo:  planRepo,
		ExecRepo:  execRepo,
		OrderRepo: orderRepo,
		Trades:    []exchange.TradeAdapter{},
	})

	app := fiber.New()
	app.Use(middleware.ErrorHandler(logger))
	app.Get("/api/v1/executions/auto-close-candidates", NewExecutionHandler(svc).AutoCloseCandidates)
	app.Post("/api/v1/executions/:planKey/open", NewExecutionHandler(svc).Open)
	app.Post("/api/v1/executions/:planKey/close", NewExecutionHandler(svc).Close)
	app.Post("/api/v1/executions/auto-close-sweep", NewExecutionHandler(svc).SweepAutoClose)
	app.Post("/api/v1/executions/events/order", NewExecutionHandler(svc).InjectOrderEvent)
	return app
}

func TestAutoCloseCandidates_ReturnsInspection(t *testing.T) {
	execRepo := &executionHandlerTestExecRepo{
		items: []entity.ExecutionRecord{{
			PlanKey:     "plan-retry-close",
			Symbol:      "BTC",
			Status:      "close_failed",
			LiveTrading: true,
			AutoClose:   true,
		}},
	}
	planRepo := &executionHandlerTestPlanRepo{
		items: []entity.ExecutionPlan{{
			PlanKey:          "plan-retry-close",
			Symbol:           "BTC",
			LongExchange:     "binance",
			ShortExchange:    "aster",
			LongVenueSymbol:  "BTCUSDT",
			ShortVenueSymbol: "BTCUSDT",
			LongQty:          1,
			ShortQty:         1,
			LongEntryPrice:   100,
			ShortEntryPrice:  100,
		}},
	}
	app := newExecutionHandlerTestApp(&executionHandlerTestOrderRepo{}, execRepo, planRepo)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/executions/auto-close-candidates", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body struct {
		Code int                         `json:"code"`
		Data service.AutoCloseInspection `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("expected response body to be valid json: %v", err)
	}
	if body.Code != 0 {
		t.Fatalf("expected business code 0, got %d", body.Code)
	}
	if body.Data.Total != 1 || body.Data.ShouldClose != 1 {
		t.Fatalf("expected one retry-close candidate, got %+v", body.Data)
	}
}

func TestSweepAutoClose_ReturnsReport(t *testing.T) {
	execRepo := &executionHandlerTestExecRepo{
		items: []entity.ExecutionRecord{{
			PlanKey:     "plan-opened-no-auto-close",
			Symbol:      "BTC",
			Status:      "opened",
			LiveTrading: true,
			AutoClose:   false,
		}},
	}
	app := newExecutionHandlerTestApp(&executionHandlerTestOrderRepo{}, execRepo, &executionHandlerTestPlanRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions/auto-close-sweep", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body struct {
		Code int                          `json:"code"`
		Data service.AutoCloseSweepReport `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("expected response body to be valid json: %v", err)
	}
	if body.Code != 0 {
		t.Fatalf("expected business code 0, got %d", body.Code)
	}
	if body.Data.Total != 1 || body.Data.Attempted != 0 || body.Data.Skipped != 1 {
		t.Fatalf("expected no-op sweep report, got %+v", body.Data)
	}
}

func TestInjectOrderEvent_AcceptsSnakeCaseJSON(t *testing.T) {
	orderRepo := &executionHandlerTestOrderRepo{
		items: []entity.OrderRecord{{
			ID:            1,
			PlanKey:       "plan-1",
			Phase:         "open",
			LegRole:       "long_leg",
			Exchange:      "binance",
			ClientOrderID: "cid-1",
			Status:        "NEW",
		}},
	}
	execRepo := &executionHandlerTestExecRepo{
		items: []entity.ExecutionRecord{{
			PlanKey: "plan-1",
			Status:  "pending_open",
		}},
	}
	app := newExecutionHandlerTestApp(orderRepo, execRepo, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions/events/order", strings.NewReader(`{
		"source":"debug_http",
		"exchange":"binance",
		"client_order_id":"cid-1",
		"status":"FILLED",
		"executed_qty":1,
		"average_price":101.5,
		"terminal":true,
		"occurred_at_ms":1710000000123
	}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to succeed, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("expected response body to be valid json: %v", err)
	}
	if code, _ := body["code"].(float64); code != 0 {
		t.Fatalf("expected business code 0, got %v", body["code"])
	}
	if orderRepo.items[0].Status != "FILLED" {
		t.Fatalf("expected order status to be updated to FILLED, got %s", orderRepo.items[0].Status)
	}
	if orderRepo.items[0].ExecutedQty != 1 {
		t.Fatalf("expected executed qty to be updated to 1, got %v", orderRepo.items[0].ExecutedQty)
	}
	if execRepo.items[0].Status != "opened" {
		t.Fatalf("expected execution status to advance to opened, got %s", execRepo.items[0].Status)
	}
	if execRepo.items[0].LastTransitionEvent != "debug_http_order_filled" {
		t.Fatalf("expected last transition event to track debug_http input, got %s", execRepo.items[0].LastTransitionEvent)
	}
}

func TestInjectOrderEvent_NotFoundReturns404(t *testing.T) {
	app := newExecutionHandlerTestApp(&executionHandlerTestOrderRepo{}, &executionHandlerTestExecRepo{}, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions/events/order", strings.NewReader(`{
		"exchange":"binance",
		"client_order_id":"missing-order",
		"status":"FILLED"
	}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}

	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("expected response body to be valid json: %v", err)
	}
	if code, _ := body["code"].(float64); code != 40400 {
		t.Fatalf("expected not found business code 40400, got %v", body["code"])
	}
}

func TestOpen_MissingPlanReturns404(t *testing.T) {
	app := newExecutionHandlerTestApp(&executionHandlerTestOrderRepo{}, &executionHandlerTestExecRepo{}, &executionHandlerTestPlanRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions/missing-plan/open", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestClose_MissingPlanReturns404(t *testing.T) {
	app := newExecutionHandlerTestApp(&executionHandlerTestOrderRepo{}, &executionHandlerTestExecRepo{}, &executionHandlerTestPlanRepo{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions/missing-plan/close", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestOpen_InFlightExecutionReturns409(t *testing.T) {
	app := newExecutionHandlerTestApp(
		&executionHandlerTestOrderRepo{},
		&executionHandlerTestExecRepo{
			items: []entity.ExecutionRecord{{
				PlanKey: "plan-pending-open",
				Status:  "pending_open",
			}},
		},
		&executionHandlerTestPlanRepo{
			items: []entity.ExecutionPlan{{
				PlanKey: "plan-pending-open",
				Symbol:  "BTC",
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions/plan-pending-open/open", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}
}

func TestClose_InFlightExecutionReturns409(t *testing.T) {
	app := newExecutionHandlerTestApp(
		&executionHandlerTestOrderRepo{},
		&executionHandlerTestExecRepo{
			items: []entity.ExecutionRecord{{
				PlanKey: "plan-pending-close",
				Status:  "pending_close",
			}},
		},
		&executionHandlerTestPlanRepo{
			items: []entity.ExecutionPlan{{
				PlanKey: "plan-pending-close",
				Symbol:  "BTC",
			}},
		},
	)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/executions/plan-pending-close/close", nil)
	resp, err := app.Test(req)
	if err != nil {
		t.Fatalf("expected request to complete, got error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", resp.StatusCode)
	}
}

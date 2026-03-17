package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"

	"go.uber.org/fx"
)

type ExecutionServiceParams struct {
	fx.In

	Cfg       Config
	Logger    *slog.Logger
	Store     *MarketStore
	PlanRepo  repository.ExecutionPlanRepository
	ExecRepo  repository.ExecutionRepository
	OrderRepo repository.OrderRepository
	Trades    []exchange.TradeAdapter `group:"trades"`
}

type ExecutionService struct {
	cfg       Config
	logger    *slog.Logger
	store     *MarketStore
	planRepo  repository.ExecutionPlanRepository
	execRepo  repository.ExecutionRepository
	orderRepo repository.OrderRepository
	trades    map[string]exchange.TradeAdapter
}

func NewExecutionService(p ExecutionServiceParams) *ExecutionService {
	return &ExecutionService{
		cfg:       p.Cfg.normalize(),
		logger:    p.Logger,
		store:     p.Store,
		planRepo:  p.PlanRepo,
		execRepo:  p.ExecRepo,
		orderRepo: p.OrderRepo,
		trades:    exchange.BuildTradeMap(p.Trades),
	}
}

func StartExecutionEngine(lc fx.Lifecycle, svc *ExecutionService) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			runCtx, c := context.WithCancel(context.Background())
			cancel = c
			go svc.loop(runCtx)
			return nil
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			return nil
		},
	})
}

func (s *ExecutionService) loop(ctx context.Context) {
	if !s.cfg.Enabled {
		return
	}
	ticker := time.NewTicker(s.cfg.Execution.LoopInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if s.cfg.Execution.AutoEntry {
				s.runAutoOpen(ctx)
			}
			if s.cfg.Execution.AutoClose {
				s.runAutoClose(ctx)
			}
		}
	}
}

func (s *ExecutionService) runAutoOpen(ctx context.Context) {
	plans, err := s.planRepo.ListLatest(ctx, s.cfg.Execution.MaxLatestPlans)
	if err != nil {
		s.logger.Error("execution_auto_open_list_plans_failed", slog.Any("err", err))
		return
	}
	for _, plan := range plans {
		if !plan.ReadyNow || strings.ToLower(plan.Status) != "ready" {
			continue
		}
		rec, _ := s.execRepo.FindByPlanKey(ctx, plan.PlanKey)
		if rec != nil {
			continue
		}
		if _, err := s.openPlan(ctx, &plan, "auto", s.cfg.Execution.Enabled); err != nil {
			s.logger.Error("execution_auto_open_failed", slog.String("plan_key", plan.PlanKey), slog.Any("err", err))
		}
	}
}

func (s *ExecutionService) runAutoClose(ctx context.Context) {
	records, err := s.execRepo.ListLatest(ctx, s.cfg.Execution.MaxLatestPlans)
	if err != nil {
		s.logger.Error("execution_auto_close_list_records_failed", slog.Any("err", err))
		return
	}
	nowMs := time.Now().UnixMilli()
	for _, rec := range records {
		status := strings.ToLower(rec.Status)
		if status != "opened" && status != "dry_run_opened" && status != "open_partial_failed" {
			continue
		}
		if !rec.AutoClose || rec.TargetCloseTimeMs <= 0 || nowMs < rec.TargetCloseTimeMs {
			continue
		}
		plan, err := s.planRepo.FindByPlanKey(ctx, rec.PlanKey)
		if err != nil {
			s.logger.Error("execution_auto_close_load_plan_failed", slog.String("plan_key", rec.PlanKey), slog.Any("err", err))
			continue
		}
		if _, err := s.closePlan(ctx, plan, "auto", s.cfg.Execution.Enabled); err != nil {
			s.logger.Error("execution_auto_close_failed", slog.String("plan_key", rec.PlanKey), slog.Any("err", err))
		}
	}
}

func (s *ExecutionService) ListLatest(ctx context.Context, limit int) ([]entity.ExecutionRecord, error) {
	return s.execRepo.ListLatest(ctx, limit)
}

func (s *ExecutionService) ListOrdersByPlanKey(ctx context.Context, planKey string) ([]entity.OrderRecord, error) {
	return s.orderRepo.ListByPlanKey(ctx, planKey)
}

func (s *ExecutionService) OpenByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionRecord, error) {
	plan, err := s.planRepo.FindByPlanKey(ctx, planKey)
	if err != nil {
		return nil, err
	}
	return s.openPlan(ctx, plan, "manual", s.cfg.Execution.Enabled)
}

func (s *ExecutionService) CloseByPlanKey(ctx context.Context, planKey string) (*entity.ExecutionRecord, error) {
	plan, err := s.planRepo.FindByPlanKey(ctx, planKey)
	if err != nil {
		return nil, err
	}
	return s.closePlan(ctx, plan, "manual", s.cfg.Execution.Enabled)
}

func (s *ExecutionService) openPlan(ctx context.Context, plan *entity.ExecutionPlan, trigger string, live bool) (*entity.ExecutionRecord, error) {
	if plan == nil {
		return nil, fmt.Errorf("nil execution plan")
	}
	rec, _ := s.execRepo.FindByPlanKey(ctx, plan.PlanKey)
	if rec != nil {
		status := strings.ToLower(rec.Status)
		if status == "opened" || status == "dry_run_opened" || status == "closed" || status == "dry_run_closed" {
			return rec, nil
		}
	}
	if rec == nil {
		rec = &entity.ExecutionRecord{
			PlanKey:            plan.PlanKey,
			BatchID:            plan.BatchID,
			OpportunityBatchID: plan.OpportunityBatchID,
			Symbol:             plan.Symbol,
			LongExchange:       plan.LongExchange,
			ShortExchange:      plan.ShortExchange,
			LiveTrading:        live,
			AutoClose:          s.cfg.Execution.AutoClose,
			TargetCloseTimeMs:  plan.TargetCloseTimeMs,
		}
	}
	if !live {
		rec.Status = "dry_run_opened"
		rec.OpenedAtMs = time.Now().UnixMilli()
		rec.LastError = ""
		if err := s.execRepo.Upsert(ctx, rec); err != nil {
			return nil, err
		}
		return rec, nil
	}

	results, errMsg := s.placePlanOrders(ctx, plan, "open", trigger)
	rec.OpenedAtMs = time.Now().UnixMilli()
	rec.OpenOrderCount = len(results)
	rec.LastError = errMsg
	rec.Status = summarizeExecutionStatus(results, "open")
	if err := s.execRepo.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	if errMsg != "" {
		return rec, fmt.Errorf("%s", errMsg)
	}
	return rec, nil
}

func (s *ExecutionService) closePlan(ctx context.Context, plan *entity.ExecutionPlan, trigger string, live bool) (*entity.ExecutionRecord, error) {
	if plan == nil {
		return nil, fmt.Errorf("nil execution plan")
	}
	rec, _ := s.execRepo.FindByPlanKey(ctx, plan.PlanKey)
	if rec == nil {
		rec = &entity.ExecutionRecord{
			PlanKey:            plan.PlanKey,
			BatchID:            plan.BatchID,
			OpportunityBatchID: plan.OpportunityBatchID,
			Symbol:             plan.Symbol,
			LongExchange:       plan.LongExchange,
			ShortExchange:      plan.ShortExchange,
			LiveTrading:        live,
			AutoClose:          s.cfg.Execution.AutoClose,
			TargetCloseTimeMs:  plan.TargetCloseTimeMs,
		}
	}
	if !live {
		rec.Status = "dry_run_closed"
		rec.ClosedAtMs = time.Now().UnixMilli()
		rec.LastError = ""
		if err := s.execRepo.Upsert(ctx, rec); err != nil {
			return nil, err
		}
		return rec, nil
	}

	results, errMsg := s.placePlanOrders(ctx, plan, "close", trigger)
	rec.ClosedAtMs = time.Now().UnixMilli()
	rec.CloseOrderCount += len(results)
	rec.LastError = errMsg
	rec.Status = summarizeExecutionStatus(results, "close")
	if err := s.execRepo.Upsert(ctx, rec); err != nil {
		return nil, err
	}
	if errMsg != "" {
		return rec, fmt.Errorf("%s", errMsg)
	}
	return rec, nil
}

func summarizeExecutionStatus(results []entity.OrderRecord, phase string) string {
	if len(results) == 0 {
		return phase + "_skipped"
	}
	success := 0
	for _, item := range results {
		if item.ErrorMessage == "" {
			success++
		}
	}
	switch {
	case success == len(results) && phase == "open":
		return "opened"
	case success == len(results) && phase == "close":
		return "closed"
	case success > 0:
		return phase + "_partial_failed"
	default:
		return phase + "_failed"
	}
}

func (s *ExecutionService) placePlanOrders(ctx context.Context, plan *entity.ExecutionPlan, phase, trigger string) ([]entity.OrderRecord, string) {
	legs := []struct {
		role        string
		exchange    string
		side        string
		venueSymbol string
		qty         float64
		price       float64
	}{
		{role: "long_leg", exchange: plan.LongExchange, side: ternarySide(phase == "open", "BUY", "SELL"), venueSymbol: plan.LongVenueSymbol, qty: plan.LongQty, price: plan.LongEntryPrice},
		{role: "short_leg", exchange: plan.ShortExchange, side: ternarySide(phase == "open", "SELL", "BUY"), venueSymbol: plan.ShortVenueSymbol, qty: plan.ShortQty, price: plan.ShortEntryPrice},
	}

	type successfulLeg struct {
		role     string
		exchange string
		req      exchange.TradeOrderRequest
	}

	results := make([]entity.OrderRecord, 0, 2)
	errors := make([]string, 0, 2)
	successes := make([]successfulLeg, 0, 2)
	for _, leg := range legs {
		adapter := s.trades[strings.ToLower(leg.exchange)]
		meta, _ := s.store.Symbol(leg.exchange, plan.Symbol)
		book, _ := s.store.LatestBookTop(leg.exchange, plan.Symbol)
		req := s.buildTradeRequest(plan, phase, leg.role, leg.side, meta, book, leg.qty, leg.price)
		orderRecord := entity.OrderRecord{
			PlanKey:         plan.PlanKey,
			ExecutionStatus: phase,
			Phase:           phase,
			LegRole:         leg.role,
			Exchange:        leg.exchange,
			Symbol:          plan.Symbol,
			VenueSymbol:     req.VenueSymbol,
			ClientOrderID:   req.ClientOrderID,
			Side:            req.Side,
			OrderType:       req.OrderType,
			TimeInForce:     req.TimeInForce,
			ReduceOnly:      req.ReduceOnly,
			RequestedQty:    req.Quantity,
			RequestedPrice:  req.Price,
			Status:          "PENDING",
		}
		if adapter == nil || !adapter.Enabled() {
			orderRecord.Status = "SKIPPED"
			orderRecord.ErrorMessage = fmt.Sprintf("trade adapter %s disabled or missing credentials", leg.exchange)
			errors = append(errors, orderRecord.ErrorMessage)
			_ = s.orderRepo.Create(ctx, &orderRecord)
			results = append(results, orderRecord)
			continue
		}

		var resp exchange.TradeOrderResult
		var err error
		if phase == "open" {
			resp, err = adapter.PlaceOrder(ctx, req)
		} else {
			resp, err = adapter.ClosePosition(ctx, req)
		}
		if err != nil {
			orderRecord.Status = "ERROR"
			orderRecord.ErrorMessage = err.Error()
			errors = append(errors, fmt.Sprintf("%s:%s", leg.exchange, err.Error()))
		} else {
			orderRecord.Status = pickNonEmpty(resp.Status, "SUBMITTED")
			orderRecord.VenueOrderID = resp.VenueOrderID
			orderRecord.ExecutedQty = resp.ExecutedQty
			orderRecord.AvgPrice = resp.AveragePrice
			orderRecord.RawResponse = resp.RawResponse
		}
		_ = s.orderRepo.Create(ctx, &orderRecord)
		results = append(results, orderRecord)
		if phase == "open" && orderRecord.ErrorMessage == "" && !strings.EqualFold(orderRecord.Status, "ERROR") && !strings.EqualFold(orderRecord.Status, "SKIPPED") {
			successes = append(successes, successfulLeg{role: leg.role, exchange: leg.exchange, req: req})
		}
		s.logger.Info("execution_order_placed",
			slog.String("plan_key", plan.PlanKey),
			slog.String("phase", phase),
			slog.String("trigger", trigger),
			slog.String("exchange", leg.exchange),
			slog.String("symbol", plan.Symbol),
			slog.String("side", req.Side),
			slog.Float64("qty", req.Quantity),
		)
	}

	if phase == "open" && len(errors) > 0 && len(successes) > 0 {
		s.logger.Warn("execution_open_partial_failure_hedge_start",
			slog.String("plan_key", plan.PlanKey),
			slog.Int("successful_legs", len(successes)),
			slog.Int("error_legs", len(errors)),
		)
		for _, okLeg := range successes {
			adapter := s.trades[strings.ToLower(okLeg.exchange)]
			hedgeReq := okLeg.req
			hedgeReq.ClientOrderID = buildClientOrderID(plan, "hedge", okLeg.role)
			hedgeReq.Reason = "open_leg_failed_hedge"
			hedgeReq.OrderType = "MARKET"
			hedgeReq.ReduceOnly = true
			hedgeReq.TimeInForce = "IOC"
			hedgeResp := exchange.TradeOrderResult{}
			hedgeErr := error(nil)
			rec := entity.OrderRecord{
				PlanKey:         plan.PlanKey,
				ExecutionStatus: "hedge_close",
				Phase:           "hedge_close",
				LegRole:         okLeg.role,
				Exchange:        okLeg.exchange,
				Symbol:          plan.Symbol,
				VenueSymbol:     hedgeReq.VenueSymbol,
				ClientOrderID:   hedgeReq.ClientOrderID,
				Side:            hedgeReq.Side,
				OrderType:       hedgeReq.OrderType,
				TimeInForce:     hedgeReq.TimeInForce,
				ReduceOnly:      true,
				RequestedQty:    hedgeReq.Quantity,
				RequestedPrice:  hedgeReq.Price,
				Status:          "PENDING",
			}
			if adapter == nil || !adapter.Enabled() {
				rec.Status = "SKIPPED"
				rec.ErrorMessage = fmt.Sprintf("hedge trade adapter %s disabled", okLeg.exchange)
				errors = append(errors, rec.ErrorMessage)
			} else {
				hedgeResp, hedgeErr = adapter.ClosePosition(ctx, hedgeReq)
				if hedgeErr != nil {
					rec.Status = "ERROR"
					rec.ErrorMessage = hedgeErr.Error()
					errors = append(errors, fmt.Sprintf("hedge:%s:%s", okLeg.exchange, hedgeErr.Error()))
				} else {
					rec.Status = pickNonEmpty(hedgeResp.Status, "SUBMITTED")
					rec.VenueOrderID = hedgeResp.VenueOrderID
					rec.ExecutedQty = hedgeResp.ExecutedQty
					rec.AvgPrice = hedgeResp.AveragePrice
					rec.RawResponse = hedgeResp.RawResponse
				}
			}
			_ = s.orderRepo.Create(ctx, &rec)
			results = append(results, rec)
		}
	}

	return results, strings.Join(errors, " | ")
}

func (s *ExecutionService) buildTradeRequest(plan *entity.ExecutionPlan, phase, legRole, side string, meta entity.Symbol, book entity.BookTopSnapshot, qty, refPrice float64) exchange.TradeOrderRequest {
	mode := plan.EntryMode
	if phase == "close" {
		mode = plan.ExitMode
	}
	mode = strings.ToLower(strings.TrimSpace(mode))
	orderType := "LIMIT"
	tif := "GTC"
	price := refPrice

	switch mode {
	case "maker":
		if strings.EqualFold(side, "BUY") {
			if book.BidPrice > 0 {
				price = book.BidPrice
			}
		} else {
			if book.AskPrice > 0 {
				price = book.AskPrice
			}
		}
		tif = makerTIF(meta.Exchange)
	case "taker":
		if strings.EqualFold(meta.Exchange, "hyperliquid") {
			orderType = "LIMIT"
			tif = "IOC"
			price = aggressivePrice(side, book, refPrice)
		} else {
			orderType = "MARKET"
			tif = ""
			price = 0
		}
	default: // mixed
		if strings.EqualFold(meta.Exchange, "hyperliquid") {
			orderType = "LIMIT"
			tif = "IOC"
			price = aggressivePrice(side, book, refPrice)
		} else {
			orderType = "LIMIT"
			tif = "IOC"
			price = aggressivePrice(side, book, refPrice)
		}
	}

	qty = roundDownStep(qty, meta.StepSize)
	return exchange.TradeOrderRequest{
		CanonicalSymbol: plan.Symbol,
		VenueSymbol:     meta.VenueSymbol,
		AssetID:         meta.VenueAssetID,
		Side:            side,
		OrderType:       orderType,
		TimeInForce:     tif,
		Quantity:        round8(qty),
		Price:           round8(price),
		ReduceOnly:      phase == "close",
		ClientOrderID:   buildClientOrderID(plan, phase, legRole),
		Reason:          phase,
	}
}

func aggressivePrice(side string, book entity.BookTopSnapshot, fallback float64) float64 {
	ref := fallback
	if strings.EqualFold(side, "BUY") {
		if book.AskPrice > 0 {
			ref = book.AskPrice
		}
		if ref <= 0 {
			ref = 1
		}
		return ref * 1.002
	}
	if book.BidPrice > 0 {
		ref = book.BidPrice
	}
	if ref <= 0 {
		ref = 1
	}
	return ref * 0.998
}

func makerTIF(exchangeName string) string {
	if strings.EqualFold(exchangeName, "hyperliquid") {
		return "ALO"
	}
	return "GTX"
}

func buildClientOrderID(plan *entity.ExecutionPlan, phase, legRole string) string {
	raw := fmt.Sprintf("%s-%s-%s-%s-%d", strings.ToLower(plan.Symbol), phase, legRole, plan.PlanKey[:10], time.Now().UnixMilli()%1_000_000)
	if len(raw) > 36 {
		return raw[:36]
	}
	return raw
}

func ternarySide(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

func pickNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

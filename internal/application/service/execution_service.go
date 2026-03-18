package service

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"

	"go.uber.org/fx"
)

const (
	executionStatePendingOpen  = "pending_open"
	executionStateOpened       = "opened"
	executionStateOpenPartial  = "open_partial_failed"
	executionStateOpenFailed   = "open_failed"
	executionStateOpenHedging  = "open_hedging"
	executionStatePendingClose = "pending_close"
	executionStateClosed       = "closed"
	executionStateClosePartial = "close_partial_failed"
	executionStateCloseFailed  = "close_failed"
	executionStateCloseHedging = "close_hedging"
	executionStateRiskBlocked  = "risk_blocked"
	executionStateCircuitOpen  = "api_circuit_open"
)

type exchangeFailureState struct {
	consecutive int
	openUntil   time.Time
}

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

	mu              sync.Mutex
	exchangeFailure map[string]exchangeFailureState
}

func NewExecutionService(p ExecutionServiceParams) *ExecutionService {
	return &ExecutionService{
		cfg:             p.Cfg.normalize(),
		logger:          p.Logger,
		store:           p.Store,
		planRepo:        p.PlanRepo,
		execRepo:        p.ExecRepo,
		orderRepo:       p.OrderRepo,
		trades:          exchange.BuildTradeMap(p.Trades),
		exchangeFailure: make(map[string]exchangeFailureState),
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
		if status != executionStateOpened && status != "dry_run_opened" && status != executionStateOpenPartial {
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
		if status == executionStateOpened || status == "dry_run_opened" || status == executionStateClosed || status == "dry_run_closed" {
			return rec, nil
		}
	}
	if rec == nil {
		rec = s.newExecutionRecord(plan, live)
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
	if err := s.enforceRiskControls(ctx, plan); err != nil {
		rec.Status = executionStateRiskBlocked
		rec.LastError = err.Error()
		_ = s.execRepo.Upsert(ctx, rec)
		return rec, err
	}
	rec.Status = executionStatePendingOpen
	if err := s.execRepo.Upsert(ctx, rec); err != nil {
		return nil, err
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
		rec = s.newExecutionRecord(plan, live)
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
	rec.Status = executionStatePendingClose
	if err := s.execRepo.Upsert(ctx, rec); err != nil {
		return nil, err
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

func (s *ExecutionService) newExecutionRecord(plan *entity.ExecutionPlan, live bool) *entity.ExecutionRecord {
	return &entity.ExecutionRecord{
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

func summarizeExecutionStatus(results []entity.OrderRecord, phase string) string {
	if len(results) == 0 {
		if phase == "open" {
			return executionStateOpenFailed
		}
		return executionStateCloseFailed
	}
	totalPrimary := 0
	successPrimary := 0
	hasHedge := false
	hasErr := false
	for _, item := range results {
		if strings.HasPrefix(item.Phase, "hedge") {
			hasHedge = true
			if item.ErrorMessage != "" || isFailedOrderStatus(item.Status) {
				hasErr = true
			}
			continue
		}
		totalPrimary++
		if item.ErrorMessage != "" || isFailedOrderStatus(item.Status) {
			hasErr = true
			continue
		}
		if isSuccessfulOrderStatus(item.Status, item.ExecutedQty) {
			successPrimary++
		} else {
			hasErr = true
		}
	}
	if phase == "open" {
		switch {
		case hasHedge:
			return executionStateOpenHedging
		case successPrimary == totalPrimary && !hasErr:
			return executionStateOpened
		case successPrimary > 0:
			return executionStateOpenPartial
		default:
			return executionStateOpenFailed
		}
	}
	switch {
	case hasHedge:
		return executionStateCloseHedging
	case successPrimary == totalPrimary && !hasErr:
		return executionStateClosed
	case successPrimary > 0:
		return executionStateClosePartial
	default:
		return executionStateCloseFailed
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

	results := make([]entity.OrderRecord, 0, 4)
	errors := make([]string, 0, 4)
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
		if err := s.ensureExchangeAvailable(leg.exchange); err != nil {
			orderRecord.Status = "CIRCUIT_OPEN"
			orderRecord.ErrorMessage = err.Error()
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
			s.registerAPIFailure(leg.exchange)
			orderRecord.Status = "ERROR"
			orderRecord.ErrorMessage = err.Error()
			errors = append(errors, fmt.Sprintf("%s:%s", leg.exchange, err.Error()))
		} else {
			s.registerAPISuccess(leg.exchange)
			orderRecord.Status = pickNonEmpty(resp.Status, "SUBMITTED")
			orderRecord.VenueOrderID = resp.VenueOrderID
			orderRecord.ExecutedQty = resp.ExecutedQty
			orderRecord.AvgPrice = resp.AveragePrice
			orderRecord.RawResponse = resp.RawResponse
			orderRecord = s.reconcileOrder(ctx, adapter, orderRecord, req)
		}
		_ = s.orderRepo.Create(ctx, &orderRecord)
		results = append(results, orderRecord)
		if phase == "open" && orderRecord.ErrorMessage == "" && isSuccessfulOrderStatus(orderRecord.Status, orderRecord.ExecutedQty) {
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
			slog.String("status", orderRecord.Status),
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
			hedgeReq.Side = reverseSide(hedgeReq.Side)
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
					s.registerAPIFailure(okLeg.exchange)
					rec.Status = "ERROR"
					rec.ErrorMessage = hedgeErr.Error()
					errors = append(errors, fmt.Sprintf("hedge:%s:%s", okLeg.exchange, hedgeErr.Error()))
				} else {
					s.registerAPISuccess(okLeg.exchange)
					rec.Status = pickNonEmpty(hedgeResp.Status, "SUBMITTED")
					rec.VenueOrderID = hedgeResp.VenueOrderID
					rec.ExecutedQty = hedgeResp.ExecutedQty
					rec.AvgPrice = hedgeResp.AveragePrice
					rec.RawResponse = hedgeResp.RawResponse
					rec = s.reconcileOrder(ctx, adapter, rec, hedgeReq)
				}
			}
			_ = s.orderRepo.Create(ctx, &rec)
			results = append(results, rec)
		}
	}

	return results, strings.Join(errors, " | ")
}

func (s *ExecutionService) reconcileOrder(ctx context.Context, adapter exchange.TradeAdapter, rec entity.OrderRecord, req exchange.TradeOrderRequest) entity.OrderRecord {
	attempts := s.cfg.Execution.OrderStatusPollAttempts
	for i := 0; i < attempts; i++ {
		status, err := adapter.GetOrderStatus(ctx, exchange.OrderLookupRequest{
			CanonicalSymbol: req.CanonicalSymbol,
			VenueSymbol:     req.VenueSymbol,
			AssetID:         req.AssetID,
			ClientOrderID:   rec.ClientOrderID,
			VenueOrderID:    rec.VenueOrderID,
		})
		if err == nil {
			rec.Status = pickNonEmpty(status.Status, rec.Status)
			rec.VenueOrderID = pickNonEmpty(status.VenueOrderID, rec.VenueOrderID)
			rec.ExecutedQty = maxFloat(rec.ExecutedQty, status.ExecutedQty)
			rec.AvgPrice = maxFloat(rec.AvgPrice, status.AveragePrice)
			rec.RawResponse = pickNonEmpty(status.RawResponse, rec.RawResponse)
			if status.Terminal || isSuccessfulOrderStatus(rec.Status, rec.ExecutedQty) {
				return rec
			}
		}
		if i < attempts-1 {
			time.Sleep(s.cfg.Execution.OrderStatusPollInterval)
		}
	}
	pos, err := adapter.GetPosition(ctx, req.CanonicalSymbol, req.VenueSymbol, req.AssetID)
	if err == nil {
		if req.ReduceOnly {
			if math.Abs(pos.Quantity) < req.Quantity*0.2 {
				rec.Status = "FILLED"
				rec.ExecutedQty = maxFloat(rec.ExecutedQty, req.Quantity)
			}
		} else if math.Abs(pos.Quantity) >= req.Quantity*0.8 {
			rec.Status = "FILLED"
			rec.ExecutedQty = maxFloat(rec.ExecutedQty, req.Quantity)
		}
	}
	if rec.ExecutedQty > 0 && !isSuccessfulOrderStatus(rec.Status, rec.ExecutedQty) {
		rec.Status = "PARTIALLY_FILLED"
	}
	return rec
}

func (s *ExecutionService) enforceRiskControls(ctx context.Context, plan *entity.ExecutionPlan) error {
	legs := []struct {
		exchange string
		venue    string
		assetID  string
		qty      float64
		price    float64
	}{
		{exchange: plan.LongExchange, venue: plan.LongVenueSymbol, qty: plan.LongQty, price: plan.LongEntryPrice},
		{exchange: plan.ShortExchange, venue: plan.ShortVenueSymbol, qty: plan.ShortQty, price: plan.ShortEntryPrice},
	}
	var symbolExposure float64
	exchangeExposure := map[string]float64{}
	for _, leg := range legs {
		adapter := s.trades[strings.ToLower(leg.exchange)]
		if adapter == nil || !adapter.Enabled() {
			return fmt.Errorf("risk check: trade adapter %s disabled", leg.exchange)
		}
		if err := s.ensureExchangeAvailable(leg.exchange); err != nil {
			return err
		}
		acct, err := adapter.GetAccountSnapshot(ctx)
		if err != nil {
			s.registerAPIFailure(leg.exchange)
			return fmt.Errorf("risk check account %s failed: %w", leg.exchange, err)
		}
		s.registerAPISuccess(leg.exchange)
		notional := math.Abs(leg.qty * leg.price)
		if s.cfg.Execution.MinAccountEquityUSDT > 0 && acct.Equity > 0 && acct.Equity < s.cfg.Execution.MinAccountEquityUSDT {
			return fmt.Errorf("risk check: %s equity %.4f below minimum %.4f", leg.exchange, acct.Equity, s.cfg.Execution.MinAccountEquityUSDT)
		}
		if acct.Equity > 0 && acct.AvailableBalance/acct.Equity < s.cfg.Execution.MinAvailableBalanceRatio {
			return fmt.Errorf("risk check: %s available ratio %.4f below minimum %.4f", leg.exchange, acct.AvailableBalance/acct.Equity, s.cfg.Execution.MinAvailableBalanceRatio)
		}
		if acct.AvailableBalance > 0 && acct.AvailableBalance < notional/math.Max(s.cfg.Leverage, 1) {
			return fmt.Errorf("risk check: %s available balance %.4f below required margin %.4f", leg.exchange, acct.AvailableBalance, notional/math.Max(s.cfg.Leverage, 1))
		}
		pos, err := adapter.GetPosition(ctx, plan.Symbol, leg.venue, leg.assetID)
		if err == nil {
			ref := firstPositive(pos.MarkPrice, pos.EntryPrice, leg.price)
			existing := math.Abs(pos.Quantity * ref)
			symbolExposure += existing + notional
			exchangeExposure[strings.ToLower(leg.exchange)] += existing + notional
		} else {
			symbolExposure += notional
			exchangeExposure[strings.ToLower(leg.exchange)] += notional
		}
	}
	if s.cfg.Execution.MaxSingleSymbolExposureUSDT > 0 && symbolExposure > s.cfg.Execution.MaxSingleSymbolExposureUSDT {
		return fmt.Errorf("risk check: symbol exposure %.4f exceeds limit %.4f", symbolExposure, s.cfg.Execution.MaxSingleSymbolExposureUSDT)
	}
	if s.cfg.Execution.MaxSingleExchangeExposureUSDT > 0 {
		for name, exposure := range exchangeExposure {
			if exposure > s.cfg.Execution.MaxSingleExchangeExposureUSDT {
				return fmt.Errorf("risk check: exchange %s exposure %.4f exceeds limit %.4f", name, exposure, s.cfg.Execution.MaxSingleExchangeExposureUSDT)
			}
		}
	}
	return nil
}

func (s *ExecutionService) ensureExchangeAvailable(exchangeName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.exchangeFailure[strings.ToLower(exchangeName)]
	if !state.openUntil.IsZero() && time.Now().Before(state.openUntil) {
		return fmt.Errorf("%s: %s until %s", executionStateCircuitOpen, exchangeName, state.openUntil.UTC().Format(time.RFC3339))
	}
	return nil
}

func (s *ExecutionService) registerAPIFailure(exchangeName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(exchangeName)
	state := s.exchangeFailure[key]
	state.consecutive++
	if state.consecutive >= s.cfg.Execution.APIFailureThreshold {
		state.openUntil = time.Now().Add(s.cfg.Execution.APIFailureCooldown)
		state.consecutive = 0
	}
	s.exchangeFailure[key] = state
}

func (s *ExecutionService) registerAPISuccess(exchangeName string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToLower(exchangeName)
	state := s.exchangeFailure[key]
	state.consecutive = 0
	state.openUntil = time.Time{}
	s.exchangeFailure[key] = state
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
	default:
		orderType = "LIMIT"
		tif = "IOC"
		price = aggressivePrice(side, book, refPrice)
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
	prefix := plan.PlanKey
	if len(prefix) > 10 {
		prefix = prefix[:10]
	}
	raw := fmt.Sprintf("%s-%s-%s-%s-%d", strings.ToLower(plan.Symbol), phase, legRole, prefix, time.Now().UnixMilli()%1_000_000)
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

func reverseSide(side string) string {
	switch strings.ToUpper(strings.TrimSpace(side)) {
	case "BUY":
		return "SELL"
	case "SELL":
		return "BUY"
	default:
		return side
	}
}

func pickNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func isSuccessfulOrderStatus(status string, executedQty float64) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FILLED", "NO_POSITION":
		return true
	case "PARTIALLY_FILLED":
		return executedQty > 0
	default:
		return executedQty > 0 && !isFailedOrderStatus(status)
	}
}

func isFailedOrderStatus(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "ERROR", "REJECTED", "SKIPPED", "EXPIRED", "CANCELED", "CANCELLED", "CIRCUIT_OPEN":
		return true
	default:
		return false
	}
}

func maxFloat(a, b float64) float64 {
	if b > a {
		return b
	}
	return a
}

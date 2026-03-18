package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/internal/infrastructure/exchange"

	"go.uber.org/fx"
)

const (
	OpportunityStatusEligible         = "eligible"
	OpportunityStatusWatching         = "watching"
	OpportunityStatusNotProfitable    = "not_profitable"
	OpportunityStatusSpreadTooSmall   = "spread_too_small"
	OpportunityStatusBasisTooWide     = "basis_too_wide"
	OpportunityStatusStaleData        = "stale_data"
	OpportunityStatusOutsideEntryWind = "outside_entry_window"
)

// fundingSnapshotMinPriceChangeBps 控制 funding 快照持久化的最低价格变化阈值。
// 这样可以避免价格极小波动导致数据库写入过于频繁，同时又不会让价格长期“卡住”。
const fundingSnapshotMinPriceChangeBps = 1.0

type StrategyRunnerParams struct {
	fx.In

	Cfg        Config
	Logger     *slog.Logger
	Store      *MarketStore
	SymbolRepo repository.SymbolRepository
	MarketRepo repository.MarketDataRepository
	OppRepo    repository.OpportunityRepository
	PlanRepo   repository.ExecutionPlanRepository
	Markets    []exchange.MarketAdapter `group:"markets"`
}

// coarseCandidate 是全市场 funding 粗筛阶段使用的轻量结构。
// 它只关心“这个 canonical symbol 是否值得进入盘口深扫池”，
// 不承载最终机会展示所需的完整净收益细节。
type coarseCandidate struct {
	Symbol string
	Score  float64
}

// fundingProjection 描述“在某个明确方向下，基于当前已知 funding 事件，
// 这条机会最值得持有到哪个真实结算点”。
//
// 重要：
// 1. 它不是把 funding 先小时化再线性外推；
// 2. 它只使用当前已经知道的下一次 fundingRate + fundingTime；
// 3. 它会在两个真实结算点（long 的下一次、short 的下一次）中，挑出单位收益更高的那个。
//
// 例如：
//
//	Binance: -0.01%, fundingTime = 1h 后
//	Aster:   -0.07%, fundingTime = 4h 后
//
// 对于方向“Long Aster / Short Binance”，候选真实结算点有两个：
//   - 持有到 1h：只会吃到 Binance 这一侧的下一次 funding
//   - 持有到 4h：会吃到 Binance 与 Aster 这两侧各自已知的下一次 funding
//
// 最终应该选哪一个结算点，由 CarryRate（真实 funding 收益率）来决定。
type fundingProjection struct {
	ProjectedFundingTimeMs       int64
	RequiredEntryByFundingTimeMs int64
	LongFundingEventCount        int
	ShortFundingEventCount       int
	FundingWindowHours           float64
	CarryRate                    float64
	CarryRateHourlyEquivalent    float64
	ComputationMode              string
}

// StrategyRunner 负责把“交易所原始市场数据”组织成三层流程：
// 1. 全市场基础池：所有至少在两家交易所同时存在的 canonical symbol。
// 2. Funding 粗筛池：使用 funding / mark 这类相对便宜的数据，为全市场打分。
// 3. 深扫池：核心币 + TopN 候选 + 冷门轮转池，只对这批币维护高频盘口并做精算机会。
//
// 这样做的目的，是在“不轻易漏掉机会”和“别把 websocket / DB 压爆”之间取得平衡。
type StrategyRunner struct {
	cfg        Config
	logger     *slog.Logger
	store      *MarketStore
	symbolRepo repository.SymbolRepository
	marketRepo repository.MarketDataRepository
	oppRepo    repository.OpportunityRepository
	planRepo   repository.ExecutionPlanRepository
	markets    map[string]exchange.MarketAdapter
	forecaster FundingForecaster

	lastFundingPersisted map[string]entity.FundingSnapshot
	lastBookPersisted    map[string]entity.BookTopSnapshot

	// fundingSymbolsByExchange 保存“全市场粗筛层”使用的 symbol 集合。
	// 这批 symbol 不要求都有盘口，只要求至少在两家交易所可比即可。
	fundingSymbolsByExchange map[string][]entity.Symbol
	// bookSymbolsByExchange 保存“当前深扫池”中、各交易所真正需要维护盘口的一组 symbol。
	bookSymbolsByExchange map[string][]entity.Symbol

	// 下面这几项是深扫池调度状态：
	// deepScanPinnedUntil: 候选进入深扫池后，至少保留一段时间，避免订阅来回抖动。
	// rotationCursor: 冷门轮转池的游标，保证所有 symbol 最终都能被抽查到。
	subscriptionMu      sync.RWMutex
	deepScanPinnedUntil map[string]time.Time
	rotationCursor      int
}

func NewStrategyRunner(p StrategyRunnerParams) *StrategyRunner {
	cfg := p.Cfg.normalize()
	return &StrategyRunner{
		cfg:                      cfg,
		logger:                   p.Logger,
		store:                    p.Store,
		symbolRepo:               p.SymbolRepo,
		marketRepo:               p.MarketRepo,
		oppRepo:                  p.OppRepo,
		planRepo:                 p.PlanRepo,
		markets:                  exchange.BuildMarketMap(p.Markets),
		forecaster:               NewFundingForecaster(cfg, p.MarketRepo),
		lastFundingPersisted:     make(map[string]entity.FundingSnapshot),
		lastBookPersisted:        make(map[string]entity.BookTopSnapshot),
		fundingSymbolsByExchange: make(map[string][]entity.Symbol),
		bookSymbolsByExchange:    make(map[string][]entity.Symbol),
		deepScanPinnedUntil:      make(map[string]time.Time),
	}
}

func StartStrategyRunner(lc fx.Lifecycle, runner *StrategyRunner) {
	var cancel context.CancelFunc
	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if !runner.cfg.Enabled {
				runner.logger.Info("strategy_runner_disabled")
				return nil
			}
			runCtx, c := context.WithCancel(context.Background())
			cancel = c
			return runner.Start(runCtx)
		},
		OnStop: func(ctx context.Context) error {
			if cancel != nil {
				cancel()
			}
			return nil
		},
	})
}

func (r *StrategyRunner) Start(ctx context.Context) error {
	allowed := make(map[string]struct{}, len(r.cfg.AllowedSymbols))
	for _, sym := range r.cfg.AllowedSymbols {
		key := strings.TrimSpace(strings.ToUpper(sym))
		if key == "" {
			continue
		}
		allowed[key] = struct{}{}
	}

	symbolsByExchange := make(map[string][]entity.Symbol)
	for name, market := range r.markets {
		if market == nil || !market.Enabled() {
			continue
		}
		items, err := market.FetchTradableSymbols(ctx, r.cfg.QuoteAsset, allowed)
		if err != nil {
			return fmt.Errorf("fetch %s symbols: %w", name, err)
		}
		symbolsByExchange[name] = items
		r.logger.Info("strategy_symbols_loaded", slog.String("exchange", name), slog.Int("count", len(items)))
	}

	// 先保留“各交易所自己实际可交易”的完整 symbol inventory，供数据库与排障使用。
	// 注意这里不要求跨所可比：哪怕某个币只在单一交易所存在，也应该能在 symbols 表里看到。
	//
	// 之前这里直接把 symbols 表写成了“至少在两家交易所同时存在的交集”，
	// 会造成一种很强的误导：
	// - 用户去数据库里看 Binance，只能看到跨所交集，不是 Binance 的完整合约列表；
	// - Aster / Hyperliquid 也会有同样问题；
	// - 最终看起来像是“某家交易所没拉全”，实际上是“入库前被交集过滤掉了”。
	allRecords := flattenSymbolInventory(symbolsByExchange)

	// 全市场基础池：只要一个 canonical symbol 在至少两家交易所同时存在，就纳入后续候选范围。
	watchlist, recordsByExchange := buildMultiVenueWatchlist(symbolsByExchange)
	if len(watchlist) == 0 {
		return fmt.Errorf("no common tradable symbols found across at least two enabled exchanges")
	}
	if err := r.symbolRepo.UpsertBatch(ctx, allRecords); err != nil {
		return err
	}
	for _, item := range allRecords {
		r.store.UpsertSymbol(item)
	}
	r.store.SetWatchlist(watchlist)

	// funding 层默认覆盖整个基础池，因为 funding / mark 数据相对便宜。
	r.subscriptionMu.Lock()
	r.fundingSymbolsByExchange = cloneSymbolMap(recordsByExchange)
	r.subscriptionMu.Unlock()

	// 启动初期还没有全市场 funding 粗筛结果，因此先用核心币 + 少量轮转种子预热盘口深扫池。
	r.refreshDeepScanPlan(time.Now().UTC())

	for _, market := range r.markets {
		if market == nil || !market.Enabled() {
			continue
		}
		market.Start(ctx, r, r.store)
	}

	go r.subscriptionPlannerLoop(ctx)
	go r.fundingSnapshotLoop(ctx)
	go r.bookSnapshotLoop(ctx)
	go r.snapshotCleanupLoop(ctx)
	go r.opportunityLoop(ctx)
	return nil
}

// FundingSymbols 返回某个交易所在“粗筛层”应该维护的 symbol 集合。
// 当前策略里它通常覆盖整个全市场基础池。
func (r *StrategyRunner) FundingSymbols(exchangeName string) []entity.Symbol {
	r.subscriptionMu.RLock()
	defer r.subscriptionMu.RUnlock()
	return append([]entity.Symbol(nil), r.fundingSymbolsByExchange[strings.ToLower(exchangeName)]...)
}

// BookSymbols 返回某个交易所在“深扫层”应该维护盘口的 symbol 集合。
// 这批 symbol 会随 funding 粗筛结果和轮转池动态变化。
func (r *StrategyRunner) BookSymbols(exchangeName string) []entity.Symbol {
	r.subscriptionMu.RLock()
	defer r.subscriptionMu.RUnlock()
	return append([]entity.Symbol(nil), r.bookSymbolsByExchange[strings.ToLower(exchangeName)]...)
}

func flattenSymbolInventory(symbolsByExchange map[string][]entity.Symbol) []entity.Symbol {
	seen := make(map[string]struct{})
	out := make([]entity.Symbol, 0)
	for ex, items := range symbolsByExchange {
		for _, item := range items {
			item.Exchange = ex
			item.Symbol = strings.ToUpper(strings.TrimSpace(item.Symbol))
			item.VenueSymbol = strings.ToUpper(strings.TrimSpace(item.VenueSymbol))
			item.Watched = false
			key := strings.ToLower(ex) + "|" + item.Symbol + "|" + item.VenueSymbol
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Exchange == out[j].Exchange {
			if out[i].Symbol == out[j].Symbol {
				return out[i].VenueSymbol < out[j].VenueSymbol
			}
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Exchange < out[j].Exchange
	})
	return out
}

func buildMultiVenueWatchlist(symbolsByExchange map[string][]entity.Symbol) ([]string, map[string][]entity.Symbol) {
	byCanonical := make(map[string]map[string]entity.Symbol)
	for ex, items := range symbolsByExchange {
		for _, item := range items {
			key := strings.ToUpper(item.Symbol)
			if byCanonical[key] == nil {
				byCanonical[key] = make(map[string]entity.Symbol)
			}
			item.Exchange = ex
			if _, ok := byCanonical[key][ex]; ok {
				continue
			}
			byCanonical[key][ex] = item
		}
	}
	watchlist := make([]string, 0)
	recordsByExchange := make(map[string][]entity.Symbol)
	for symbol, m := range byCanonical {
		if len(m) < 2 {
			continue
		}
		watchlist = append(watchlist, symbol)
		for ex, item := range m {
			item.Watched = true
			recordsByExchange[ex] = append(recordsByExchange[ex], item)
		}
	}
	sort.Strings(watchlist)
	for ex := range recordsByExchange {
		sort.Slice(recordsByExchange[ex], func(i, j int) bool { return recordsByExchange[ex][i].Symbol < recordsByExchange[ex][j].Symbol })
	}
	return watchlist, recordsByExchange
}

// subscriptionPlannerLoop 周期性重算深扫池。
// 它会把“全市场 funding 粗筛结果”“核心币”“冷门轮转池”三者合并，
// 决定下一轮真正需要订阅盘口的 symbol 集合。
func (r *StrategyRunner) subscriptionPlannerLoop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.RotationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			r.refreshDeepScanPlan(now.UTC())
		}
	}
}

func (r *StrategyRunner) refreshDeepScanPlan(now time.Time) {
	watchlist := r.store.Watchlist()
	if len(watchlist) == 0 {
		return
	}

	universeSet := make(map[string]struct{}, len(watchlist))
	for _, sym := range watchlist {
		universeSet[strings.ToUpper(sym)] = struct{}{}
	}

	selected := make(map[string]struct{})
	ordered := make([]string, 0)
	add := func(symbol string) {
		symbol = strings.ToUpper(strings.TrimSpace(symbol))
		if symbol == "" {
			return
		}
		if _, ok := universeSet[symbol]; !ok {
			return
		}
		if _, ok := selected[symbol]; ok {
			return
		}
		selected[symbol] = struct{}{}
		ordered = append(ordered, symbol)
	}

	// 1) 核心币：始终常驻深扫池。
	for _, sym := range r.cfg.CoreSymbols {
		add(sym)
	}

	// 2) 保留仍在“最短驻留期”内的旧候选，降低 websocket 抖动。
	r.subscriptionMu.Lock()
	for sym, until := range r.deepScanPinnedUntil {
		if until.After(now) {
			add(sym)
			continue
		}
		delete(r.deepScanPinnedUntil, sym)
	}
	r.subscriptionMu.Unlock()

	// 3) 全市场 funding 粗筛：挑出 TopN 候选，进入盘口深扫。
	//
	// DynamicCandidateLimit 取值约定：
	//   > 0 : 只取前 N 个候选。
	//   = 0 : 不设上限，所有粗筛候选都进入深扫池。
	ranked := r.rankCoarseCandidates(now)
	dynamicAdded := 0
	dynamicUnlimited := r.cfg.DynamicCandidateLimit == 0
	for _, item := range ranked {
		if _, ok := selected[item.Symbol]; ok {
			continue
		}
		if !dynamicUnlimited && dynamicAdded >= r.cfg.DynamicCandidateLimit {
			break
		}
		add(item.Symbol)
		r.subscriptionMu.Lock()
		r.deepScanPinnedUntil[item.Symbol] = now.Add(r.cfg.DeepScanHoldDuration)
		r.subscriptionMu.Unlock()
		dynamicAdded++
	}

	// 4) 冷门轮转池：即便没进 TopN，也定期给一部分 symbol 分配盘口深扫，降低漏检概率。
	remaining := make([]string, 0, len(watchlist))
	for _, sym := range watchlist {
		if _, ok := selected[sym]; ok {
			continue
		}
		remaining = append(remaining, sym)
	}
	if len(remaining) > 0 && r.cfg.RotationBatchSize > 0 {
		rotationCount := minInt(r.cfg.RotationBatchSize, len(remaining))
		start := 0
		r.subscriptionMu.Lock()
		if len(remaining) > 0 {
			start = r.rotationCursor % len(remaining)
			r.rotationCursor = (r.rotationCursor + rotationCount) % len(remaining)
		}
		r.subscriptionMu.Unlock()
		for i := 0; i < rotationCount; i++ {
			idx := (start + i) % len(remaining)
			add(remaining[idx])
		}
	}

	bookMap := make(map[string][]entity.Symbol)
	for _, symbol := range ordered {
		for _, ex := range r.store.ExchangesForSymbol(symbol) {
			item, ok := r.store.Symbol(ex, symbol)
			if !ok {
				continue
			}
			bookMap[strings.ToLower(ex)] = append(bookMap[strings.ToLower(ex)], item)
		}
	}
	for ex := range bookMap {
		sort.Slice(bookMap[ex], func(i, j int) bool {
			return bookMap[ex][i].Symbol < bookMap[ex][j].Symbol
		})
	}

	r.subscriptionMu.Lock()
	r.bookSymbolsByExchange = bookMap
	r.subscriptionMu.Unlock()
	r.store.SetDeepScanWatchlist(ordered)
	r.logger.Info("deep_scan_watchlist_updated",
		slog.Int("deep_scan_count", len(ordered)),
		slog.Int("coarse_candidate_count", len(ranked)),
		slog.Int("rotation_batch_size", r.cfg.RotationBatchSize),
	)
}

// rankCoarseCandidates 对全市场 funding 数据做轻量打分。
// 这里故意不依赖盘口，只看 funding / 时间窗口 / 交易所覆盖情况，
// 目的是让“全市场覆盖”成本尽量低。
func (r *StrategyRunner) rankCoarseCandidates(now time.Time) []coarseCandidate {
	watch := r.store.Watchlist()
	out := make([]coarseCandidate, 0, len(watch))
	for _, symbol := range watch {
		exchanges := r.store.ExchangesForSymbol(symbol)
		if len(exchanges) < 2 {
			continue
		}

		bestScore := 0.0
		for i := 0; i < len(exchanges); i++ {
			for j := i + 1; j < len(exchanges); j++ {
				fA, okA := r.store.LatestFunding(exchanges[i], symbol)
				fB, okB := r.store.LatestFunding(exchanges[j], symbol)
				if !(okA && okB) {
					continue
				}
				if r.isSnapshotStale(now, fA.EventTimeMs) || r.isSnapshotStale(now, fB.EventTimeMs) {
					continue
				}
				forecastA := r.forecastFunding(context.Background(), now, fA)
				forecastB := r.forecastFunding(context.Background(), now, fB)

				_, _, projection, ok := r.bestFundingDirection(now, exchanges[i], fA, forecastA, exchanges[j], fB, forecastB)
				if !ok || projection.CarryRate <= 0 {
					continue
				}

				// 全市场粗筛只用 funding 事件与时间信息打分，不依赖盘口。
				// 这样可以把更多“可能有机会但还没进深扫池”的 symbol 先拉进候选集。
				score := projection.CarryRate * 10000 * r.fundingTimeWeight(now, projection.ProjectedFundingTimeMs)
				score *= 1 + float64(len(exchanges)-2)*0.05
				if projection.CarryRateHourlyEquivalent > 0 {
					score += projection.CarryRateHourlyEquivalent * 1000
				}
				if score > bestScore {
					bestScore = score
				}
			}
		}

		if bestScore > 0 {
			out = append(out, coarseCandidate{Symbol: symbol, Score: bestScore})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Symbol < out[j].Symbol
		}
		return out[i].Score > out[j].Score
	})
	return out
}

// fundingTimeWeight 用来给“更接近结算时间”的机会更高权重。
// 越接近结算，越值得立刻拉进盘口深扫池；太远的机会先观察即可。
func (r *StrategyRunner) fundingTimeWeight(now time.Time, fundingTimeMs int64) float64 {
	if fundingTimeMs <= 0 {
		return 1
	}
	until := time.UnixMilli(fundingTimeMs).Sub(now)
	if until <= 0 {
		return 0.4
	}
	if until <= r.cfg.EntryLeadTime {
		return 1.4
	}
	hours := until.Hours()
	if hours <= 1 {
		return 1.2
	}
	return 1 / (1 + hours/6)
}

func (r *StrategyRunner) opportunityLoop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.OpportunityCalcInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			batchID := fmt.Sprintf("%d", now.UnixMilli())
			items := r.computeCandidates(now)
			if len(items) == 0 {
				continue
			}
			if err := r.oppRepo.SaveBatch(ctx, batchID, items); err != nil {
				r.logger.Error("save_opportunities_failed", slog.Any("err", err))
				continue
			}
			plans := r.buildExecutionPlans(now, batchID, items)
			if len(plans) > 0 {
				if err := r.planRepo.SaveBatch(ctx, batchID, batchID, plans); err != nil {
					r.logger.Error("save_execution_plans_failed", slog.Any("err", err))
				}
			}
		}
	}
}

// computeCandidates 是“最终机会精算层”的核心。
//
// 计算步骤（逐 symbol、逐交易所对）：
// 1) 读取双边 funding + 盘口 + symbol 元数据（任一缺失直接跳过）；
// 2) 用 bestFundingDirection 同时评估两个方向：
//   - Long A / Short B
//   - Long B / Short A
//     并选择 carry 更高的一边；
//     3. 资金收益：
//     grossFundingPNL = notional * projection.CarryRate
//     其中 projection.CarryRate 来自“事件时间轴模型”，不是简单小时化线性外推；
//     4. 成本扣减：entry fee + exit fee + slippage + safety buffer；
//     5. 风控与执行窗校验：数据新鲜度、basis、最小净利润、入场时间窗；
//     6. 产出 Opportunity（包含方向、窗口、事件计数、预估净收益）。
//
// 精算阶段默认遍历整个基础池。
//
// 为什么这里不直接只遍历“当前深扫池”：
// 1) CEX 侧现在可以常驻全市场 bookTicker，盘口数据天然比深扫池更广；
// 2) 真正的缺失会在 ok1~ok6 处被过滤，不会因为全量遍历而误下单；
// 3) 这样可以避免“已经拿到盘口的数据，仅因未被调度进深扫池而被静默漏算”。
func (r *StrategyRunner) computeCandidates(now time.Time) []entity.Opportunity {
	// 精算阶段默认遍历整个基础池。
	//
	// 原因：
	// 1. Binance / Aster 这类 CEX 现在可以直接接全市场 bookTicker，
	//    因此它们的盘口数据并不一定受“深扫池”限制；
	// 2. 如果这里只遍历深扫池，会把已经拿到实时盘口的 CEX-only 机会也一起漏掉；
	// 3. 对于还没有盘口的一侧（例如未进入 Hyperliquid 深扫池的 coin），
	//    下面的 ok1~ok6 校验会自动把该组合跳过。
	watch := r.store.Watchlist()
	items := make([]entity.Opportunity, 0, len(watch)*2)
	for _, symbol := range watch {
		exchanges := r.store.ExchangesForSymbol(symbol)
		if len(exchanges) < 2 {
			continue
		}
		for i := 0; i < len(exchanges); i++ {
			for j := i + 1; j < len(exchanges); j++ {
				exA, exB := exchanges[i], exchanges[j]
				fA, ok1 := r.store.LatestFunding(exA, symbol)
				fB, ok2 := r.store.LatestFunding(exB, symbol)
				bA, ok3 := r.store.LatestBookTop(exA, symbol)
				bB, ok4 := r.store.LatestBookTop(exB, symbol)
				metaA, ok5 := r.store.Symbol(exA, symbol)
				metaB, ok6 := r.store.Symbol(exB, symbol)
				if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6) {
					continue
				}
				forecastA := r.forecastFunding(context.Background(), now, fA)
				forecastB := r.forecastFunding(context.Background(), now, fB)

				longExchange, shortExchange, projection, ok := r.bestFundingDirection(now, exA, fA, forecastA, exB, fB, forecastB)
				if !ok {
					continue
				}

				longFunding, shortFunding := fA, fB
				longBook, shortBook := bA, bB
				longMeta, shortMeta := metaA, metaB
				longForecast, shortForecast := forecastA, forecastB
				if strings.EqualFold(longExchange, exB) {
					longFunding, shortFunding = fB, fA
					longBook, shortBook = bB, bA
					longMeta, shortMeta = metaB, metaA
					longForecast, shortForecast = forecastB, forecastA
				}
				estimateMode, estimateConfidence := fundingEstimateProfile(projection, longForecast, shortForecast)

				// 保留“小时化等价值”只用于展示和打分，不再作为 funding 收益的核心计算公式。
				longHourly := longFunding.FundingRate / float64(maxInt(longFunding.FundingIntervalHours, 1))
				shortHourly := shortFunding.FundingRate / float64(maxInt(shortFunding.FundingIntervalHours, 1))
				grossEdgeHourly := projection.CarryRateHourlyEquivalent
				notional := r.cfg.EffectiveNotional()

				grossFundingPNL := notional * projection.CarryRate
				entryFeePNL := r.calcLegFee(notional, longExchange, r.cfg.EntryMode) + r.calcLegFee(notional, shortExchange, r.cfg.EntryMode)
				exitFeePNL := r.calcLegFee(notional, longExchange, r.cfg.ExitMode) + r.calcLegFee(notional, shortExchange, r.cfg.ExitMode)
				slippagePNL := notional * r.cfg.SlippageBps / 10000
				safetyBufferPNL := r.cfg.SafetyBufferUSDT
				netExpectedPNL := grossFundingPNL - entryFeePNL - exitFeePNL - slippagePNL - safetyBufferPNL
				netExpectedBps := 0.0
				if notional > 0 {
					netExpectedBps = netExpectedPNL / notional * 10000
				}
				basisBps := 0.0
				if longBook.AskPrice > 0 {
					basisBps = absFloat(shortBook.BidPrice-longBook.AskPrice) / longBook.AskPrice * 10000
				}
				// estimateExecutionPenalty 将执行惩罚拆成三段：
				// 1. entry: 开仓吃到的基础滑点/盘口冲击；
				// 2. exit: 平仓时残留 basis 与退出执行摩擦；
				// 3. hedge rollback: 多事件路径更长、单腿异常时需要预留的回滚冗余。
				//
				// 同时，这个惩罚还会乘上 exchange / symbol / time bucket 的经验乘子，
				// 让不同 venue、不同币种、不同时间段的执行质量差异开始显式进入模型。
				execPenalty := r.estimateExecutionPenalty(now, symbol, longExchange, shortExchange, projection, basisBps)
				maxAllowedBasisBps := r.allowedBasisThresholdBps(projection)
				slippagePNL = notional * (execPenalty.EntryPenaltyBps + execPenalty.ExitPenaltyBps) / 10000
				safetyBufferPNL = r.cfg.SafetyBufferUSDT + notional*execPenalty.HedgeRollbackBps/10000
				netExpectedPNL = grossFundingPNL - entryFeePNL - exitFeePNL - slippagePNL - safetyBufferPNL
				if notional > 0 {
					netExpectedBps = netExpectedPNL / notional * 10000
				}
				status, reason, eligible := r.evaluateOpportunity(now, projection, netExpectedPNL, basisBps, maxAllowedBasisBps, longFunding, shortFunding, longBook, shortBook)
				items = append(items, entity.Opportunity{
					BatchID:                      "",
					AsOfTimeMs:                   now.UnixMilli(),
					Symbol:                       symbol,
					LongExchange:                 longExchange,
					ShortExchange:                shortExchange,
					LongVenueSymbol:              longMeta.VenueSymbol,
					ShortVenueSymbol:             shortMeta.VenueSymbol,
					LongFundingRate:              longFunding.FundingRate,
					ShortFundingRate:             shortFunding.FundingRate,
					LongFundingTimeMs:            longFunding.FundingTimeMs,
					ShortFundingTimeMs:           shortFunding.FundingTimeMs,
					LongFundingIntervalHours:     longFunding.FundingIntervalHours,
					ShortFundingIntervalHours:    shortFunding.FundingIntervalHours,
					LongFundingHourly:            longHourly,
					ShortFundingHourly:           shortHourly,
					GrossEdgeHourly:              grossEdgeHourly,
					LongFutureFundingRate:        longForecast.PredictedRateForEvent(2),
					ShortFutureFundingRate:       shortForecast.PredictedRateForEvent(2),
					FundingEstimateMode:          estimateMode,
					FundingEstimateConfidence:    estimateConfidence,
					LongBidPrice:                 longBook.BidPrice,
					LongAskPrice:                 longBook.AskPrice,
					ShortBidPrice:                shortBook.BidPrice,
					ShortAskPrice:                shortBook.AskPrice,
					LongMarkPrice:                longFunding.MarkPrice,
					ShortMarkPrice:               shortFunding.MarkPrice,
					GrossFundingPNL:              grossFundingPNL,
					EntryFeePNL:                  entryFeePNL,
					ExitFeePNL:                   exitFeePNL,
					SlippagePNL:                  slippagePNL,
					SafetyBufferPNL:              safetyBufferPNL,
					EntryPenaltyBps:              execPenalty.EntryPenaltyBps,
					ExitPenaltyBps:               execPenalty.ExitPenaltyBps,
					HedgePenaltyBps:              execPenalty.HedgeRollbackBps,
					ExecutionPenaltyBps:          execPenalty.TotalPenaltyBps,
					ExecutionPenaltyModel:        execPenalty.ExperienceModel,
					ExecutionPenaltyBucket:       execPenalty.ExperienceBucket,
					NetExpectedPNL:               netExpectedPNL,
					NetExpectedBps:               netExpectedBps,
					BasisBps:                     basisBps,
					MaxAllowedBasisBps:           maxAllowedBasisBps,
					Score:                        r.scoreOpportunity(netExpectedPNL, grossEdgeHourly, basisBps),
					EarliestFundingTimeMs:        minInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs),
					LatestFundingTimeMs:          maxInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs),
					ProjectedFundingTimeMs:       projection.ProjectedFundingTimeMs,
					RequiredEntryByFundingTimeMs: projection.RequiredEntryByFundingTimeMs,
					LongFundingEventCount:        projection.LongFundingEventCount,
					ShortFundingEventCount:       projection.ShortFundingEventCount,
					FundingWindowHours:           projection.FundingWindowHours,
					FundingComputationMode:       projection.ComputationMode,
					Status:                       status,
					RejectReason:                 reason,
					EligibleForExecution:         eligible,
				})
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].NetExpectedPNL > items[j].NetExpectedPNL })
	// 注意：这里不再按 MaxDisplayedOpportunities 做截断。
	// 原因：机会列表的“搜索/筛选”需要基于完整 batch 数据；
	// 若在计算层提前截断，后端与前端都无法再检索被截掉的机会。
	// 展示数量控制应由查询参数(limit)与前端分页/筛选承担。
	return items
}

func (r *StrategyRunner) allowedBasisThresholdBps(projection fundingProjection) float64 {
	base := r.cfg.MaxSpreadBps
	if base <= 0 {
		base = 12
	}
	multiplier := r.cfg.DynamicMaxSpreadMultiplier
	if multiplier < 1 {
		multiplier = 1
	}
	refHours := r.cfg.DynamicMaxSpreadReferenceHours
	if refHours <= 0 {
		refHours = r.cfg.HoldHours
	}
	if refHours <= 0 {
		return base
	}
	windowHours := projection.FundingWindowHours
	if windowHours < 0 {
		windowHours = 0
	}
	ratio := windowHours / refHours
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	return base * (1 + (multiplier-1)*ratio)
}

// evaluateOpportunity 将“收益估算”转成“可执行状态”。
//
// 判定顺序是刻意设计的：
// 1) stale data：防止基于过期市场数据下单；
// 2) carryRate<=0：funding 本身无正向优势，直接淘汰；
// 3) basis 限制：避免靠 funding 赚的钱被入场基差吞掉；
// 4) min net pnl：统一门槛；
// 5) entry window：确保在计划结算前仍有可执行性。
func (r *StrategyRunner) evaluateOpportunity(now time.Time, projection fundingProjection, netExpectedPNL float64, basisBps float64, maxAllowedBasisBps float64, longFunding, shortFunding entity.FundingSnapshot, longBook, shortBook entity.BookTopSnapshot) (string, string, bool) {
	if r.isSnapshotStale(now, longFunding.EventTimeMs) || r.isSnapshotStale(now, shortFunding.EventTimeMs) || r.isSnapshotStale(now, longBook.EventTimeMs) || r.isSnapshotStale(now, shortBook.EventTimeMs) {
		return OpportunityStatusStaleData, "market data is stale", false
	}
	if projection.CarryRate <= 0 {
		return OpportunityStatusSpreadTooSmall, "event-based funding carry is not positive", false
	}
	if basisBps > maxAllowedBasisBps {
		return OpportunityStatusBasisTooWide, fmt.Sprintf("basis %.4f bps > dynamic max %.4f bps", basisBps, maxAllowedBasisBps), false
	}
	if netExpectedPNL < r.cfg.MinNetPNL {
		return OpportunityStatusNotProfitable, fmt.Sprintf("net pnl %.4f < min %.4f", netExpectedPNL, r.cfg.MinNetPNL), false
	}
	entryAnchor := projection.RequiredEntryByFundingTimeMs
	if entryAnchor <= 0 {
		entryAnchor = minInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs)
	}
	if !r.isWithinEntryWindow(now, entryAnchor) {
		return OpportunityStatusOutsideEntryWind, "not in entry window", false
	}
	return OpportunityStatusEligible, "", true
}

// bestFundingDirection 会把同一个交易所对的两个方向都算一遍：
// 1. Long A / Short B
// 2. Long B / Short A
//
// 然后按“真实 funding 事件下的单位收益率”挑出更优方向。
// 这就是“固定一个方向再计算”的真正含义：
// - 不是永远只做 Long Binance / Short Aster；
// - 而是每次评估时，都先假设一个明确方向，再按该方向计算真实 funding 收益；
// - 反方向也会完整计算一遍；
// - 当前时刻哪个方向更优，就返回哪个方向。
//
// 因此，如果后续 funding 快照变化，反方向变得更优，下一轮计算自然会切换方向。
func (r *StrategyRunner) bestFundingDirection(now time.Time, exA string, fA entity.FundingSnapshot, forecastA fundingForecast, exB string, fB entity.FundingSnapshot, forecastB fundingForecast) (string, string, fundingProjection, bool) {
	projAB, okAB := r.projectFundingCarry(now, fA, forecastA, fB, forecastB)
	projBA, okBA := r.projectFundingCarry(now, fB, forecastB, fA, forecastA)
	if !okAB && !okBA {
		return "", "", fundingProjection{}, false
	}
	if !okBA {
		return exA, exB, projAB, true
	}
	if !okAB {
		return exB, exA, projBA, true
	}
	if projAB.CarryRate > projBA.CarryRate {
		return exA, exB, projAB, true
	}
	if projBA.CarryRate > projAB.CarryRate {
		return exB, exA, projBA, true
	}
	if projAB.CarryRateHourlyEquivalent > projBA.CarryRateHourlyEquivalent {
		return exA, exB, projAB, true
	}
	if projBA.CarryRateHourlyEquivalent > projAB.CarryRateHourlyEquivalent {
		return exB, exA, projBA, true
	}
	if projAB.ProjectedFundingTimeMs > 0 && projBA.ProjectedFundingTimeMs > 0 {
		if projAB.ProjectedFundingTimeMs <= projBA.ProjectedFundingTimeMs {
			return exA, exB, projAB, true
		}
		return exB, exA, projBA, true
	}
	return exA, exB, projAB, true
}

// projectFundingCarry 按“当前已知 funding 节奏”估算方向收益。
//
// longFunding / shortFunding 的含义是：
// - longFunding: 假设做多腿所在交易所的 funding 快照
// - shortFunding: 假设做空腿所在交易所的 funding 快照
//
// 这里不把 funding 先统一小时化后线性外推，而是优先按事件时间轴逐点估算：
// - 候选结算点 = 两侧 funding 事件时间轴上、直到 max(nextLong, nextShort) 之前的所有结算点
// - 对每个候选结算点，计算到该时点时双腿各自会发生几次 funding（会考虑多次结算）
// - 选择 CarryRate 最大的那个时点
//
// 这样可以正确覆盖：
// - 一边 1h 结算，一边 4h / 8h 结算；
// - 两边 funding 都为负，但负得不一样；
// - 最优方向和最优退出点不一定是“最早结算点”。
func (r *StrategyRunner) projectFundingCarry(now time.Time, longFunding entity.FundingSnapshot, longForecast fundingForecast, shortFunding entity.FundingSnapshot, shortForecast fundingForecast) (fundingProjection, bool) {
	nowMs := now.UnixMilli()
	candidateTimes := buildFundingCandidateTimes(nowMs, longFunding, shortFunding, r.cfg.HoldHours)
	if len(candidateTimes) == 0 {
		return fundingProjection{}, false
	}

	best := fundingProjection{}
	bestOK := false
	for _, projectedTime := range candidateTimes {
		longCount := fundingEventCountUntil(nowMs, projectedTime, longFunding.FundingTimeMs, longFunding.FundingIntervalHours)
		shortCount := fundingEventCountUntil(nowMs, projectedTime, shortFunding.FundingTimeMs, shortFunding.FundingIntervalHours)
		if longCount == 0 && shortCount == 0 {
			continue
		}

		// 第一笔已知 funding 事件仍使用当前快照；
		// 只有当持仓窗口跨到第 2 次及以后事件时，才使用预测器给出的“分 event 未来费率路径”参与估算。
		shortCarry := projectedLegFundingCarry(shortForecast, shortCount)
		longCarry := projectedLegFundingCarry(longForecast, longCount)
		carryRate := shortCarry - longCarry
		windowHours := float64(projectedTime-nowMs) / float64(time.Hour/time.Millisecond)
		if windowHours <= 0 {
			windowHours = 1.0 / 60.0
		}
		hourlyEq := carryRate / windowHours

		requiredEntryBy := int64(0)
		if longCount > 0 {
			requiredEntryBy = longFunding.FundingTimeMs
		}
		if shortCount > 0 {
			if requiredEntryBy == 0 || shortFunding.FundingTimeMs < requiredEntryBy {
				requiredEntryBy = shortFunding.FundingTimeMs
			}
		}

		projection := fundingProjection{
			ProjectedFundingTimeMs:       projectedTime,
			RequiredEntryByFundingTimeMs: requiredEntryBy,
			LongFundingEventCount:        longCount,
			ShortFundingEventCount:       shortCount,
			FundingWindowHours:           windowHours,
			CarryRate:                    carryRate,
			CarryRateHourlyEquivalent:    hourlyEq,
			ComputationMode:              "event_based_known_next_funding",
		}
		if longForecast.Regime != "" || shortForecast.Regime != "" {
			projection.ComputationMode = "event_based_regime_aware_forecast"
		}

		if !bestOK || projection.CarryRate > best.CarryRate ||
			(projection.CarryRate == best.CarryRate && projection.CarryRateHourlyEquivalent > best.CarryRateHourlyEquivalent) ||
			(projection.CarryRate == best.CarryRate && projection.CarryRateHourlyEquivalent == best.CarryRateHourlyEquivalent && projection.ProjectedFundingTimeMs < best.ProjectedFundingTimeMs) {
			best = projection
			bestOK = true
		}
	}
	return best, bestOK
}

// buildFundingCandidateTimes 构建候选退出时点。
//
// 思路：
// - 各腿从 nextFundingTime 开始，按 interval 生成事件时间轴；
// - 合并去重；
// - 在 horizon 内保留候选点，其中 horizon=max(latestNextFunding, now+holdHours)。
//
// 这意味着：
//   - 至少会覆盖两腿“已知下一次结算”之前的所有相关事件；
//   - 当 holdHours 更长（例如 24h）时，会继续评估更远的退出点，
//     支持“多轮 funding 覆盖建仓成本”的策略。
func buildFundingCandidateTimes(nowMs int64, longFunding, shortFunding entity.FundingSnapshot, holdHours float64) []int64 {
	latestNextFunding := maxInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs)
	horizon := latestNextFunding
	if holdHours > 0 {
		holdMs := int64(holdHours * float64(time.Hour/time.Millisecond))
		if holdMs > 0 {
			horizon = maxInt64(horizon, nowMs+holdMs)
		}
	}
	if horizon <= nowMs {
		return nil
	}
	uniq := make(map[int64]struct{})
	out := make([]int64, 0, 16)
	appendTimeline := func(nextTimeMs int64, intervalHours int) {
		if nextTimeMs <= nowMs {
			return
		}
		intervalMs := int64(maxInt(intervalHours, 0)) * int64(time.Hour/time.Millisecond)
		for ts := nextTimeMs; ts <= horizon; {
			if _, exists := uniq[ts]; !exists {
				uniq[ts] = struct{}{}
				out = append(out, ts)
			}
			if intervalMs <= 0 {
				break
			}
			ts += intervalMs
		}
	}

	appendTimeline(longFunding.FundingTimeMs, longFunding.FundingIntervalHours)
	appendTimeline(shortFunding.FundingTimeMs, shortFunding.FundingIntervalHours)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (r *StrategyRunner) forecastFunding(ctx context.Context, now time.Time, item entity.FundingSnapshot) fundingForecast {
	if r.forecaster == nil {
		return fundingForecast{
			CurrentRate:        item.FundingRate,
			BaselineRate:       item.FundingRate,
			HistoryMean:        item.FundingRate,
			Regime:             "spot_only",
			Confidence:         "low",
			MeanReversion:      0.2,
			ContinuationDecay:  normalizedContinuationDecay(r.cfg.FundingRateContinuationDecay),
			EffectiveFloorRate: item.FundingRate,
			EffectiveCapRate:   item.FundingRate,
		}
	}
	return r.forecaster.Forecast(ctx, now, item)
}

// fundingEventCountUntil 计算 [now, projectedTime] 窗口内某一腿会发生几次 funding。
//
// 规则：
// - nextFundingTime 不在窗口内 => 0 次；
// - 在窗口内先计 1 次；
// - 若 interval>0，再按等间隔累加后续次数。
//
// 注意：这里默认“当前已知 fundingRate 在该时间轴上延续”，
// 属于实盘中常见的近端近似；后续若接入更长历史/预测模型，可替换此处。
func fundingRateEventMultiplier(eventCount int, continuationDecay float64) float64 {
	if eventCount <= 0 {
		return 0
	}
	if continuationDecay <= 0 || continuationDecay > 1 {
		continuationDecay = 0.6
	}
	if continuationDecay == 1 {
		return float64(eventCount)
	}
	// 首次事件使用 1.0，后续事件按 decay^(k-1) 衰减。
	// sum_{k=0}^{n-1} decay^k = (1-decay^n)/(1-decay)
	pow := math.Pow(continuationDecay, float64(eventCount))
	return (1 - pow) / (1 - continuationDecay)
}

func blendedFundingRate(currentRate, historyAvg, currentWeight float64) float64 {
	return weightedBlend(currentRate, historyAvg, currentWeight)
}

func projectedLegFundingCarry(forecast fundingForecast, eventCount int) float64 {
	if eventCount <= 0 {
		return 0
	}
	carry := 0.0
	for eventIndex := 1; eventIndex <= eventCount; eventIndex++ {
		rate := forecast.PredictedRateForEvent(eventIndex)
		if eventIndex == 1 {
			carry += rate
			continue
		}
		carry += rate * math.Pow(forecast.ContinuationDecay, float64(eventIndex-2))
	}
	return carry
}

func fundingEstimateProfile(projection fundingProjection, longForecast, shortForecast fundingForecast) (string, string) {
	maxEvents := maxInt(projection.LongFundingEventCount, projection.ShortFundingEventCount)
	confidence := "high"
	if longForecast.Confidence == "guarded" || shortForecast.Confidence == "guarded" {
		confidence = "guarded"
	} else if longForecast.Confidence == "medium" || shortForecast.Confidence == "medium" || longForecast.Confidence == "low" || shortForecast.Confidence == "low" {
		confidence = "medium"
	}
	switch {
	case maxEvents <= 1:
		return "single_cycle_spot", confidence
	case maxEvents == 2:
		return "multi_cycle_regime_aware", confidence
	default:
		if confidence == "high" {
			confidence = "medium"
		}
		return "multi_cycle_regime_aware", confidence
	}
}

func fundingEventCountUntil(nowMs, projectedTimeMs, nextFundingTimeMs int64, intervalHours int) int {
	if projectedTimeMs <= nowMs || nextFundingTimeMs <= nowMs || nextFundingTimeMs > projectedTimeMs {
		return 0
	}
	count := 1
	intervalMs := int64(maxInt(intervalHours, 0)) * int64(time.Hour/time.Millisecond)
	if intervalMs <= 0 {
		return count
	}
	extra := (projectedTimeMs - nextFundingTimeMs) / intervalMs
	if extra > 0 {
		count += int(extra)
	}
	return count
}

func (r *StrategyRunner) ConfigSnapshot() string {
	b, _ := json.Marshal(r.cfg)
	return string(b)
}

func (r *StrategyRunner) fundingSnapshotLoop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.FundingSnapshotPersistInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			items := r.collectChangedFundingSnapshots()
			if len(items) == 0 {
				continue
			}
			if err := r.marketRepo.SaveFundingSnapshots(ctx, items); err != nil {
				r.logger.Error("save_funding_snapshots_failed", slog.Any("err", err))
			}
		}
	}
}

func (r *StrategyRunner) bookSnapshotLoop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.BookSnapshotPersistInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			items := r.collectChangedBookTopSnapshots()
			if len(items) == 0 {
				continue
			}
			if err := r.marketRepo.SaveBookTopSnapshots(ctx, items); err != nil {
				r.logger.Error("save_book_snapshots_failed", slog.Any("err", err))
			}
		}
	}
}

func (r *StrategyRunner) snapshotCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-r.cfg.SnapshotRetention)
			_ = r.marketRepo.DeleteOldFundingSnapshots(ctx, cutoff)
			_ = r.marketRepo.DeleteOldBookTopSnapshots(ctx, cutoff)
		}
	}
}

func (r *StrategyRunner) collectChangedFundingSnapshots() []entity.FundingSnapshot {
	// funding 快照保留整个全市场基础池，因为它本来就是粗筛输入。
	current := r.store.FundingSnapshots(r.store.Watchlist())
	out := make([]entity.FundingSnapshot, 0, len(current))
	for _, item := range current {
		key := item.Exchange + ":" + item.Symbol
		prev, ok := r.lastFundingPersisted[key]
		if ok && !r.shouldPersistFundingSnapshot(prev, item) {
			continue
		}
		out = append(out, item)
		r.lastFundingPersisted[key] = item
	}
	return out
}

func (r *StrategyRunner) collectChangedBookTopSnapshots() []entity.BookTopSnapshot {
	// 盘口快照只保留深扫池，避免把全市场高频盘口都刷进数据库。
	current := r.store.BookTopSnapshots(r.store.DeepScanWatchlist())
	out := make([]entity.BookTopSnapshot, 0, len(current))
	for _, item := range current {
		key := item.Exchange + ":" + item.Symbol
		prev, ok := r.lastBookPersisted[key]
		if ok && !r.shouldPersistBookTopSnapshot(prev, item) {
			continue
		}
		out = append(out, item)
		r.lastBookPersisted[key] = item
	}
	return out
}

// shouldPersistFundingSnapshot 控制 funding 快照的持久化策略。
// 资金费率、结算时间变化一定落库；价格只有在变化超过阈值时才落库，
// 这样能兼顾“价格别长时间不更新”和“数据库别被轻微抖动打爆”。
func (r *StrategyRunner) shouldPersistFundingSnapshot(prev, curr entity.FundingSnapshot) bool {
	if curr.EventTimeMs <= prev.EventTimeMs {
		return false
	}
	if prev.FundingTimeMs != curr.FundingTimeMs || prev.FundingIntervalHours != curr.FundingIntervalHours || prev.FundingRate != curr.FundingRate {
		return true
	}
	markChanged := calcBpsChange(prev.MarkPrice, curr.MarkPrice) >= fundingSnapshotMinPriceChangeBps
	indexChanged := calcBpsChange(prev.IndexPrice, curr.IndexPrice) >= fundingSnapshotMinPriceChangeBps
	settleChanged := calcBpsChange(prev.EstimatedSettlePrice, curr.EstimatedSettlePrice) >= fundingSnapshotMinPriceChangeBps
	return markChanged || indexChanged || settleChanged
}

func (r *StrategyRunner) shouldPersistBookTopSnapshot(prev, curr entity.BookTopSnapshot) bool {
	if prev.BidPrice <= 0 || prev.AskPrice <= 0 {
		return true
	}
	bidPriceChangeBps := calcBpsChange(prev.BidPrice, curr.BidPrice)
	askPriceChangeBps := calcBpsChange(prev.AskPrice, curr.AskPrice)
	bidQtyChangeRatio := calcRatioChange(prev.BidQty, curr.BidQty)
	askQtyChangeRatio := calcRatioChange(prev.AskQty, curr.AskQty)
	priceChangedEnough := bidPriceChangeBps >= r.cfg.BookSnapshotMinPriceChangeBps || askPriceChangeBps >= r.cfg.BookSnapshotMinPriceChangeBps
	qtyChangedEnough := bidQtyChangeRatio >= r.cfg.BookSnapshotMinQtyChangeRatio || askQtyChangeRatio >= r.cfg.BookSnapshotMinQtyChangeRatio
	return priceChangedEnough || qtyChangedEnough
}

func (r *StrategyRunner) isSnapshotStale(now time.Time, eventTimeMs int64) bool {
	if eventTimeMs <= 0 {
		return true
	}
	maxAge := r.cfg.MaxDataAge
	if maxAge <= 0 {
		maxAge = 30 * time.Second
	}
	return now.Sub(time.UnixMilli(eventTimeMs)) > maxAge
}

func (r *StrategyRunner) isWithinEntryWindow(now time.Time, fundingTimeMs int64) bool {
	if fundingTimeMs <= 0 {
		return true
	}
	openBefore := r.cfg.EntryLeadTime
	closeBefore := r.cfg.EntryCutoffTime
	if openBefore <= 0 || closeBefore <= 0 {
		return true
	}
	untilFunding := time.UnixMilli(fundingTimeMs).Sub(now)
	return untilFunding <= openBefore && untilFunding >= closeBefore
}

func (r *StrategyRunner) calcLegFee(notional float64, exchangeName, mode string) float64 {
	market := r.markets[strings.ToLower(exchangeName)]
	if market == nil {
		return 0
	}
	fees := market.Fees()
	switch strings.ToLower(mode) {
	case "maker":
		return notional * fees.MakerBps / 10000
	case "taker":
		return notional * fees.TakerBps / 10000
	case "mixed":
		return notional * (fees.MakerBps + fees.TakerBps) / 20000
	default:
		return notional * fees.MakerBps / 10000
	}
}

func (r *StrategyRunner) scoreOpportunity(netExpectedPNL, grossEdgeHourly, basisBps float64) float64 {
	return netExpectedPNL*100 + grossEdgeHourly*1000000 - basisBps*2
}

func cloneSymbolMap(src map[string][]entity.Symbol) map[string][]entity.Symbol {
	out := make(map[string][]entity.Symbol, len(src))
	for ex, items := range src {
		out[strings.ToLower(ex)] = append([]entity.Symbol(nil), items...)
	}
	return out
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func minInt64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
func absInt64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
func calcBpsChange(prev, curr float64) float64 {
	if prev == 0 {
		return 0
	}
	diff := math.Abs(curr - prev)
	return diff / prev * 10000
}
func calcRatioChange(prev, curr float64) float64 {
	if prev == 0 {
		if curr == 0 {
			return 0
		}
		return 1
	}
	return math.Abs(curr-prev) / prev
}

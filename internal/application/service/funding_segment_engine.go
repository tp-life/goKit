package service

import (
	"math"
	"sort"
	"time"

	"goKit/internal/domain/entity"
)

const (
	// fundingSegmentTypeSingleReal 表示“只有一腿真实结算”的当前可见结算点。
	// 例如 Aster 14:00 / Binance 16:00 时，14:00 就属于这一类。
	fundingSegmentTypeSingleReal = "single_real"
	// fundingSegmentTypeSingleForecast 是早期设计里保留下来的“中间预测段”类型。
	//
	// 当前 rolling 机会识别已经切成 real-only，不再主动生成这类 segment；
	// 这里保留常量，主要是为了兼容历史数据和旧字段含义，避免序列化值突变。
	fundingSegmentTypeSingleForecast = "single_forecast"
	// fundingSegmentTypeSharedReal 表示双方当前 next funding time 已经对齐，因此该结算点可以直接比较真实 funding 差异。
	fundingSegmentTypeSharedReal = "shared_real"

	fundingProjectionPathEndBoundary      = "sync_boundary"
	fundingProjectionPathEndDirectionFlip = "direction_flip"
	fundingProjectionPathEndHoldHorizon   = "hold_horizon"
)

// fundingSegment 是 rolling_cycle_aligned 模式下的最小收益单元。
//
// 它刻意不直接表达“最终要不要开仓”，而是只描述：
// 1. 这一个结算点上哪一腿会结算；
// 2. 对于当前假设的持仓方向（held long / held short），这一段 funding 的符号贡献是多少；
// 3. 这一段单独看时，更优的方向是什么。
//
// 这样上层既可以：
// - 用它构建“当前开仓应选择的 entry path”；
// - 也可以在后续 rolling monitor 里，逐 review 点复用同一套 segment 解释。
type fundingSegment struct {
	SegmentRank int

	SettlementTimeMs int64
	SegmentType      string
	UsesForecast     bool
	SharedSettlement bool

	// HeldLongExchange / HeldShortExchange 表示“当前正在评估的固定持仓方向”。
	// 对同一对交易所，我们会分别评估：
	// - Long A / Short B
	// - Long B / Short A
	// 哪一边能形成正向 entry path，就由上层再选。
	HeldLongExchange  string
	HeldShortExchange string

	// OptimalLongExchange / OptimalShortExchange 表示“只看当前这一段”时更优的方向。
	// 它主要用于 review 和调试：
	// - 如果它与 Held* 一致，说明当前方向在这一段继续成立；
	// - 如果不一致，说明这里已经出现方向反转。
	OptimalLongExchange  string
	OptimalShortExchange string
	DirectionMatchesHeld bool

	LongLegSettles   bool
	ShortLegSettles  bool
	LongLegRate      float64
	ShortLegRate     float64
	LongLegEventIdx  int
	ShortLegEventIdx int

	// CarryRate 是“这一段单独看时”的最优 funding 收益率，始终为绝对值口径。
	CarryRate float64
	// HeldDirectionCarryRate 是“如果当前真的持有 HeldLong/HeldShort”时，这一段对持仓的有符号贡献。
	// 规则与现有模型保持一致：
	// - long leg 贡献 = -fundingRate
	// - short leg 贡献 = +fundingRate
	// - shared settlement 时，两者相加。
	HeldDirectionCarryRate float64

	ComputationMode string
}

// directionalFundingPlan 把“某个固定方向的所有 segment”与“该方向可接受的 projection 前缀”放在一起。
//
// 这里同时保留 Segments 与 Projections，是为了服务两个阅读层次：
// 1. projection：给机会主列表、plan、自动平仓等旧链路复用；
// 2. segment：给你 review 新策略时，直接看每一段为什么能算、为什么不能算。
type directionalFundingPlan struct {
	Segments           []fundingSegment
	Projections        []fundingProjection
	NextReviewTimeMs   int64
	SyncBoundaryTimeMs int64
}

// buildDirectionalFundingPlanForConfig 会根据 strategy.mode 选择收益路径构建器。
//
// legacy_projection 继续沿用原来的“枚举 hold_hours 内所有候选时点”逻辑；
// rolling_cycle_aligned 则切到“只看到当前 sync boundary 为止”的分段模型。
func buildDirectionalFundingPlanForConfig(
	cfg Config,
	now time.Time,
	longExchange string,
	longFunding entity.FundingSnapshot,
	longForecast fundingForecast,
	shortExchange string,
	shortFunding entity.FundingSnapshot,
	shortForecast fundingForecast,
) directionalFundingPlan {
	normalized := cfg.normalize()
	if normalized.StrategyMode != StrategyModeRollingCycleAligned {
		return directionalFundingPlan{
			Projections: buildLegacyFundingProjectionsForConfig(normalized, now, longFunding, longForecast, shortFunding, shortForecast),
		}
	}
	return buildRollingDirectionalFundingPlan(
		normalized,
		now,
		longExchange,
		longFunding,
		longForecast,
		shortExchange,
		shortFunding,
		shortForecast,
	)
}

// buildRollingDirectionalFundingPlan 构造固定方向下的 rolling entry path。
//
// 这里最关键的约束有两个：
// 1. 只看到“当前双边 next funding time 的较晚者”为止，不跨 sync boundary；
// 2. 当前方向的首段必须立即为正，否则这不是一个应该从现在就入场的方向。
//
// 这样可以直接落地你确认过的业务语义：
// - 13:48 时 Aster 14:00 / 15:00 可以算；
// - 16:00 这一档不能靠 Aster 预测硬算出来；
// - 如果 14:00 对某个方向就是负收益，就不应该因为“后面也许会翻正”而提前开仓。
func buildRollingDirectionalFundingPlan(
	cfg Config,
	now time.Time,
	longExchange string,
	longFunding entity.FundingSnapshot,
	longForecast fundingForecast,
	shortExchange string,
	shortFunding entity.FundingSnapshot,
	shortForecast fundingForecast,
) directionalFundingPlan {
	segments, nextReview, syncBoundary := buildRollingFundingSegments(
		cfg,
		now,
		longExchange,
		longFunding,
		longForecast,
		shortExchange,
		shortFunding,
		shortForecast,
	)
	if len(segments) == 0 {
		return directionalFundingPlan{
			NextReviewTimeMs:   nextReview,
			SyncBoundaryTimeMs: syncBoundary,
		}
	}

	// 首段必须马上对当前持仓方向有正贡献，否则这条固定方向不应成为“现在开仓”的候选。
	// 反方向会在外层被完整再算一遍，因此这里直接过滤掉是安全的。
	if segments[0].HeldDirectionCarryRate <= 0 {
		return directionalFundingPlan{
			Segments:           segments,
			NextReviewTimeMs:   nextReview,
			SyncBoundaryTimeMs: syncBoundary,
		}
	}

	projections := make([]fundingProjection, 0, len(segments))
	includedSegments := len(segments)
	stopReason := fundingProjectionPathEndBoundary
	if syncBoundary > 0 && cfg.HoldHours > 0 {
		horizonMs := now.UnixMilli() + int64(cfg.HoldHours*float64(time.Hour/time.Millisecond))
		if horizonMs > 0 && horizonMs < syncBoundary {
			stopReason = fundingProjectionPathEndHoldHorizon
		}
	}
	if cfg.RollingEntryPathRequireConsistentDirection {
		for idx := 1; idx < len(segments); idx++ {
			if segments[idx].HeldDirectionCarryRate <= 0 {
				includedSegments = idx
				stopReason = fundingProjectionPathEndDirectionFlip
				break
			}
		}
	}

	totalCarry := 0.0
	longCount := 0
	shortCount := 0
	usedForecast := false
	for idx, segment := range segments[:includedSegments] {
		totalCarry += segment.HeldDirectionCarryRate
		if segment.LongLegSettles {
			longCount++
		}
		if segment.ShortLegSettles {
			shortCount++
		}
		if segment.UsesForecast {
			usedForecast = true
		}

		windowHours := float64(segment.SettlementTimeMs-now.UnixMilli()) / float64(time.Hour/time.Millisecond)
		if windowHours <= 0 {
			windowHours = 1.0 / 60.0
		}
		mode := "rolling_cycle_aligned_entry_path"
		if usedForecast {
			mode = "rolling_cycle_aligned_entry_path_forecast"
		}
		projections = append(projections, fundingProjection{
			ProjectedFundingTimeMs:       segment.SettlementTimeMs,
			RequiredEntryByFundingTimeMs: segments[0].SettlementTimeMs,
			LongFundingEventCount:        longCount,
			ShortFundingEventCount:       shortCount,
			FundingWindowHours:           windowHours,
			CarryRate:                    totalCarry,
			CarryRateHourlyEquivalent:    totalCarry / windowHours,
			ComputationMode:              mode,
			StrategyMode:                 StrategyModeRollingCycleAligned,
			NextReviewTimeMs:             nextReview,
			SyncBoundaryTimeMs:           syncBoundary,
			IncludedSegmentCount:         idx + 1,
			PathEndReason:                stopReason,
			Segments:                     cloneFundingSegments(segments[:idx+1]),
		})
	}

	return directionalFundingPlan{
		Segments:           segments,
		Projections:        projections,
		NextReviewTimeMs:   nextReview,
		SyncBoundaryTimeMs: syncBoundary,
	}
}

// buildRollingFundingSegments 把“当前时刻直到 sync boundary”拆成离散 settlement 段。
//
// 规则严格对应之前确认的设计：
// 1. 当前双方 next funding time 一致 => 只生成 shared_real；
// 2. 当前不一致 => 只生成早结算腿的第一段真实 settlement；
// 3. 不再把 boundary 之前的中间预测段直接算进机会收益。
//
// 这里的 real-only 约束是刻意收紧后的策略语义：
// - rolling 结构仍然保留：先吃最近一个真实结算点，再到下一个 review 重新判断；
// - 但机会识别不再因为历史拟合把 18:00 / 19:00 这类未来单边段提前累计进 headline carry；
// - 也就是说，rolling 保留“按结算段滚动”的执行方式，但去掉“跨未来段猜收益”的机会放大。
func buildRollingFundingSegments(
	cfg Config,
	now time.Time,
	longExchange string,
	longFunding entity.FundingSnapshot,
	longForecast fundingForecast,
	shortExchange string,
	shortFunding entity.FundingSnapshot,
	shortForecast fundingForecast,
) ([]fundingSegment, int64, int64) {
	nowMs := now.UnixMilli()
	nextReview := minPositiveInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs)
	syncBoundary := maxInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs)
	if nextReview <= nowMs || syncBoundary <= nowMs {
		return nil, nextReview, syncBoundary
	}

	pathLimit := syncBoundary
	if cfg.HoldHours > 0 {
		horizonMs := nowMs + int64(cfg.HoldHours*float64(time.Hour/time.Millisecond))
		if horizonMs > 0 && horizonMs < pathLimit {
			pathLimit = horizonMs
		}
	}
	if pathLimit <= nowMs {
		return nil, nextReview, syncBoundary
	}

	// forecast 参数仍然保留在签名里，是为了让 rolling / legacy 两套 plan builder
	// 维持一致的调用方式。当前 real-only rolling 不再消费它们。
	_, _ = longForecast, shortForecast

	segments := make([]fundingSegment, 0, 8)
	appendSegment := func(segment fundingSegment) {
		segment.SegmentRank = len(segments) + 1
		segments = append(segments, segment)
	}

	// 当前 next funding time 已完全对齐时，只能比较这一档真实 shared settlement。
	if longFunding.FundingTimeMs == shortFunding.FundingTimeMs {
		if longFunding.FundingTimeMs <= pathLimit {
			appendSegment(buildSharedRealFundingSegment(
				longExchange,
				longFunding,
				shortExchange,
				shortFunding,
			))
		}
		sortFundingSegments(segments)
		return segments, nextReview, syncBoundary
	}

	type legDescriptor struct {
		exchange string
		funding  entity.FundingSnapshot
		forecast fundingForecast
		isLong   bool
	}

	early := legDescriptor{exchange: longExchange, funding: longFunding, forecast: longForecast, isLong: true}
	if shortFunding.FundingTimeMs < longFunding.FundingTimeMs {
		early = legDescriptor{exchange: shortExchange, funding: shortFunding, forecast: shortForecast, isLong: false}
	}

	if early.funding.FundingTimeMs <= pathLimit {
		appendSegment(buildSingleFundingSegment(
			longExchange,
			shortExchange,
			early.funding.FundingRate,
			early.funding.FundingTimeMs,
			1,
			false,
			early.isLong,
		))
	}

	sortFundingSegments(segments)
	return segments, nextReview, syncBoundary
}

func buildSharedRealFundingSegment(
	longExchange string,
	longFunding entity.FundingSnapshot,
	shortExchange string,
	shortFunding entity.FundingSnapshot,
) fundingSegment {
	heldCarry := shortFunding.FundingRate - longFunding.FundingRate
	optimalLong, optimalShort := longExchange, shortExchange
	if heldCarry < 0 {
		optimalLong, optimalShort = shortExchange, longExchange
	}
	return fundingSegment{
		SettlementTimeMs:       maxInt64(longFunding.FundingTimeMs, shortFunding.FundingTimeMs),
		SegmentType:            fundingSegmentTypeSharedReal,
		UsesForecast:           false,
		SharedSettlement:       true,
		HeldLongExchange:       longExchange,
		HeldShortExchange:      shortExchange,
		OptimalLongExchange:    optimalLong,
		OptimalShortExchange:   optimalShort,
		DirectionMatchesHeld:   heldCarry >= 0,
		LongLegSettles:         true,
		ShortLegSettles:        true,
		LongLegRate:            longFunding.FundingRate,
		ShortLegRate:           shortFunding.FundingRate,
		LongLegEventIdx:        1,
		ShortLegEventIdx:       1,
		CarryRate:              math.Abs(heldCarry),
		HeldDirectionCarryRate: heldCarry,
		ComputationMode:        "rolling_cycle_aligned_shared_real",
	}
}

func buildSingleFundingSegment(
	longExchange string,
	shortExchange string,
	settledRate float64,
	settlementTimeMs int64,
	eventIdx int,
	usesForecast bool,
	settledLegIsLong bool,
) fundingSegment {
	heldCarry := settledRate
	optimalLong, optimalShort := shortExchange, longExchange
	longSettles := false
	shortSettles := false
	longRate := 0.0
	shortRate := 0.0
	longEventIdx := 0
	shortEventIdx := 0

	if settledLegIsLong {
		heldCarry = -settledRate
		optimalLong, optimalShort = longExchange, shortExchange
		longSettles = true
		longRate = settledRate
		longEventIdx = eventIdx
	} else {
		shortSettles = true
		shortRate = settledRate
		shortEventIdx = eventIdx
	}

	if heldCarry < 0 {
		optimalLong, optimalShort = shortExchange, longExchange
	}

	mode := "rolling_cycle_aligned_single_real"
	segmentType := fundingSegmentTypeSingleReal
	if usesForecast {
		mode = "rolling_cycle_aligned_single_forecast"
		segmentType = fundingSegmentTypeSingleForecast
	}

	return fundingSegment{
		SettlementTimeMs:       settlementTimeMs,
		SegmentType:            segmentType,
		UsesForecast:           usesForecast,
		SharedSettlement:       false,
		HeldLongExchange:       longExchange,
		HeldShortExchange:      shortExchange,
		OptimalLongExchange:    optimalLong,
		OptimalShortExchange:   optimalShort,
		DirectionMatchesHeld:   heldCarry >= 0,
		LongLegSettles:         longSettles,
		ShortLegSettles:        shortSettles,
		LongLegRate:            longRate,
		ShortLegRate:           shortRate,
		LongLegEventIdx:        longEventIdx,
		ShortLegEventIdx:       shortEventIdx,
		CarryRate:              math.Abs(heldCarry),
		HeldDirectionCarryRate: heldCarry,
		ComputationMode:        mode,
	}
}

func buildLegacyFundingProjectionsForConfig(cfg Config, now time.Time, longFunding entity.FundingSnapshot, longForecast fundingForecast, shortFunding entity.FundingSnapshot, shortForecast fundingForecast) []fundingProjection {
	nowMs := now.UnixMilli()
	candidateTimes := buildFundingCandidateTimesForConfig(cfg, nowMs, longFunding, shortFunding)
	return buildLegacyFundingProjectionsFromCandidateTimes(nowMs, candidateTimes, longFunding, longForecast, shortFunding, shortForecast)
}

func buildLegacyFundingProjections(now time.Time, holdHours float64, longFunding entity.FundingSnapshot, longForecast fundingForecast, shortFunding entity.FundingSnapshot, shortForecast fundingForecast) []fundingProjection {
	nowMs := now.UnixMilli()
	candidateTimes := buildFundingCandidateTimes(nowMs, longFunding, shortFunding, holdHours)
	return buildLegacyFundingProjectionsFromCandidateTimes(nowMs, candidateTimes, longFunding, longForecast, shortFunding, shortForecast)
}

func buildLegacyFundingProjectionsFromCandidateTimes(nowMs int64, candidateTimes []int64, longFunding entity.FundingSnapshot, longForecast fundingForecast, shortFunding entity.FundingSnapshot, shortForecast fundingForecast) []fundingProjection {
	if len(candidateTimes) == 0 {
		return nil
	}

	projections := make([]fundingProjection, 0, len(candidateTimes))
	for _, projectedTime := range candidateTimes {
		longCount := fundingEventCountUntil(nowMs, projectedTime, longFunding.FundingTimeMs, longFunding.FundingIntervalHours)
		shortCount := fundingEventCountUntil(nowMs, projectedTime, shortFunding.FundingTimeMs, shortFunding.FundingIntervalHours)
		if longCount == 0 && shortCount == 0 {
			continue
		}

		// legacy 模式保留旧项目的累计口径：
		// 在固定方向下，把窗口内双腿 funding 事件都累计起来，再做 shortCarry-longCarry。
		shortCarry := projectedLegFundingCarry(shortForecast, shortCount)
		longCarry := projectedLegFundingCarry(longForecast, longCount)
		carryRate := shortCarry - longCarry
		windowHours := float64(projectedTime-nowMs) / float64(time.Hour/time.Millisecond)
		if windowHours <= 0 {
			windowHours = 1.0 / 60.0
		}

		requiredEntryBy := int64(0)
		if longCount > 0 {
			requiredEntryBy = longFunding.FundingTimeMs
		}
		if shortCount > 0 {
			if requiredEntryBy == 0 || shortFunding.FundingTimeMs < requiredEntryBy {
				requiredEntryBy = shortFunding.FundingTimeMs
			}
		}

		projections = append(projections, fundingProjection{
			ProjectedFundingTimeMs:       projectedTime,
			RequiredEntryByFundingTimeMs: requiredEntryBy,
			LongFundingEventCount:        longCount,
			ShortFundingEventCount:       shortCount,
			FundingWindowHours:           windowHours,
			CarryRate:                    carryRate,
			CarryRateHourlyEquivalent:    carryRate / windowHours,
			ComputationMode:              legacyFundingComputationMode(longForecast, shortForecast),
			StrategyMode:                 StrategyModeLegacyProjection,
		})
	}
	sort.Slice(projections, func(i, j int) bool {
		if projections[i].ProjectedFundingTimeMs != projections[j].ProjectedFundingTimeMs {
			return projections[i].ProjectedFundingTimeMs < projections[j].ProjectedFundingTimeMs
		}
		if projections[i].CarryRate != projections[j].CarryRate {
			return projections[i].CarryRate > projections[j].CarryRate
		}
		if projections[i].CarryRateHourlyEquivalent != projections[j].CarryRateHourlyEquivalent {
			return projections[i].CarryRateHourlyEquivalent > projections[j].CarryRateHourlyEquivalent
		}
		return projections[i].RequiredEntryByFundingTimeMs < projections[j].RequiredEntryByFundingTimeMs
	})
	return projections
}

func legacyFundingComputationMode(longForecast, shortForecast fundingForecast) string {
	if longForecast.Regime != "" || shortForecast.Regime != "" {
		return "event_based_regime_aware_forecast"
	}
	return "event_based_known_next_funding"
}

func cloneFundingSegments(items []fundingSegment) []fundingSegment {
	if len(items) == 0 {
		return nil
	}
	out := make([]fundingSegment, len(items))
	copy(out, items)
	return out
}

func sortFundingSegments(items []fundingSegment) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].SettlementTimeMs != items[j].SettlementTimeMs {
			return items[i].SettlementTimeMs < items[j].SettlementTimeMs
		}
		if items[i].SegmentType != items[j].SegmentType {
			return items[i].SegmentType < items[j].SegmentType
		}
		return items[i].SegmentRank < items[j].SegmentRank
	})
	for i := range items {
		items[i].SegmentRank = i + 1
	}
}

func minPositiveInt64(left, right int64) int64 {
	switch {
	case left <= 0:
		return right
	case right <= 0:
		return left
	case left < right:
		return left
	default:
		return right
	}
}

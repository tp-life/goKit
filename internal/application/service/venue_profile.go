package service

import (
	"math"
	"strings"
)

// VenueProfile 描述某个交易所（venue）在策略层真正关心的“规则画像”。
//
// 这里刻意不直接存一大堆静态数字，而是允许用函数承载规则，原因有两点：
// 1. funding clamp 往往和 interval 有关，例如 1h / 4h / 8h 需要按周期缩放；
// 2. 某些 venue 的限制不是固定线，而是“基础机制上限 + 某些短周期自适应放宽”。
//
// 换句话说，VenueProfile 的职责不是保存交易所元数据，而是把
// “策略在估算 funding carry / execution friction 时需要知道的 venue 特性”
// 统一收口到一个地方，避免在多个 service 文件里散落 switch-case。
type VenueProfile struct {
	Name                           string
	FundingClampSource             string
	FundingClampFunc               func(intervalHours int) (floorRate, capRate float64, source string)
	AdaptiveShortIntervalClampFunc func(intervalHours int, currentRate, historyMean, historyStdDev, venueFloor, venueCap float64) (floorRate, capRate float64, source string, widened bool)
	ExecutionPenaltyMultiplier     float64
}

// VenueProfileRegistry 是 venue rules 的只读查找表。
//
// 当前策略里有两条链路会读取它：
// 1. FundingForecaster：决定 funding 预测路径可以落在什么 clamp 区间；
// 2. StrategyRunner / execution penalty：决定 venue 经验摩擦乘子。
//
// 这样做之后，新 venue 的接入成本会变成：
// “在 registry 中补一份 profile + 为它补测试”，
// 而不是继续修改 funding / execution 两条主流程代码。
type VenueProfileRegistry struct {
	profiles map[string]VenueProfile
}

func NewVenueProfileRegistry(profiles ...VenueProfile) *VenueProfileRegistry {
	registry := &VenueProfileRegistry{
		profiles: make(map[string]VenueProfile, len(profiles)),
	}
	for _, profile := range profiles {
		key := normalizeVenueName(profile.Name)
		if key == "" {
			continue
		}
		registry.profiles[key] = profile
	}
	return registry
}

// defaultVenueProfiles 是整个 service 包共享的一份默认规则表。
//
// 这里使用包级单例，而不是每次调用都重新构造，目的是：
// 1. 保证 StrategyRunner、FundingForecaster、测试 helper 读到的是同一份默认规则；
// 2. 避免后续有人在两个位置分别“改默认值”导致策略行为静默分叉；
// 3. 让 wrapper helper 仍然保留简单调用方式，同时不重复分配 registry。
var defaultVenueProfiles = NewVenueProfileRegistry(
	VenueProfile{
		Name:               "binance",
		FundingClampSource: "binance_like_cap",
		FundingClampFunc: func(intervalHours int) (floorRate, capRate float64, source string) {
			intervalHours = maxInt(intervalHours, 1)
			perHourCap := 0.0075 / 8.0
			cap := perHourCap * float64(intervalHours)
			return -cap, cap, "binance_like_cap"
		},
		AdaptiveShortIntervalClampFunc: binanceLikeAdaptiveShortIntervalClamp,
		ExecutionPenaltyMultiplier:     1.00,
	},
	VenueProfile{
		Name:               "aster",
		FundingClampSource: "binance_like_cap",
		FundingClampFunc: func(intervalHours int) (floorRate, capRate float64, source string) {
			intervalHours = maxInt(intervalHours, 1)
			perHourCap := 0.0075 / 8.0
			cap := perHourCap * float64(intervalHours)
			return -cap, cap, "binance_like_cap"
		},
		AdaptiveShortIntervalClampFunc: binanceLikeAdaptiveShortIntervalClamp,
		ExecutionPenaltyMultiplier:     1.10,
	},
	VenueProfile{
		Name:               "hyperliquid",
		FundingClampSource: "hyperliquid_cap",
		FundingClampFunc: func(intervalHours int) (floorRate, capRate float64, source string) {
			cap := 0.0040
			return -cap, cap, "hyperliquid_cap"
		},
		ExecutionPenaltyMultiplier: 1.15,
	},
)

func defaultVenueProfileRegistry() *VenueProfileRegistry {
	return defaultVenueProfiles
}

func (r *VenueProfileRegistry) Profile(exchangeName string) (VenueProfile, bool) {
	if r == nil {
		return VenueProfile{}, false
	}
	profile, ok := r.profiles[normalizeVenueName(exchangeName)]
	return profile, ok
}

func (r *VenueProfileRegistry) FundingClamp(exchangeName string, intervalHours int) (floorRate, capRate float64, source string) {
	profile, ok := r.Profile(exchangeName)
	if !ok || profile.FundingClampFunc == nil {
		return 0, 0, ""
	}
	return profile.FundingClampFunc(intervalHours)
}

func (r *VenueProfileRegistry) AdaptiveShortIntervalClamp(exchangeName string, intervalHours int, currentRate, historyMean, historyStdDev, venueFloor, venueCap float64) (floorRate, capRate float64, source string, widened bool) {
	profile, ok := r.Profile(exchangeName)
	if !ok || profile.AdaptiveShortIntervalClampFunc == nil {
		return 0, 0, "", false
	}
	return profile.AdaptiveShortIntervalClampFunc(intervalHours, currentRate, historyMean, historyStdDev, venueFloor, venueCap)
}

func (r *VenueProfileRegistry) ExecutionPenaltyMultiplier(exchangeName string) float64 {
	profile, ok := r.Profile(exchangeName)
	if !ok || profile.ExecutionPenaltyMultiplier <= 0 {
		return 1.05
	}
	return profile.ExecutionPenaltyMultiplier
}

func normalizeVenueName(exchangeName string) string {
	return strings.ToLower(strings.TrimSpace(exchangeName))
}

// binanceLikeAdaptiveShortIntervalClamp 用来处理 Binance/Aster 这类“规则来源相似、
// 但短周期 funding 不适合简单按 8h 线性压缩”的 venue。
//
// 设计意图：
//   - venue 基础 clamp 仍然提供“机制护栏”；
//   - 但当 1h/4h funding 本身已经显著偏离历史常态时，说明短周期 funding
//     可能正是 alpha 的来源，如果继续强行按线性 8h cap 收紧，会把真实边缘全部抹平；
//   - 因此这里只在短周期时提供“有上限的放宽”，而不是彻底取消约束。
func binanceLikeAdaptiveShortIntervalClamp(intervalHours int, currentRate, historyMean, historyStdDev, venueFloor, venueCap float64) (floorRate, capRate float64, source string, widened bool) {
	if intervalHours <= 0 || intervalHours >= 8 {
		return 0, 0, "", false
	}

	fullWindowCap := 0.0075
	dynamicCap := math.Max(math.Max(
		math.Abs(currentRate)*1.25,
		math.Abs(historyMean)+historyStdDev*4),
		math.Abs(venueCap),
	)
	dynamicCap = math.Min(dynamicCap, fullWindowCap)
	if dynamicCap <= math.Abs(venueCap) {
		return 0, 0, "", false
	}
	return -dynamicCap, dynamicCap, "binance_like_adaptive_short_interval_cap", true
}

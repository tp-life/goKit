package service

import (
	"context"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

type OpportunityQueryService struct {
	repo repository.OpportunityRepository
	cfg  Config
}

func NewOpportunityQueryService(repo repository.OpportunityRepository, cfg Config) *OpportunityQueryService {
	return &OpportunityQueryService{
		repo: repo,
		cfg:  cfg.normalize(),
	}
}

func (s *OpportunityQueryService) ListLatest(ctx context.Context, limit int) ([]entity.Opportunity, error) {
	// 默认返回“最新 batch 全量”，而不是再按单一结算周期做二次裁剪。
	//
	// 这样做的原因是：
	// 1. latest_profitable / strict_target 会让不同 symbol 落在不同的 projected funding window；
	// 2. 如果查询层再强行只保留“最近那一个结算周期”，同一个 symbol 就会在每轮重算时反复进出列表；
	// 3. Web / TUI 看起来就会像“列表一直轮动”，即便底层最新 batch 本身是稳定的。
	//
	// 因此机会接口现在的语义改成：
	// - 先取最新 batch 的完整结果；
	// - 再只应用用户可见的 limit；
	// - 是否按结算周期分组/筛选，交给前端展示层或未来的显式查询参数。
	items, err := s.repo.ListLatest(ctx, 0)
	if err != nil {
		return nil, err
	}
	if batchIsStale(time.Now().UTC(), latestOpportunityBatchAsOf(items), s.cfg) {
		return []entity.Opportunity{}, nil
	}
	return applyOpportunityLimit(items, limit), nil
}

func (s *OpportunityQueryService) ListLatestSummary(ctx context.Context, limit int) ([]repository.OpportunitySummary, error) {
	// Summary 列表与详情页/TUI 共用，因此这里同样返回“最新 batch 全量”。
	// 这样前端本地搜索、排序、筛选看到的是一套稳定的数据全集，而不是被后端
	// 先裁成“当前最近结算周期”的动态子集。
	items, err := s.repo.ListLatestSummary(ctx, 0)
	if err != nil {
		return nil, err
	}
	if batchIsStale(time.Now().UTC(), latestOpportunitySummaryBatchAsOf(items), s.cfg) {
		return []repository.OpportunitySummary{}, nil
	}
	return applyOpportunityLimit(items, limit), nil
}

func (s *OpportunityQueryService) GetByID(ctx context.Context, id uint) (*entity.Opportunity, error) {
	return s.repo.FindByID(ctx, id)
}

func applyOpportunityLimit[T any](items []T, limit int) []T {
	if limit <= 0 {
		limit = 20
	}
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func latestOpportunityBatchAsOf(items []entity.Opportunity) int64 {
	if len(items) == 0 {
		return 0
	}
	return items[0].AsOfTimeMs
}

func latestOpportunitySummaryBatchAsOf(items []repository.OpportunitySummary) int64 {
	if len(items) == 0 {
		return 0
	}
	return items[0].AsOfTimeMs
}

// batchIsStale 用来避免“当前已经没有新机会，但界面仍然展示上一次非空 batch”。
//
// 机会循环现在只有在算出至少一条机会时才会落库，因此一旦市场转为空窗期，
// DB 里“最新一批机会”就会停留在最后一个非空 batch。这里加一层时间闸门后：
// - 新 batch 正常滚动时，列表继续展示；
// - 最新 batch 已经超过合理刷新窗口时，列表直接返回空，而不是继续展示旧机会。
func batchIsStale(now time.Time, asOfTimeMs int64, cfg Config) bool {
	if asOfTimeMs <= 0 {
		return false
	}
	age := now.Sub(time.UnixMilli(asOfTimeMs))
	if age <= 0 {
		return false
	}
	return age > opportunityFreshnessWindow(cfg.normalize())
}

func opportunityFreshnessWindow(cfg Config) time.Duration {
	window := 15 * time.Second
	if cfg.OpportunityCalcInterval > 0 {
		candidate := cfg.OpportunityCalcInterval * 3
		if candidate > window {
			window = candidate
		}
	}
	if cfg.MaxDataAge > 0 {
		candidate := cfg.MaxDataAge + cfg.OpportunityCalcInterval
		if candidate > window {
			window = candidate
		}
	}
	return window
}

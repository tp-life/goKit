package handler

import (
	"github.com/gofiber/fiber/v2"

	"goKit/internal/application/service"
	"goKit/internal/interface/http/response"
)

// StrategyHandler 策略接口处理器
type StrategyHandler struct {
	strategyService *service.StrategyService
}

// NewStrategyHandler 构造函数
func NewStrategyHandler(strategyService *service.StrategyService) *StrategyHandler {
	return &StrategyHandler{
		strategyService: strategyService,
	}
}

// =========================================================================
// API 1: 获取国家队重仓股
// GET /api/strategy/national-team/heavy?min_ratio=5.0&limit=50
// =========================================================================

func (h *StrategyHandler) HandleHeavyHoldings(c *fiber.Ctx) error {
	// 1. 使用 Fiber 内置方法解析参数并赋默认值
	// QueryFloat 和 QueryInt 会自动处理空值或解析失败的情况，直接返回默认值
	minRatio := c.QueryFloat("min_ratio", 5.0)
	limit := c.QueryInt("limit", 50)

	// 2. 获取标准 context (Fiber 中传递给 GORM 等底层库应使用 UserContext)
	ctx := c.UserContext()

	// 3. 调用底层 Service
	results, err := h.strategyService.GetNationalTeamHeavyHoldings(ctx, minRatio, limit)
	if err != nil {
		// 采用统一 response 包构建错误返回
		return response.ErrInternal(err, "获取国家队重仓股失败: ")
	}

	// 4. 采用统一 response 包构建成功返回
	return response.Success(c, results)
}

// =========================================================================
// API 2: 获取国家队“黄金坑” (持仓高 + 股价低)
// GET /api/strategy/national-team/golden-pit?min_ratio=2.0
// =========================================================================

func (h *StrategyHandler) HandleGoldenPit(c *fiber.Ctx) error {
	// 1. 解析参数
	minRatio := c.QueryFloat("min_ratio", 2.0)

	// 2. 调用底层 Service
	ctx := c.UserContext()
	results, err := h.strategyService.GetNationalTeamGoldenPit(ctx, minRatio)
	if err != nil {
		return response.ErrInternal(err, "分析黄金坑标的失败: ")
	}

	// 3. 返回成功响应
	return response.Success(c, results)
}

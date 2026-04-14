package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

const (
	sameExchangePriceRiskReasonEligible      = "eligible"
	sameExchangePriceRiskReason1hPriceShock  = "max_1h_price_shock"
	sameExchangePriceRiskLookbackDuration    = time.Hour
	sameExchangePriceRiskSnapshotSampleLimit = 180
)

type sameExchangePriceRiskAssessment struct {
	Allowed           bool
	Reason            string
	CurrentMarkPrice  float64
	BaselineMarkPrice float64
	PriceShockRatio   float64
}

func assessSameExchangePriceRisk(
	ctx context.Context,
	cfg Config,
	marketRepo repository.MarketDataRepository,
	arbitrageMode string,
	exchangeName string,
	symbol string,
	current entity.FundingSnapshot,
	now time.Time,
) sameExchangePriceRiskAssessment {
	assessment := sameExchangePriceRiskAssessment{
		Allowed:          true,
		Reason:           sameExchangePriceRiskReasonEligible,
		CurrentMarkPrice: fundingSnapshotReferencePrice(current),
	}
	if normalizeArbitrageMode(arbitrageMode, normalizeArbitrageMode(cfg.ArbitrageMode, ArbitrageModeCrossExchange)) != ArbitrageModeSameExchangeSpotPerp {
		return assessment
	}
	if marketRepo == nil || cfg.SameExchangeMax1hPriceShockRatio <= 0 {
		return assessment
	}
	if assessment.CurrentMarkPrice <= 0 || stringsBlank(exchangeName) || stringsBlank(symbol) {
		return assessment
	}
	lookbackStart := now.UTC().Add(-sameExchangePriceRiskLookbackDuration)
	history, err := marketRepo.RecentFundingSnapshots(ctx, exchangeName, symbol, lookbackStart, sameExchangePriceRiskSnapshotSampleLimit)
	if err != nil || len(history) == 0 {
		return assessment
	}
	baseline := assessment.CurrentMarkPrice
	foundBaseline := false
	oldestEventTime := int64(0)
	for _, item := range history {
		price := fundingSnapshotReferencePrice(item)
		if price <= 0 {
			continue
		}
		if !foundBaseline || oldestEventTime == 0 || item.EventTimeMs < oldestEventTime {
			baseline = price
			oldestEventTime = item.EventTimeMs
			foundBaseline = true
		}
	}
	if !foundBaseline || baseline <= 0 {
		return assessment
	}
	assessment.BaselineMarkPrice = baseline
	assessment.PriceShockRatio = (assessment.CurrentMarkPrice - baseline) / baseline
	if assessment.PriceShockRatio <= cfg.SameExchangeMax1hPriceShockRatio+1e-9 {
		return assessment
	}
	assessment.Allowed = false
	assessment.Reason = sameExchangePriceRiskReason1hPriceShock
	return assessment
}

func sameExchangePriceRiskRejectReason(cfg Config, assessment sameExchangePriceRiskAssessment) string {
	switch assessment.Reason {
	case sameExchangePriceRiskReason1hPriceShock:
		return fmt.Sprintf(
			"same_exchange price guard: perp mark moved +%.2f%% over last 1h > max +%.2f%% (baseline %.4f -> current %.4f)",
			assessment.PriceShockRatio*100,
			cfg.SameExchangeMax1hPriceShockRatio*100,
			assessment.BaselineMarkPrice,
			assessment.CurrentMarkPrice,
		)
	default:
		return ""
	}
}

func fundingSnapshotReferencePrice(item entity.FundingSnapshot) float64 {
	switch {
	case item.MarkPrice > 0:
		return item.MarkPrice
	case item.IndexPrice > 0:
		return item.IndexPrice
	case item.EstimatedSettlePrice > 0:
		return item.EstimatedSettlePrice
	default:
		return 0
	}
}

func stringsBlank(v string) bool {
	return strings.TrimSpace(v) == ""
}

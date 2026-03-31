package marketdata

import "context"

// FeedClient 定义外部行情基础设施向上层暴露的能力边界。
type FeedClient interface {
	GetBinanceBTCPrice(ctx context.Context) (float64, error)
	SubscribeBinanceBTC(ctx context.Context, onPrice func(float64)) error
	GetCryptoPrice(ctx context.Context, startTime, endTime string) (openPrice *float64, closePrice *float64, err error)
	SubscribeRTDS(ctx context.Context, onPrice func(float64)) error
}

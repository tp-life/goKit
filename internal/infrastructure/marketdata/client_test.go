package marketdata

import "testing"

// TestExtractRTDSPriceFromPayloadBatch 验证 RTDS 的 payload.data 数组结构可以被正确解析。
func TestExtractRTDSPriceFromPayloadBatch(t *testing.T) {
	message := []byte(`{
		"topic":"crypto_prices",
		"payload":{
			"symbol":"btc/usd",
			"data":[
				{"value":111111.11},
				{"value":111222.22}
			]
		}
	}`)

	price, ok := extractRTDSPrice(message, "btc/usd")
	if !ok {
		t.Fatalf("expected RTDS payload batch to be parsed")
	}
	if price != 111222.22 {
		t.Fatalf("expected latest batch price 111222.22, got %.2f", price)
	}
}

// TestExtractRTDSPriceFromPayloadValue 验证 RTDS 的 payload.value 单值结构可以被正确解析。
func TestExtractRTDSPriceFromPayloadValue(t *testing.T) {
	message := []byte(`{
		"topic":"crypto_prices",
		"payload":{
			"symbol":"btc/usd",
			"value":"110987.65"
		}
	}`)

	price, ok := extractRTDSPrice(message, "btc/usd")
	if !ok {
		t.Fatalf("expected RTDS payload value to be parsed")
	}
	if price != 110987.65 {
		t.Fatalf("expected payload value 110987.65, got %.2f", price)
	}
}

// TestExtractBinancePriceFromTradeMessage 验证 Binance trade 流消息里的成交价字段可以被正确解析。
func TestExtractBinancePriceFromTradeMessage(t *testing.T) {
	message := []byte(`{"e":"trade","s":"BTCUSDT","p":"109876.54"}`)

	price, ok := extractBinancePrice(message)
	if !ok {
		t.Fatalf("expected Binance trade price to be parsed")
	}
	if price != 109876.54 {
		t.Fatalf("expected trade price 109876.54, got %.2f", price)
	}
}

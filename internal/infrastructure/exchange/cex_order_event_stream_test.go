package exchange

import "testing"

func TestSupportsOrderEventStream_BinanceAndAsterWhenAPIKeyPresent(t *testing.T) {
	binance := &CEXTradeClient{
		name:      "binance",
		cfg:       ExchangeConfig{Enabled: true},
		apiKey:    "key",
		apiSecret: "secret",
	}
	if !binance.supportsOrderEventStream() {
		t.Fatalf("expected binance adapter to expose order event stream capability")
	}

	aster := &CEXTradeClient{
		name:   "aster",
		cfg:    ExchangeConfig{Enabled: true},
		apiKey: "key",
	}
	if !aster.supportsOrderEventStream() {
		t.Fatalf("expected aster adapter to expose order event stream capability when api key is present")
	}

	asterSignerOnly := &CEXTradeClient{
		name: "aster",
		cfg:  ExchangeConfig{Enabled: true},
	}
	if asterSignerOnly.supportsOrderEventStream() {
		t.Fatalf("expected aster signer-only adapter to keep order event stream capability disabled without api key")
	}
}

func TestParseUserDataOrderEvent_OrderTradeUpdate(t *testing.T) {
	client := &CEXTradeClient{name: "aster"}

	event, ok, err := client.parseUserDataOrderEvent([]byte(`{
		"e":"ORDER_TRADE_UPDATE",
		"E":1710000000000,
		"T":1710000000123,
		"o":{
			"c":"cid-1",
			"X":"FILLED",
			"ap":"101.25",
			"z":"2",
			"i":123456
		}
	}`))
	if err != nil {
		t.Fatalf("expected parse success, got error: %v", err)
	}
	if !ok {
		t.Fatalf("expected ORDER_TRADE_UPDATE to be published")
	}
	if event.Source != "aster_user_stream" {
		t.Fatalf("expected event source to be aster_user_stream, got %s", event.Source)
	}
	if event.Exchange != "aster" {
		t.Fatalf("expected exchange to be aster, got %s", event.Exchange)
	}
	if event.ClientOrderID != "cid-1" {
		t.Fatalf("expected client order id cid-1, got %s", event.ClientOrderID)
	}
	if event.VenueOrderID != "123456" {
		t.Fatalf("expected venue order id 123456, got %s", event.VenueOrderID)
	}
	if event.Status != "FILLED" {
		t.Fatalf("expected status FILLED, got %s", event.Status)
	}
	if event.ExecutedQty != 2 {
		t.Fatalf("expected executed qty 2, got %v", event.ExecutedQty)
	}
	if event.AveragePrice != 101.25 {
		t.Fatalf("expected average price 101.25, got %v", event.AveragePrice)
	}
	if !event.Terminal {
		t.Fatalf("expected FILLED update to be terminal")
	}
	if event.OccurredAtMs != 1710000000123 {
		t.Fatalf("expected transaction timestamp to win, got %d", event.OccurredAtMs)
	}
}

func TestParseUserDataOrderEvent_ListenKeyExpiredReturnsError(t *testing.T) {
	client := &CEXTradeClient{name: "binance"}

	_, ok, err := client.parseUserDataOrderEvent([]byte(`{"e":"listenKeyExpired","E":1710000000000}`))
	if err == nil {
		t.Fatalf("expected listenKeyExpired to return an error")
	}
	if ok {
		t.Fatalf("expected listenKeyExpired not to publish an order event")
	}
}

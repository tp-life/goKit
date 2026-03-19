package exchange

import (
	"testing"
	"time"
)

func TestBybitSupportsOrderEventStreamWhenCredentialsPresent(t *testing.T) {
	client := &BybitV5TradeClient{
		name:      "bybit",
		cfg:       ExchangeConfig{Enabled: true},
		apiKey:    "key",
		apiSecret: "secret",
	}
	if !client.supportsOrderEventStream() {
		t.Fatalf("expected bybit adapter to expose private order stream capability")
	}
	if !client.Capabilities().SupportsOrderEventStream {
		t.Fatalf("expected capabilities to surface private order stream support")
	}
}

func TestBybitBuildPrivateAuthRequest_UsesRealtimePayloadSignature(t *testing.T) {
	client := &BybitV5TradeClient{
		name:      "bybit",
		apiKey:    "key-1",
		apiSecret: "secret-1",
	}

	now := time.UnixMilli(1710000000000)
	req := client.buildPrivateAuthRequest(now)
	if req.Op != "auth" {
		t.Fatalf("expected auth op, got %q", req.Op)
	}
	if len(req.Args) != 3 {
		t.Fatalf("expected 3 auth args, got %d", len(req.Args))
	}
	if got := req.Args[0]; got != "key-1" {
		t.Fatalf("expected api key arg, got %#v", got)
	}

	expires, ok := req.Args[1].(int64)
	if !ok {
		t.Fatalf("expected expires arg to be int64, got %T", req.Args[1])
	}
	expectedExpires := now.Add(10 * time.Second).UnixMilli()
	if expires != expectedExpires {
		t.Fatalf("expected expires %d, got %d", expectedExpires, expires)
	}

	expectedSignature := hmacHex("secret-1", "GET/realtime1710000010000")
	if got := req.Args[2]; got != expectedSignature {
		t.Fatalf("expected signature %q, got %#v", expectedSignature, got)
	}
}

func TestBybitParsePrivateOrderEvents_OrderLinearTopic(t *testing.T) {
	client := &BybitV5TradeClient{name: "bybit", category: "linear"}

	events, ok, err := client.parsePrivateOrderEvents([]byte(`{
		"id":"msg-1",
		"topic":"order.linear",
		"creationTime":1710000000000,
		"data":[
			{
				"category":"linear",
				"orderId":"venue-1",
				"orderLinkId":"client-1",
				"orderStatus":"Filled",
				"avgPrice":"60010.5",
				"cumExecQty":"0.01",
				"updatedTime":"1710000000123"
			}
		]
	}`))
	if err != nil {
		t.Fatalf("expected parse success, got error: %v", err)
	}
	if !ok {
		t.Fatalf("expected order topic to publish events")
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}

	event := events[0]
	if event.Source != "bybit_private_stream" {
		t.Fatalf("expected bybit_private_stream source, got %s", event.Source)
	}
	if event.Exchange != "bybit" {
		t.Fatalf("expected exchange bybit, got %s", event.Exchange)
	}
	if event.ClientOrderID != "client-1" || event.VenueOrderID != "venue-1" {
		t.Fatalf("unexpected order identifiers %#v", event)
	}
	if event.Status != "FILLED" {
		t.Fatalf("expected FILLED status, got %s", event.Status)
	}
	if event.ExecutedQty != 0.01 || event.AveragePrice != 60010.5 {
		t.Fatalf("unexpected execution payload %#v", event)
	}
	if !event.Terminal {
		t.Fatalf("expected FILLED update to be terminal")
	}
	if event.OccurredAtMs != 1710000000123 {
		t.Fatalf("expected updatedTime to win, got %d", event.OccurredAtMs)
	}
}

func TestBybitParsePrivateOrderEvents_ControlMessageIgnored(t *testing.T) {
	client := &BybitV5TradeClient{name: "bybit", category: "linear"}

	events, ok, err := client.parsePrivateOrderEvents([]byte(`{"op":"pong"}`))
	if err != nil {
		t.Fatalf("expected control message to be ignored, got error: %v", err)
	}
	if ok {
		t.Fatalf("expected control message not to publish events")
	}
	if len(events) != 0 {
		t.Fatalf("expected no events for control message, got %d", len(events))
	}
}

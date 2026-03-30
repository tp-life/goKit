package exchange

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"goKit/internal/domain/entity"
)

type testMarketProvider struct {
	funding map[string][]entity.Symbol
	book    map[string][]entity.Symbol
}

func (p testMarketProvider) FundingSymbols(exchangeName string) []entity.Symbol {
	return append([]entity.Symbol(nil), p.funding[strings.ToLower(exchangeName)]...)
}

func (p testMarketProvider) BookSymbols(exchangeName string) []entity.Symbol {
	return append([]entity.Symbol(nil), p.book[strings.ToLower(exchangeName)]...)
}

type testMarketSink struct {
	symbols  []entity.Symbol
	funding  []entity.FundingSnapshot
	book     []entity.BookTopSnapshot
	statuses []ConnectorStatus
}

func (s *testMarketSink) UpsertSymbol(symbol entity.Symbol) {
	s.symbols = append(s.symbols, symbol)
}

func (s *testMarketSink) UpsertFunding(item entity.FundingSnapshot) {
	s.funding = append(s.funding, item)
}

func (s *testMarketSink) UpsertBookTop(item entity.BookTopSnapshot) {
	s.book = append(s.book, item)
}

func (s *testMarketSink) UpdateStatus(status ConnectorStatus) {
	s.statuses = append(s.statuses, status)
}

func TestBybitV5MarketFetchTradableSymbols_PaginatesAndFiltersPerpetuals(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/market/instruments-info" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		cursor := r.URL.Query().Get("cursor")
		switch cursor {
		case "":
			_, _ = io.WriteString(w, `{
				"retCode":0,
				"retMsg":"OK",
				"result":{
					"category":"linear",
					"nextPageCursor":"page-2",
					"list":[
						{
							"symbol":"BTCUSDT",
							"status":"Trading",
							"contractType":"LinearPerpetual",
							"baseCoin":"BTC",
							"quoteCoin":"USDT",
							"settleCoin":"USDT",
							"fundingInterval":480,
							"priceFilter":{"tickSize":"0.10"},
							"lotSizeFilter":{"qtyStep":"0.001","minOrderQty":"0.001","minNotionalValue":"5"}
						},
						{
							"symbol":"BTCUSDC",
							"status":"Trading",
							"contractType":"LinearPerpetual",
							"baseCoin":"BTC",
							"quoteCoin":"USDC",
							"settleCoin":"USDC",
							"fundingInterval":480,
							"priceFilter":{"tickSize":"0.10"},
							"lotSizeFilter":{"qtyStep":"0.001","minOrderQty":"0.001","minNotionalValue":"5"}
						},
						{
							"symbol":"SOLUSDT-30JUN27",
							"status":"Trading",
							"contractType":"LinearFutures",
							"baseCoin":"SOL",
							"quoteCoin":"USDT",
							"settleCoin":"USDT",
							"fundingInterval":0,
							"priceFilter":{"tickSize":"0.001"},
							"lotSizeFilter":{"qtyStep":"0.1","minOrderQty":"0.1","minNotionalValue":"5"}
						}
					]
				},
				"time":1710000000000
			}`)
		case "page-2":
			_, _ = io.WriteString(w, `{
				"retCode":0,
				"retMsg":"OK",
				"result":{
					"category":"linear",
					"nextPageCursor":"",
					"list":[
						{
							"symbol":"ETHUSDT",
							"status":"Trading",
							"contractType":"LinearPerpetual",
							"baseCoin":"ETH",
							"quoteCoin":"USDT",
							"settleCoin":"USDT",
							"fundingInterval":480,
							"priceFilter":{"tickSize":"0.01"},
							"lotSizeFilter":{"qtyStep":"0.01","minOrderQty":"0.01","minNotionalValue":"5"}
						}
					]
				},
				"time":1710000001000
			}`)
		default:
			t.Fatalf("unexpected cursor %q", cursor)
		}
	}))
	defer server.Close()

	client := NewBybitV5MarketAdapter("bybit", ExchangeConfig{
		Enabled:     true,
		RestBaseURL: server.URL,
	}, logger).(*BybitV5MarketClient)

	symbols, err := client.FetchTradableSymbols(context.Background(), "USDT", map[string]struct{}{
		"BTC": {},
		"ETH": {},
	})
	if err != nil {
		t.Fatalf("FetchTradableSymbols error = %v", err)
	}
	if len(symbols) != 2 {
		t.Fatalf("expected 2 tradable perpetual symbols, got %d", len(symbols))
	}
	if symbols[0].Symbol != "BTC" || symbols[0].VenueSymbol != "BTCUSDT" {
		t.Fatalf("unexpected first symbol %#v", symbols[0])
	}
	if symbols[0].FundingIntervalHours != 8 {
		t.Fatalf("expected BTC funding interval 8h, got %d", symbols[0].FundingIntervalHours)
	}
	if symbols[1].Symbol != "ETH" || symbols[1].VenueSymbol != "ETHUSDT" {
		t.Fatalf("unexpected second symbol %#v", symbols[1])
	}
}

func TestBybitV5MarketPollTickers_UpdatesFundingAndBook(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/market/tickers" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{
			"retCode":0,
			"retMsg":"OK",
			"result":{
				"category":"linear",
				"list":[
					{
						"symbol":"BTCUSDT",
						"bid1Price":"61000",
						"bid1Size":"12",
						"ask1Price":"61000.5",
						"ask1Size":"11",
						"markPrice":"61000.2",
						"indexPrice":"60999.8",
						"fundingRate":"0.0001",
						"nextFundingTime":"1710003600000"
					}
				]
			},
			"time":1710000002000
		}`)
	}))
	defer server.Close()

	client := NewBybitV5MarketAdapter("bybit", ExchangeConfig{
		Enabled:     true,
		RestBaseURL: server.URL,
	}, logger).(*BybitV5MarketClient)

	meta := entity.Symbol{
		Exchange:             "bybit",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingIntervalHours: 8,
	}
	provider := testMarketProvider{
		funding: map[string][]entity.Symbol{"bybit": {meta}},
		book:    map[string][]entity.Symbol{"bybit": {meta}},
	}
	sink := &testMarketSink{}

	client.pollTickers(context.Background(), provider, sink)

	if len(sink.funding) != 1 {
		t.Fatalf("expected 1 funding snapshot, got %d", len(sink.funding))
	}
	if len(sink.book) != 1 {
		t.Fatalf("expected 1 book snapshot, got %d", len(sink.book))
	}
	if sink.funding[0].FundingRate != 0.0001 || sink.funding[0].FundingTimeMs != 1710003600000 {
		t.Fatalf("unexpected funding snapshot %#v", sink.funding[0])
	}
	if sink.book[0].BidPrice != 61000 || sink.book[0].AskPrice != 61000.5 {
		t.Fatalf("unexpected book snapshot %#v", sink.book[0])
	}
	if len(sink.statuses) == 0 {
		t.Fatalf("expected connector status update")
	}
	lastStatus := sink.statuses[len(sink.statuses)-1]
	if !lastStatus.MarkPriceConnected || !lastStatus.BookTickerConnected {
		t.Fatalf("expected both market and book status connected, got %#v", lastStatus)
	}
}

func TestBybitV5MarketHandleTickerStreamMessage_UpdatesFundingAndBook(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewBybitV5MarketAdapter("bybit", ExchangeConfig{
		Enabled: true,
	}, logger).(*BybitV5MarketClient)

	meta := entity.Symbol{
		Exchange:             "bybit",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingIntervalHours: 8,
	}
	fundingWatch := map[string]entity.Symbol{"BTCUSDT": meta}
	bookWatch := map[string]entity.Symbol{"BTCUSDT": meta}
	sink := &testMarketSink{}
	state := map[string]bybitTicker{}

	client.handleTickerStreamMessage([]byte(`{
		"topic":"tickers.BTCUSDT",
		"type":"snapshot",
		"ts":1710000003000,
		"data":{
			"symbol":"BTCUSDT",
			"markPrice":"61000.2",
			"indexPrice":"60999.8",
			"fundingRate":"0.0001",
			"nextFundingTime":"1710003600000",
			"bid1Price":"61000",
			"bid1Size":"12",
			"ask1Price":"61000.5",
			"ask1Size":"11"
		}
	}`), fundingWatch, bookWatch, state, sink)

	if len(sink.funding) != 1 || len(sink.book) != 1 {
		t.Fatalf("expected first snapshot to update both funding and book, got funding=%d book=%d", len(sink.funding), len(sink.book))
	}
	if sink.funding[0].FundingRate != 0.0001 || sink.book[0].BidPrice != 61000 {
		t.Fatalf("unexpected first stream update funding=%#v book=%#v", sink.funding[0], sink.book[0])
	}

	client.handleTickerStreamMessage([]byte(`{
		"topic":"tickers.BTCUSDT",
		"type":"delta",
		"ts":1710000003200,
		"data":{
			"symbol":"BTCUSDT",
			"bid1Price":"61001",
			"ask1Price":"61001.5"
		}
	}`), fundingWatch, bookWatch, state, sink)

	if len(sink.funding) != 2 || len(sink.book) != 2 {
		t.Fatalf("expected delta to continue updating both outputs, got funding=%d book=%d", len(sink.funding), len(sink.book))
	}
	lastFunding := sink.funding[len(sink.funding)-1]
	lastBook := sink.book[len(sink.book)-1]
	if lastFunding.FundingRate != 0.0001 || lastFunding.MarkPrice != 61000.2 {
		t.Fatalf("expected delta merge to preserve previous funding fields, got %#v", lastFunding)
	}
	if lastBook.BidPrice != 61001 || lastBook.AskPrice != 61001.5 {
		t.Fatalf("unexpected book prices after delta merge %#v", lastBook)
	}
}

func TestBybitV5MarketHandleTickerStreamMessage_IgnoresControlMessages(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewBybitV5MarketAdapter("bybit", ExchangeConfig{
		Enabled: true,
	}, logger).(*BybitV5MarketClient)
	sink := &testMarketSink{}
	state := map[string]bybitTicker{}

	client.handleTickerStreamMessage([]byte(`{"op":"subscribe","success":true}`), nil, nil, state, sink)

	if len(sink.funding) != 0 || len(sink.book) != 0 {
		t.Fatalf("expected control message to be ignored")
	}
}

func TestBybitV5MarketHandleTickerStreamMessage_FundingOnlyDoesNotWriteBookWhenOrderbookOwnsBook(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewBybitV5MarketAdapter("bybit", ExchangeConfig{
		Enabled: true,
	}, logger).(*BybitV5MarketClient)

	meta := entity.Symbol{
		Exchange:             "bybit",
		Symbol:               "BTC",
		VenueSymbol:          "BTCUSDT",
		FundingIntervalHours: 8,
	}
	fundingWatch := map[string]entity.Symbol{"BTCUSDT": meta}
	sink := &testMarketSink{}
	state := map[string]bybitTicker{}

	client.handleTickerStreamMessage([]byte(`{
		"topic":"tickers.BTCUSDT",
		"type":"snapshot",
		"ts":1710000003000,
		"data":{
			"symbol":"BTCUSDT",
			"markPrice":"61000.2",
			"indexPrice":"60999.8",
			"fundingRate":"0.0001",
			"nextFundingTime":"1710003600000",
			"bid1Price":"61000",
			"bid1Size":"12",
			"ask1Price":"61000.5",
			"ask1Size":"11"
		}
	}`), fundingWatch, nil, state, sink)

	if len(sink.funding) != 1 {
		t.Fatalf("expected funding update, got %d", len(sink.funding))
	}
	if len(sink.book) != 0 {
		t.Fatalf("expected ticker path not to publish book when orderbook is authoritative, got %d", len(sink.book))
	}
}

func TestBybitV5MarketHandleOrderbookStreamMessage_UpdatesBookTop(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewBybitV5MarketAdapter("bybit", ExchangeConfig{
		Enabled: true,
	}, logger).(*BybitV5MarketClient)

	meta := entity.Symbol{
		Exchange:    "bybit",
		Symbol:      "BTC",
		VenueSymbol: "BTCUSDT",
	}
	bookWatch := map[string]entity.Symbol{"BTCUSDT": meta}
	sink := &testMarketSink{}

	client.handleOrderbookStreamMessage([]byte(`{
		"topic":"orderbook.1.BTCUSDT",
		"type":"snapshot",
		"ts":1710000004000,
		"cts":1710000003998,
		"data":{
			"s":"BTCUSDT",
			"b":[["61002","9"]],
			"a":[["61002.5","8"]]
		}
	}`), bookWatch, sink)

	if len(sink.book) != 1 {
		t.Fatalf("expected 1 book snapshot, got %d", len(sink.book))
	}
	lastBook := sink.book[0]
	if lastBook.BidPrice != 61002 || lastBook.BidQty != 9 || lastBook.AskPrice != 61002.5 || lastBook.AskQty != 8 {
		t.Fatalf("unexpected orderbook-derived book top %#v", lastBook)
	}
	if lastBook.EventTimeMs != 1710000003998 {
		t.Fatalf("expected cts to win as event time, got %d", lastBook.EventTimeMs)
	}
}

func TestBybitV5MarketHandleOrderbookStreamMessage_IgnoresControlMessages(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	client := NewBybitV5MarketAdapter("bybit", ExchangeConfig{
		Enabled: true,
	}, logger).(*BybitV5MarketClient)
	sink := &testMarketSink{}

	client.handleOrderbookStreamMessage([]byte(`{"op":"subscribe","success":true}`), nil, sink)

	if len(sink.book) != 0 {
		t.Fatalf("expected control message not to publish book updates")
	}
}

func TestBybitV5TradePlaceOrder_SignsRequestAndParsesResponse(t *testing.T) {
	t.Setenv("BYBIT_API_KEY", "key-1")
	t.Setenv("BYBIT_API_SECRET", "secret-1")
	t.Setenv("BYBIT_REFERER", "broker-ref")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/order/create" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("X-BAPI-API-KEY"); got != "key-1" {
			t.Fatalf("unexpected api key header %q", got)
		}
		if got := r.Header.Get("X-Referer"); got != "broker-ref" {
			t.Fatalf("unexpected referer header %q", got)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body error = %v", err)
		}

		expectedSignature := hmacHex("secret-1", r.Header.Get("X-BAPI-TIMESTAMP")+"key-1"+r.Header.Get("X-BAPI-RECV-WINDOW")+string(body))
		if got := r.Header.Get("X-BAPI-SIGN"); got != expectedSignature {
			t.Fatalf("unexpected signature %q, expected %q", got, expectedSignature)
		}

		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body error = %v", err)
		}
		if payload["timeInForce"] != "PostOnly" {
			t.Fatalf("expected GTX to normalize to PostOnly, got %#v", payload["timeInForce"])
		}
		if payload["reduceOnly"] != true {
			t.Fatalf("expected reduceOnly=true, got %#v", payload["reduceOnly"])
		}

		_, _ = io.WriteString(w, `{
			"retCode":0,
			"retMsg":"OK",
			"result":{"orderId":"order-1","orderLinkId":"cli-1"},
			"time":1710000003000
		}`)
	}))
	defer server.Close()

	client := NewBybitV5TradeAdapter("bybit", ExchangeConfig{
		Enabled:     true,
		RestBaseURL: server.URL,
		Auth: AuthConfig{
			APIKeyEnv:    "BYBIT_API_KEY",
			APISecretEnv: "BYBIT_API_SECRET",
			ExtraEnv: map[string]string{
				"referer": "BYBIT_REFERER",
			},
		},
	}, logger).(*BybitV5TradeClient)

	result, err := client.PlaceOrder(context.Background(), TradeOrderRequest{
		CanonicalSymbol: "BTC",
		VenueSymbol:     "BTCUSDT",
		Side:            "BUY",
		OrderType:       "LIMIT",
		TimeInForce:     "GTX",
		Quantity:        0.01,
		Price:           60000,
		ReduceOnly:      true,
		ClientOrderID:   "cli-1",
	})
	if err != nil {
		t.Fatalf("PlaceOrder error = %v", err)
	}
	if result.VenueOrderID != "order-1" || result.ClientOrderID != "cli-1" || result.Status != "SUBMITTED" {
		t.Fatalf("unexpected order result %#v", result)
	}
}

func TestBybitV5TradeParsesOrderPositionAndAccountResponses(t *testing.T) {
	t.Setenv("BYBIT_API_KEY", "key-2")
	t.Setenv("BYBIT_API_SECRET", "secret-2")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v5/order/realtime":
			_, _ = io.WriteString(w, `{
				"retCode":0,
				"retMsg":"OK",
				"result":{
					"category":"linear",
					"list":[
						{
							"orderId":"order-2",
							"orderLinkId":"cli-2",
							"orderStatus":"PartiallyFilled",
							"avgPrice":"60010",
							"cumExecQty":"0.005"
						}
					]
				},
				"time":1710000004000
			}`)
		case "/v5/position/list":
			_, _ = io.WriteString(w, `{
				"retCode":0,
				"retMsg":"OK",
				"result":{
					"category":"linear",
					"list":[
						{
							"symbol":"BTCUSDT",
							"side":"Sell",
							"size":"0.02",
							"avgPrice":"60100",
							"markPrice":"60050",
							"unrealisedPnl":"1.25"
						}
					]
				},
				"time":1710000005000
			}`)
		case "/v5/account/wallet-balance":
			_, _ = io.WriteString(w, `{
				"retCode":0,
				"retMsg":"OK",
				"result":{
					"list":[
						{
							"totalEquity":"1000",
							"totalAvailableBalance":"700",
							"totalInitialMargin":"300",
							"totalMarginBalance":"1005",
							"totalWalletBalance":"995"
						}
					]
				},
				"time":1710000006000
			}`)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := NewBybitV5TradeAdapter("bybit", ExchangeConfig{
		Enabled:     true,
		RestBaseURL: server.URL,
		Auth: AuthConfig{
			APIKeyEnv:    "BYBIT_API_KEY",
			APISecretEnv: "BYBIT_API_SECRET",
		},
	}, logger).(*BybitV5TradeClient)

	status, err := client.GetOrderStatus(context.Background(), OrderLookupRequest{
		VenueSymbol:   "BTCUSDT",
		ClientOrderID: "cli-2",
	})
	if err != nil {
		t.Fatalf("GetOrderStatus error = %v", err)
	}
	if status.Status != "PARTIALLY_FILLED" || status.ExecutedQty != 0.005 || status.AveragePrice != 60010 {
		t.Fatalf("unexpected order status %#v", status)
	}

	position, err := client.GetPosition(context.Background(), "BTC", "BTCUSDT", "")
	if err != nil {
		t.Fatalf("GetPosition error = %v", err)
	}
	if position.Quantity != -0.02 || position.EntryPrice != 60100 || position.MarkPrice != 60050 {
		t.Fatalf("unexpected position %#v", position)
	}

	account, err := client.GetAccountSnapshot(context.Background())
	if err != nil {
		t.Fatalf("GetAccountSnapshot error = %v", err)
	}
	if account.Equity != 1000 || account.AvailableBalance != 700 || account.MarginUsed != 300 {
		t.Fatalf("unexpected account snapshot %#v", account)
	}
}

func TestBybitV5TradeCancelOrder_UsesOrderLinkID(t *testing.T) {
	t.Setenv("BYBIT_API_KEY", "key-cancel")
	t.Setenv("BYBIT_API_SECRET", "secret-cancel")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v5/order/cancel" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body error = %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatalf("unmarshal body error = %v", err)
		}
		if payload["orderLinkId"] != "cli-cancel" {
			t.Fatalf("expected orderLinkId=cli-cancel, got %#v", payload["orderLinkId"])
		}
		if payload["symbol"] != "BTCUSDT" {
			t.Fatalf("expected symbol BTCUSDT, got %#v", payload["symbol"])
		}
		_, _ = io.WriteString(w, `{
			"retCode":0,
			"retMsg":"OK",
			"result":{"orderId":"order-cancel","orderLinkId":"cli-cancel"},
			"time":1710000007000
		}`)
	}))
	defer server.Close()

	client := NewBybitV5TradeAdapter("bybit", ExchangeConfig{
		Enabled:     true,
		RestBaseURL: server.URL,
		Auth: AuthConfig{
			APIKeyEnv:    "BYBIT_API_KEY",
			APISecretEnv: "BYBIT_API_SECRET",
		},
	}, logger).(*BybitV5TradeClient)

	if err := client.CancelOrder(context.Background(), OrderLookupRequest{
		VenueSymbol:   "BTCUSDT",
		ClientOrderID: "cli-cancel",
	}); err != nil {
		t.Fatalf("CancelOrder error = %v", err)
	}
}

func hmacHex(secret, payload string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

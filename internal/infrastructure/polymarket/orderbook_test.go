package polymarket

import "testing"

func TestGenerateOrderBookSummaryHashFixtures(t *testing.T) {
	market := "0xaabbcc"
	assetID := "100"
	timestamp := "123456789"
	minOrderSize := "15"
	tickSize := "0.001"
	negRisk := false

	book := &OrderBookSummary{
		Market:       &market,
		AssetID:      &assetID,
		Timestamp:    &timestamp,
		Bids:         []OrderBookLevel{{Price: "0.3", Size: "100"}, {Price: "0.4", Size: "100"}},
		Asks:         []OrderBookLevel{{Price: "0.6", Size: "100"}, {Price: "0.7", Size: "100"}},
		MinOrderSize: &minOrderSize,
		TickSize:     &tickSize,
		NegRisk:      &negRisk,
		Hash:         "",
	}
	hash, err := GenerateOrderBookSummaryHash(book)
	if err != nil {
		t.Fatalf("GenerateOrderBookSummaryHash returned error: %v", err)
	}
	if hash != "36f56998e26d9a7c553446f35b240481efb271a3" {
		t.Fatalf("unexpected hash: %s", hash)
	}
	if book.Hash != hash {
		t.Fatalf("expected book hash to be updated, got %s", book.Hash)
	}
}

func TestGenerateOrderBookSummaryHashWithSparseFields(t *testing.T) {
	market := "0xaabbcc"
	assetID := "100"
	timestamp := ""
	minOrderSize := "15"
	tickSize := "0.001"
	negRisk := false
	lastTradePrice := "0"

	book := &OrderBookSummary{
		Market:         &market,
		AssetID:        &assetID,
		Timestamp:      &timestamp,
		Bids:           []OrderBookLevel{},
		Asks:           []OrderBookLevel{},
		MinOrderSize:   &minOrderSize,
		TickSize:       &tickSize,
		NegRisk:        &negRisk,
		LastTradePrice: &lastTradePrice,
		Hash:           "",
	}
	hash, err := GenerateOrderBookSummaryHash(book)
	if err != nil {
		t.Fatalf("GenerateOrderBookSummaryHash returned error: %v", err)
	}
	if hash != "671cb98e93a82db4c16d49daaf72ef5b3286b50a" {
		t.Fatalf("unexpected hash: %s", hash)
	}
}

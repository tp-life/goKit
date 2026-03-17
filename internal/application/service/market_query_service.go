package service

import (
	"strings"
)

type MarketQueryService struct {
	store *MarketStore
}

func NewMarketQueryService(store *MarketStore) *MarketQueryService {
	return &MarketQueryService{store: store}
}

func (s *MarketQueryService) Snapshot(symbol string) SymbolMarketState {
	return s.store.Snapshot(strings.ToUpper(strings.TrimSpace(symbol)))
}

func (s *MarketQueryService) Watchlist() []string {
	return s.store.Watchlist()
}

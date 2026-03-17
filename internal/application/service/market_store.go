package service

import (
	"sort"
	"strings"
	"sync"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/infrastructure/exchange"
)

type SymbolMarketState struct {
	Symbol  string                            `json:"symbol"`
	Funding map[string]entity.FundingSnapshot `json:"funding"`
	BookTop map[string]entity.BookTopSnapshot `json:"book_top"`
}

// MarketStore 保存“当前最新”的市场内存快照。
// 这里的数据是机会计算、执行计划、前端查询的统一来源。
type MarketStore struct {
	mu       sync.RWMutex
	symbols  map[string]map[string]entity.Symbol
	funding  map[string]map[string]entity.FundingSnapshot
	bookTop  map[string]map[string]entity.BookTopSnapshot
	statuses map[string]exchange.ConnectorStatus

	// watchlist 表示全市场基础池：
	// 只要一个 canonical symbol 至少在两家交易所同时存在，就会进入这里。
	watchlist []string
	// deepScanWatchlist 表示当前“真正做盘口深扫”的 symbol 集合：
	// 只对这批币维护高频 best bid/ask，并进入精算机会阶段。
	deepScanWatchlist []string
}

func NewMarketStore() *MarketStore {
	return &MarketStore{
		symbols:           make(map[string]map[string]entity.Symbol),
		funding:           make(map[string]map[string]entity.FundingSnapshot),
		bookTop:           make(map[string]map[string]entity.BookTopSnapshot),
		statuses:          make(map[string]exchange.ConnectorStatus),
		watchlist:         []string{},
		deepScanWatchlist: []string{},
	}
}

func (s *MarketStore) SetWatchlist(items []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]string(nil), items...)
	sort.Strings(cp)
	s.watchlist = cp
}

func (s *MarketStore) Watchlist() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.watchlist...)
}

// SetDeepScanWatchlist 更新当前“盘口深扫池”。
// 这份列表通常由策略层动态计算：核心币 + funding 粗筛候选 + 冷门轮转池。
func (s *MarketStore) SetDeepScanWatchlist(items []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := append([]string(nil), items...)
	sort.Strings(cp)
	s.deepScanWatchlist = cp
}

func (s *MarketStore) DeepScanWatchlist() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.deepScanWatchlist...)
}

func (s *MarketStore) UpsertSymbol(symbol entity.Symbol) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.ToUpper(symbol.Symbol)
	if s.symbols[key] == nil {
		s.symbols[key] = make(map[string]entity.Symbol)
	}
	s.symbols[key][strings.ToLower(symbol.Exchange)] = symbol
}

func (s *MarketStore) SymbolsByCanonical(symbol string) map[string]entity.Symbol {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metaSet := s.symbols[strings.ToUpper(symbol)]
	out := make(map[string]entity.Symbol, len(metaSet))
	for k, v := range metaSet {
		out[k] = v
	}
	return out
}

func (s *MarketStore) ExchangesForSymbol(symbol string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metaSet := s.symbols[strings.ToUpper(symbol)]
	out := make([]string, 0, len(metaSet))
	for ex := range metaSet {
		out = append(out, ex)
	}
	sort.Strings(out)
	return out
}

func (s *MarketStore) Symbol(exchangeName, symbol string) (entity.Symbol, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	metaSet := s.symbols[strings.ToUpper(symbol)]
	if metaSet == nil {
		return entity.Symbol{}, false
	}
	item, ok := metaSet[strings.ToLower(exchangeName)]
	return item, ok
}

func (s *MarketStore) UpsertFunding(item entity.FundingSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := strings.ToLower(item.Exchange)
	if s.funding[ex] == nil {
		s.funding[ex] = make(map[string]entity.FundingSnapshot)
	}
	if metaSet, ok := s.symbols[item.Symbol]; ok {
		if meta, ok := metaSet[ex]; ok {
			if meta.FundingIntervalHours > 0 {
				item.FundingIntervalHours = meta.FundingIntervalHours
			}
			if item.VenueSymbol == "" {
				item.VenueSymbol = meta.VenueSymbol
			}
		}
	}
	s.funding[ex][item.Symbol] = item
	status := s.statuses[ex]
	status.Exchange = ex
	status.MarkPriceConnected = true
	status.LastMarketEventAt = time.Now().UTC()
	status.LastError = ""
	s.statuses[ex] = status
}

func (s *MarketStore) UpsertBookTop(item entity.BookTopSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := strings.ToLower(item.Exchange)
	if s.bookTop[ex] == nil {
		s.bookTop[ex] = make(map[string]entity.BookTopSnapshot)
	}
	if metaSet, ok := s.symbols[item.Symbol]; ok {
		if meta, ok := metaSet[ex]; ok && item.VenueSymbol == "" {
			item.VenueSymbol = meta.VenueSymbol
		}
	}
	s.bookTop[ex][item.Symbol] = item
	status := s.statuses[ex]
	status.Exchange = ex
	status.BookTickerConnected = true
	status.LastBookEventAt = time.Now().UTC()
	status.LastError = ""
	s.statuses[ex] = status
}

func (s *MarketStore) UpdateStatus(status exchange.ConnectorStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ex := strings.ToLower(status.Exchange)
	cur := s.statuses[ex]
	if status.Exchange != "" {
		cur.Exchange = ex
	}
	if status.LastError != "" {
		cur.LastError = status.LastError
	}
	if status.MarkPriceConnected {
		cur.MarkPriceConnected = true
	}
	if status.BookTickerConnected {
		cur.BookTickerConnected = true
	}
	if !status.LastMarketEventAt.IsZero() {
		cur.LastMarketEventAt = status.LastMarketEventAt
	}
	if !status.LastBookEventAt.IsZero() {
		cur.LastBookEventAt = status.LastBookEventAt
	}
	s.statuses[ex] = cur
}

func (s *MarketStore) LatestFunding(exchangeName, symbol string) (entity.FundingSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.funding[strings.ToLower(exchangeName)]
	if m == nil {
		return entity.FundingSnapshot{}, false
	}
	v, ok := m[strings.ToUpper(symbol)]
	return v, ok
}

func (s *MarketStore) LatestBookTop(exchangeName, symbol string) (entity.BookTopSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m := s.bookTop[strings.ToLower(exchangeName)]
	if m == nil {
		return entity.BookTopSnapshot{}, false
	}
	v, ok := m[strings.ToUpper(symbol)]
	return v, ok
}

func (s *MarketStore) Snapshot(symbol string) SymbolMarketState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	symbol = strings.ToUpper(symbol)
	out := SymbolMarketState{Symbol: symbol, Funding: map[string]entity.FundingSnapshot{}, BookTop: map[string]entity.BookTopSnapshot{}}
	for ex, m := range s.funding {
		if item, ok := m[symbol]; ok {
			out.Funding[ex] = item
		}
	}
	for ex, m := range s.bookTop {
		if item, ok := m[symbol]; ok {
			out.BookTop[ex] = item
		}
	}
	return out
}

func (s *MarketStore) FundingSnapshots(symbols []string) []entity.FundingSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	allow := make(map[string]struct{}, len(symbols))
	for _, sym := range symbols {
		allow[strings.ToUpper(sym)] = struct{}{}
	}
	var out []entity.FundingSnapshot
	for _, m := range s.funding {
		for sym, item := range m {
			if len(allow) == 0 {
				out = append(out, item)
				continue
			}
			if _, ok := allow[sym]; ok {
				out = append(out, item)
			}
		}
	}
	return out
}

func (s *MarketStore) BookTopSnapshots(symbols []string) []entity.BookTopSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	allow := make(map[string]struct{}, len(symbols))
	for _, sym := range symbols {
		allow[strings.ToUpper(sym)] = struct{}{}
	}
	var out []entity.BookTopSnapshot
	for _, m := range s.bookTop {
		for sym, item := range m {
			if len(allow) == 0 {
				out = append(out, item)
				continue
			}
			if _, ok := allow[sym]; ok {
				out = append(out, item)
			}
		}
	}
	return out
}

func (s *MarketStore) Statuses() []exchange.ConnectorStatus {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]exchange.ConnectorStatus, 0, len(s.statuses))
	for _, item := range s.statuses {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Exchange < out[j].Exchange })
	return out
}

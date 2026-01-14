package exchange

import (
	"sync"
)

// SymbolMapper 符号映射器，处理不同交易所的币对差异
type SymbolMapper struct {
	// 统一符号 -> Binance 符号
	binanceMap map[string]string
	// 统一符号 -> Lighter Market ID
	lighterMap map[string]int
	// 统一符号 -> Hyperliquid 符号
	hyperliquidMap map[string]string
	// 反向映射
	binanceReverse     map[string]string
	lighterReverse     map[int]string
	hyperliquidReverse map[string]string
	mu                 sync.RWMutex
}

func NewSymbolMapper() *SymbolMapper {
	return &SymbolMapper{
		binanceMap:         make(map[string]string),
		lighterMap:          make(map[string]int),
		hyperliquidMap:      make(map[string]string),
		binanceReverse:     make(map[string]string),
		lighterReverse:      make(map[int]string),
		hyperliquidReverse:  make(map[string]string),
	}
}

// RegisterSymbol 注册符号映射
func (m *SymbolMapper) RegisterSymbol(
	unifiedSymbol string,
	binanceSymbol string,
	lighterMarketID int,
	hyperliquidSymbol string,
) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.binanceMap[unifiedSymbol] = binanceSymbol
	m.lighterMap[unifiedSymbol] = lighterMarketID
	m.hyperliquidMap[unifiedSymbol] = hyperliquidSymbol

	// 反向映射
	m.binanceReverse[binanceSymbol] = unifiedSymbol
	m.lighterReverse[lighterMarketID] = unifiedSymbol
	m.hyperliquidReverse[hyperliquidSymbol] = unifiedSymbol
}

// ToBinance 统一符号转 Binance 符号
func (m *SymbolMapper) ToBinance(unifiedSymbol string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	symbol, ok := m.binanceMap[unifiedSymbol]
	return symbol, ok
}

// ToLighter 统一符号转 Lighter Market ID
func (m *SymbolMapper) ToLighter(unifiedSymbol string) (int, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	marketID, ok := m.lighterMap[unifiedSymbol]
	return marketID, ok
}

// ToHyperliquid 统一符号转 Hyperliquid 符号
func (m *SymbolMapper) ToHyperliquid(unifiedSymbol string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	symbol, ok := m.hyperliquidMap[unifiedSymbol]
	return symbol, ok
}

// FromBinance Binance 符号转统一符号
func (m *SymbolMapper) FromBinance(binanceSymbol string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	symbol, ok := m.binanceReverse[binanceSymbol]
	return symbol, ok
}

// FromLighter Lighter Market ID 转统一符号
func (m *SymbolMapper) FromLighter(marketID int) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	symbol, ok := m.lighterReverse[marketID]
	return symbol, ok
}

// FromHyperliquid Hyperliquid 符号转统一符号
func (m *SymbolMapper) FromHyperliquid(hyperliquidSymbol string) (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	symbol, ok := m.hyperliquidReverse[hyperliquidSymbol]
	return symbol, ok
}

// GetAllUnifiedSymbols 获取所有统一符号
func (m *SymbolMapper) GetAllUnifiedSymbols() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	symbols := make([]string, 0, len(m.binanceMap))
	for symbol := range m.binanceMap {
		symbols = append(symbols, symbol)
	}
	return symbols
}

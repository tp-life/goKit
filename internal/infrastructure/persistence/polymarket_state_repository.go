package persistence

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	infraPolymarket "goKit/internal/infrastructure/polymarket"
)

// PolymarketStateRepository 是基于本地 JSON 文件的状态仓储实现。
type PolymarketStateRepository struct {
	path string
	mu   sync.Mutex
}

// NewPolymarketStateRepository 创建 Polymarket 文件状态仓储。
func NewPolymarketStateRepository(cfg infraPolymarket.Config) repository.PolymarketStateRepository {
	return &PolymarketStateRepository{
		path: cfg.StateFile,
	}
}

// Load 从状态文件加载上一次保存的 Polymarket 状态。
func (r *PolymarketStateRepository) Load(_ context.Context) (*entity.PolymarketState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := &entity.PolymarketState{}
	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return state, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return state, nil
	}
	if err := json.Unmarshal(data, state); err != nil {
		return &entity.PolymarketState{}, nil
	}
	return state, nil
}

// Save 把当前 Polymarket 状态原子地写回本地文件。
func (r *PolymarketStateRepository) Save(_ context.Context, state *entity.PolymarketState) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, data, 0o644)
}

package repository

import (
	"context"

	"goKit/internal/domain/entity"
)

type SymbolRepository interface {
	UpsertBatch(ctx context.Context, symbols []entity.Symbol) error
	List(ctx context.Context) ([]entity.Symbol, error)
	ListWatched(ctx context.Context) ([]entity.Symbol, error)
}

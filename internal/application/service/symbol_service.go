package service

import (
	"context"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

type SymbolService struct {
	repo repository.SymbolRepository
}

func NewSymbolService(repo repository.SymbolRepository) *SymbolService {
	return &SymbolService{repo: repo}
}

func (s *SymbolService) List(ctx context.Context) ([]entity.Symbol, error) {
	return s.repo.List(ctx)
}

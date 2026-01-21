package service

import (
	"context"
	"fmt"
	"strings"

	"goKit/internal/domain/repository"
)

type SearchService struct {
	searchRepo repository.SearchRepository
}

func NewSearchService(searchRepo repository.SearchRepository) *SearchService {
	return &SearchService{
		searchRepo: searchRepo,
	}
}

// SearchResultItem 搜索结果项
type SearchResultItem struct {
	ID          uint64 `json:"id"`
	Type        string `json:"type"`         // "memo" 或 "page"
	Title       string `json:"title"`        // Page 的标题，Memo 则为空
	Content     string `json:"content"`      // Memo 的内容，Page 则为空
	HitFragment string `json:"hit_fragment"` // Page 的命中片段
	Summary     string `json:"summary"`      // Page 的摘要
	Cover       string `json:"cover"`        // Page 的封面
}

// SearchResponse 搜索响应
type SearchResponse struct {
	Results []SearchResultItem `json:"results"`
	Total   int                `json:"total"`
	Query   string             `json:"query"`
}

// Search 执行搜索
func (s *SearchService) Search(ctx context.Context, userID uint64, query string, limit, offset int) (*SearchResponse, error) {
	if strings.TrimSpace(query) == "" {
		return &SearchResponse{
			Results: []SearchResultItem{},
			Total:   0,
			Query:   query,
		}, nil
	}

	// 搜索 Memos
	memos, err := s.searchRepo.SearchMemos(ctx, userID, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search memos: %w", err)
	}

	// 搜索 Pages
	pages, err := s.searchRepo.SearchPages(ctx, userID, query, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("search pages: %w", err)
	}

	// 合并结果
	results := make([]SearchResultItem, 0, len(memos)+len(pages))

	// 添加 Memos
	for _, memo := range memos {
		results = append(results, SearchResultItem{
			ID:      memo.ID,
			Type:    "memo",
			Content: memo.Content,
		})
	}

	// 添加 Pages
	for _, pageResult := range pages {

		results = append(results, SearchResultItem{
			ID:          pageResult.Page.ID,
			Type:        "page",
			Title:       pageResult.Page.Title,
			Summary:     pageResult.Page.Summary,
			Cover:       pageResult.Page.Cover,
			HitFragment: pageResult.HitFragment,
		})
	}

	return &SearchResponse{
		Results: results,
		Total:   len(results),
		Query:   query,
	}, nil
}

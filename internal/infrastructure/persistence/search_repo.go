package persistence

import (
	"context"
	"fmt"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
	"strings"
	"time"
)

type SearchRepo struct {
	client *db.Client
}

func NewSearchRepo(client *db.Client) repository.SearchRepository {
	return &SearchRepo{client: client}
}

func (r *SearchRepo) SearchMemos(ctx context.Context, userID uint64, query string, limit, offset int) ([]*entity.Memo, error) {
	var memos []*entity.Memo
	
	// 使用 MATCH...AGAINST 进行全文检索
	// 使用 BOOLEAN MODE 支持更灵活的搜索
	searchQuery := fmt.Sprintf("+%s*", strings.TrimSpace(query))
	
	err := r.client.GetDB(ctx).
		Where("user_id = ? AND deleted_at IS NULL", userID).
		Where("MATCH(content) AGAINST(? IN BOOLEAN MODE)", searchQuery).
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&memos).Error
	
	if err != nil {
		// 如果是 FULLTEXT 索引错误，提供更友好的错误信息
		if strings.Contains(err.Error(), "Can't find FULLTEXT index") {
			return nil, fmt.Errorf("全文索引未创建，请执行迁移脚本: %w", err)
		}
		return nil, fmt.Errorf("search memos: %w", err)
	}
	
	return memos, nil
}

func (r *SearchRepo) SearchPages(ctx context.Context, userID uint64, query string, limit, offset int) ([]*repository.SearchPageResult, error) {
	var results []*repository.SearchPageResult
	
	// 使用 MATCH...AGAINST 搜索 Blocks，然后关联 Pages
	// 使用 BOOLEAN MODE 支持更灵活的搜索
	searchQuery := fmt.Sprintf("+%s*", strings.TrimSpace(query))
	
	// 查询命中的 Blocks 及其所属的 Pages
	type SearchResult struct {
		PageID      uint64
		UserID      uint64
		Title       string
		Cover       string
		Summary     string
		IsShared    bool
		ShareID     *string
		CreatedAt   string
		UpdatedAt   string
		HitFragment string
	}
	
	var searchResults []SearchResult
	
	// 使用 Raw SQL 进行全文检索，并通过 JOIN 关联 Pages
	// 使用子查询去重，防止一篇文章命中多次
	// 取第一个匹配的 block 的 search_content 作为 hit_fragment
	sql := `
		SELECT 
			p.id as page_id,
			p.user_id,
			p.title,
			p.cover,
			p.summary,
			p.is_shared,
			p.share_id,
			p.created_at,
			p.updated_at,
			COALESCE(SUBSTRING(MAX(b.search_content), 1, 200), '') as hit_fragment
		FROM blocks b
		INNER JOIN pages p ON b.page_id = p.id
		WHERE MATCH(b.search_content) AGAINST(? IN BOOLEAN MODE)
			AND b.deleted_at IS NULL
			AND p.deleted_at IS NULL
			AND p.user_id = ?
		GROUP BY p.id, p.user_id, p.title, p.cover, p.summary, p.is_shared, p.share_id, p.created_at, p.updated_at
		ORDER BY p.updated_at DESC
		LIMIT ? OFFSET ?
	`
	
	err := r.client.GetDB(ctx).Raw(sql, searchQuery, userID, limit, offset).Scan(&searchResults).Error
	if err != nil {
		// 如果是 FULLTEXT 索引错误，提供更友好的错误信息
		if strings.Contains(err.Error(), "Can't find FULLTEXT index") {
			return nil, fmt.Errorf("全文索引未创建，请执行迁移脚本: %w", err)
		}
		return nil, fmt.Errorf("search pages: %w", err)
	}
	
	// 转换为 SearchPageResult
	for _, sr := range searchResults {
		shareID := sr.ShareID
		
		// 解析时间字段
		createdAt, _ := time.Parse("2006-01-02 15:04:05.000", sr.CreatedAt)
		updatedAt, _ := time.Parse("2006-01-02 15:04:05.000", sr.UpdatedAt)
		
		results = append(results, &repository.SearchPageResult{
			Page: &entity.Page{
				ID:        sr.PageID,
				UserID:    sr.UserID,
				Title:     sr.Title,
				Cover:     sr.Cover,
				Summary:   sr.Summary,
				IsShared:  sr.IsShared,
				ShareID:   shareID,
				CreatedAt: createdAt,
				UpdatedAt: updatedAt,
			},
			HitFragment: sr.HitFragment,
		})
	}
	
	return results, nil
}

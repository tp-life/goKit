package service

import (
	"context"
	"fmt"
	"time"

	"goKit/internal/domain/repository"
)

type TagService struct {
	tagRepo   repository.TagRepository
	memoRepo  repository.MemoRepository
	pageRepo  repository.PageRepository
	blockRepo repository.BlockRepository
}

func NewTagService(
	tagRepo repository.TagRepository,
	memoRepo repository.MemoRepository,
	pageRepo repository.PageRepository,
	blockRepo repository.BlockRepository,
) *TagService {
	return &TagService{
		tagRepo:   tagRepo,
		memoRepo:  memoRepo,
		pageRepo:  pageRepo,
		blockRepo: blockRepo,
	}
}

// GetTagsResponse 获取标签列表响应
type GetTagsResponse struct {
	Tags []TagItem `json:"tags"`
}

// TagItem 标签项
type TagItem struct {
	ID        uint64 `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// GetTags 获取用户的所有标签
func (s *TagService) GetTags(ctx context.Context, userID uint64) (*GetTagsResponse, error) {
	tags, err := s.tagRepo.FindByUserID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("find tags: %w", err)
	}

	items := make([]TagItem, len(tags))
	for i, tag := range tags {
		items[i] = TagItem{
			ID:        tag.ID,
			Name:      tag.Name,
			CreatedAt: tag.CreatedAt.Format(time.RFC3339),
		}
	}

	return &GetTagsResponse{
		Tags: items,
	}, nil
}

// TagTimelineItem 标签时间轴项目
type TagTimelineItem struct {
	ID        uint64   `json:"id"`
	Type      string   `json:"type"` // "memo" 或 "page"
	Title     string   `json:"title,omitempty"`
	Content   string   `json:"content,omitempty"`
	Summary   string   `json:"summary,omitempty"`
	Cover     string   `json:"cover,omitempty"`
	Images    []string `json:"images,omitempty"`
	IsShared  bool     `json:"is_shared,omitempty"`
	ShareID   string   `json:"share_id,omitempty"`
	CreatedAt string   `json:"created_at"`
	UpdatedAt string   `json:"updated_at,omitempty"`
}

// GetTagTimelineResponse 标签时间轴响应
type GetTagTimelineResponse struct {
	Items []TagTimelineItem `json:"items"`
	Total int               `json:"total"`
}

// GetTagTimeline 获取标签聚合时间轴
func (s *TagService) GetTagTimeline(ctx context.Context, userID uint64, tagName string, limit, offset int) (*GetTagTimelineResponse, error) {
	// 查找标签
	tag, err := s.tagRepo.FindByName(ctx, userID, tagName)
	if err != nil {
		return nil, fmt.Errorf("find tag: %w", err)
	}
	if tag == nil {
		return &GetTagTimelineResponse{
			Items: []TagTimelineItem{},
			Total: 0,
		}, nil
	}

	// 查询 Memos 和 Pages
	memos, err := s.tagRepo.FindMemosByTagID(ctx, tag.ID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("find memos: %w", err)
	}

	pages, err := s.tagRepo.FindPagesByTagID(ctx, tag.ID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("find pages: %w", err)
	}

	// 合并并排序
	items := make([]TagTimelineItem, 0, len(memos)+len(pages))

	// 添加 Memos
	for _, memo := range memos {
		items = append(items, TagTimelineItem{
			ID:        memo.ID,
			Type:      "memo",
			Content:   memo.Content,
			Images:    []string(memo.Images),
			CreatedAt: memo.CreatedAt.Format(time.RFC3339),
		})
	}

	// 添加 Pages
	for _, page := range pages {
		shareID := ""
		if page.ShareID != nil {
			shareID = *page.ShareID
		}

		items = append(items, TagTimelineItem{
			ID:        page.ID,
			Type:      "page",
			Title:     page.Title,
			Summary:   page.Summary,
			Cover:     page.Cover,
			IsShared:  page.IsShared,
			ShareID:   shareID,
			CreatedAt: page.CreatedAt.Format(time.RFC3339),
			UpdatedAt: page.UpdatedAt.Format(time.RFC3339),
		})
	}

	return &GetTagTimelineResponse{
		Items: items,
		Total: len(items),
	}, nil
}

// DeleteTagResponse 删除标签响应
type DeleteTagResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// DeleteTag 删除标签
func (s *TagService) DeleteTag(ctx context.Context, userID uint64, tagID uint64) (*DeleteTagResponse, error) {
	// 检查标签是否存在且属于该用户
	tag, err := s.tagRepo.FindByID(ctx, tagID)
	if err != nil {
		return nil, fmt.Errorf("find tag: %w", err)
	}
	if tag == nil {
		return nil, fmt.Errorf("tag not found")
	}
	if tag.UserID != userID {
		return nil, fmt.Errorf("permission denied")
	}

	// 删除标签（关联表中的记录会自动删除）
	if err := s.tagRepo.Delete(ctx, tagID); err != nil {
		return nil, fmt.Errorf("delete tag: %w", err)
	}

	return &DeleteTagResponse{
		Success: true,
		Message: "Tag deleted successfully",
	}, nil
}

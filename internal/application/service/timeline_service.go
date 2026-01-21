package service

import (
	"context"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
)

type TimelineService struct {
	memoRepo  repository.MemoRepository
	pageRepo  repository.PageRepository
	blockRepo repository.BlockRepository
}

func NewTimelineService(
	memoRepo repository.MemoRepository,
	pageRepo repository.PageRepository,
	blockRepo repository.BlockRepository,
) *TimelineService {
	return &TimelineService{
		memoRepo:  memoRepo,
		pageRepo:  pageRepo,
		blockRepo: blockRepo,
	}
}

// FeedItem 时间轴条目
type FeedItem struct {
	Type      string    `json:"type"` // "memo" 或 "page"
	ID        uint64    `json:"id"`
	Content   string    `json:"content,omitempty"`   // Memo 内容
	Title     string    `json:"title,omitempty"`     // Page 标题
	Summary   string    `json:"summary,omitempty"`   // Page 摘要
	Cover     string    `json:"cover,omitempty"`     // Page 封面
	Images    []string  `json:"images,omitempty"`    // Memo 图片或 Page 中的图片
	Tags      []string  `json:"tags,omitempty"`      // 标签列表
	IsShared  bool      `json:"is_shared,omitempty"` // Page 是否分享
	ShareID   string    `json:"share_id,omitempty"`  // Page 分享ID
	CreatedAt time.Time `json:"created_at"`
}

// GetTimelineRequest 获取时间轴请求
type GetTimelineRequest struct {
	Limit  int `json:"limit"`  // 默认 20
	Offset int `json:"offset"` // 默认 0
}

// GetTimelineResponse 获取时间轴响应
type GetTimelineResponse struct {
	Items []FeedItem `json:"items"`
	Total int        `json:"total"`
}

// GetTimeline 获取统一时间轴（聚合 Memos 和 Pages）
// userID 为 0 时，只返回公开内容（访客模式）
func (s *TimelineService) GetTimeline(ctx context.Context, userID uint64, req GetTimelineRequest) (*GetTimelineResponse, error) {
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	// 并发查询 Memos 和 Pages（使用 Goroutine + Channel 实现并发）
	type memoResult struct {
		memos []*MemoItem
		err   error
	}
	type pageResult struct {
		pages []*PageItem
		err   error
	}

	memoChan := make(chan memoResult, 1)
	pageChan := make(chan pageResult, 1)

	// 并发查询 Memos
	go func() {
		var memoList []*entity.Memo
		var err error

		if userID > 0 {
			// 登录用户：查询自己的 Memos
			memoList, err = s.memoRepo.FindByUserID(ctx, userID, req.Limit*2, req.Offset)
		} else {
			// 访客模式：不返回 Memos（Memos 都是私有的）
			memoList = []*entity.Memo{}
			err = nil
		}

		if err != nil {
			memoChan <- memoResult{err: err}
			return
		}
		memos := make([]*MemoItem, len(memoList))
		for i, m := range memoList {
			tags := make([]string, len(m.Tags))
			for j, tag := range m.Tags {
				tags[j] = tag.Name
			}
			memos[i] = &MemoItem{
				ID:        m.ID,
				Content:   m.Content,
				Images:    []string(m.Images),
				Tags:      tags,
				CreatedAt: m.CreatedAt,
			}
		}
		memoChan <- memoResult{memos: memos}
	}()

	// 并发查询 Pages
	go func() {
		var pageList []*entity.Page
		var err error

		if userID > 0 {
			// 登录用户：查询自己的 Pages
			pageList, err = s.pageRepo.FindByUserID(ctx, userID, req.Limit*2, req.Offset)
		} else {
			// 访客模式：只查询公开的 Pages
			pageList, err = s.pageRepo.FindPublicPages(ctx, req.Limit*2, req.Offset)
		}

		if err != nil {
			pageChan <- pageResult{err: err}
			return
		}
		pages := make([]*PageItem, len(pageList))
		for i, p := range pageList {
			// 提取图片 URL（从 blocks 中）
			imageURLs := s.extractPageImages(ctx, p.ID)

			tags := make([]string, len(p.Tags))
			for j, tag := range p.Tags {
				tags[j] = tag.Name
			}
			pages[i] = &PageItem{
				ID:        p.ID,
				Title:     p.Title,
				Summary:   p.Summary,
				Cover:     p.Cover,
				Images:    imageURLs, // 提取的图片 URL 列表
				Tags:      tags,
				IsShared:  p.IsShared,
				ShareID:   p.ShareID,
				CreatedAt: p.CreatedAt,
			}
		}
		pageChan <- pageResult{pages: pages}
	}()

	// 等待两个查询完成
	memoRes := <-memoChan
	pageRes := <-pageChan

	if memoRes.err != nil {
		return nil, memoRes.err
	}
	if pageRes.err != nil {
		return nil, pageRes.err
	}

	// 合并并排序（Merge Sort 思想：两个已排序数组合并）
	items := make([]FeedItem, 0, len(memoRes.memos)+len(pageRes.pages))

	// 使用双指针合并两个已按时间倒序排列的数组
	memoIdx, pageIdx := 0, 0
	for memoIdx < len(memoRes.memos) && pageIdx < len(pageRes.pages) {
		if memoRes.memos[memoIdx].CreatedAt.After(pageRes.pages[pageIdx].CreatedAt) {
			items = append(items, FeedItem{
				Type:      "memo",
				ID:        memoRes.memos[memoIdx].ID,
				Content:   memoRes.memos[memoIdx].Content,
				Images:    memoRes.memos[memoIdx].Images,
				Tags:      memoRes.memos[memoIdx].Tags,
				CreatedAt: memoRes.memos[memoIdx].CreatedAt,
			})
			memoIdx++
		} else {
			p := pageRes.pages[pageIdx]
			shareID := ""
			if p.ShareID != nil {
				shareID = *p.ShareID
			}
			items = append(items, FeedItem{
				Type:      "page",
				ID:        p.ID,
				Title:     p.Title,
				Summary:   p.Summary,
				Cover:     p.Cover,
				Images:    p.Images, // 图片 URL 列表
				Tags:      p.Tags,
				IsShared:  p.IsShared,
				ShareID:   shareID,
				CreatedAt: p.CreatedAt,
			})
			pageIdx++
		}
	}

	// 追加剩余元素
	for memoIdx < len(memoRes.memos) {
		items = append(items, FeedItem{
			Type:      "memo",
			ID:        memoRes.memos[memoIdx].ID,
			Content:   memoRes.memos[memoIdx].Content,
			Images:    memoRes.memos[memoIdx].Images,
			Tags:      memoRes.memos[memoIdx].Tags,
			CreatedAt: memoRes.memos[memoIdx].CreatedAt,
		})
		memoIdx++
	}
	for pageIdx < len(pageRes.pages) {
		p := pageRes.pages[pageIdx]
		shareID := ""
		if p.ShareID != nil {
			shareID = *p.ShareID
		}
		items = append(items, FeedItem{
			Type:      "page",
			ID:        p.ID,
			Title:     p.Title,
			Summary:   p.Summary,
			Cover:     p.Cover,
			Images:    p.Images, // 图片 URL 列表
			Tags:      p.Tags,
			IsShared:  p.IsShared,
			ShareID:   shareID,
			CreatedAt: p.CreatedAt,
		})
		pageIdx++
	}

	// 分页
	total := len(items)
	start := req.Offset
	end := start + req.Limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}

	if start < end {
		items = items[start:end]
	} else {
		items = []FeedItem{}
	}

	return &GetTimelineResponse{
		Items: items,
		Total: total,
	}, nil
}

// 内部辅助结构
type MemoItem struct {
	ID        uint64
	Content   string
	Images    []string
	Tags      []string
	CreatedAt time.Time
}

type PageItem struct {
	ID        uint64
	Title     string
	Summary   string
	Cover     string
	Images    []string // 从 blocks 中提取的图片 URL
	Tags      []string
	IsShared  bool
	ShareID   *string
	CreatedAt time.Time
}

// extractPageImages 从 Page 的 blocks 中提取图片 URL（最多4张）
func (s *TimelineService) extractPageImages(ctx context.Context, pageID uint64) []string {
	blocks, err := s.blockRepo.FindByPageID(ctx, pageID)
	if err != nil {
		return []string{}
	}

	images := make([]string, 0, 4)
	for _, block := range blocks {
		if block.Type == "image" && block.Data != nil {
			// Editor.js image 块的数据结构
			if fileData, ok := block.Data["file"].(map[string]interface{}); ok {
				if url, ok := fileData["url"].(string); ok && url != "" {
					images = append(images, url)
					if len(images) >= 4 {
						break // 最多提取4张图片
					}
				}
			} else if url, ok := block.Data["url"].(string); ok && url != "" {
				// 兼容其他可能的格式
				images = append(images, url)
				if len(images) >= 4 {
					break
				}
			}
		}
	}

	return images
}

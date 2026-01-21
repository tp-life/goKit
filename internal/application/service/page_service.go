package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type PageService struct {
	pageRepo       repository.PageRepository
	blockRepo      repository.BlockRepository
	tagRepo        repository.TagRepository
	storageService repository.StorageService
	tx             *db.Client
}

func NewPageService(
	pageRepo repository.PageRepository,
	blockRepo repository.BlockRepository,
	tagRepo repository.TagRepository,
	storageService repository.StorageService,
	tx *db.Client,
) *PageService {
	return &PageService{
		pageRepo:       pageRepo,
		blockRepo:      blockRepo,
		tagRepo:        tagRepo,
		storageService: storageService,
		tx:             tx,
	}
}

// EditorJSBlock Editor.js 块结构
type EditorJSBlock struct {
	ID   string         `json:"id"`
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// EditorJSData Editor.js 完整数据结构
type EditorJSData struct {
	Time    int64           `json:"time"`
	Blocks  []EditorJSBlock `json:"blocks"`
	Version string          `json:"version"`
}

// CreatePageRequest 创建/更新页面请求
type CreatePageRequest struct {
	ID     uint64       `json:"id,omitempty"` // 如果提供，则为更新
	Title  string       `json:"title"`
	Cover  string       `json:"cover"`
	Tags   []string     `json:"tags,omitempty"` // 标签列表
	Blocks EditorJSData `json:"blocks"`         // Editor.js 格式数据
}

// CreatePageResponse 创建/更新页面响应
type CreatePageResponse struct {
	ID        uint64    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateOrUpdatePage 创建或更新页面
func (s *PageService) CreateOrUpdatePage(ctx context.Context, userID uint64, req CreatePageRequest) (*CreatePageResponse, error) {
	var pageID uint64
	var createdAt, updatedAt time.Time

	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		var page *entity.Page
		var err error

		if req.ID > 0 {
			// 更新模式
			page, err = s.pageRepo.FindByID(ctx, req.ID)
			if err != nil {
				return fmt.Errorf("find page: %w", err)
			}
			if page == nil {
				return fmt.Errorf("page not found")
			}
			if page.UserID != userID {
				return fmt.Errorf("permission denied")
			}

			// 物理删除旧 Blocks（更新时完全替换，所以需要物理删除以避免主键冲突）
			if err := s.blockRepo.PermanentlyDeleteByPageID(ctx, req.ID); err != nil {
				return fmt.Errorf("delete blocks: %w", err)
			}

			page.Title = req.Title
			page.Cover = req.Cover
			page.UpdatedAt = time.Now()
			updatedAt = page.UpdatedAt
			createdAt = page.CreatedAt
		} else {
			// 创建模式
			now := time.Now()
			pageID = uint64(now.UnixNano() / 1000) // 微秒级时间戳
			page = &entity.Page{
				ID:        pageID,
				UserID:    userID,
				Title:     req.Title,
				Cover:     req.Cover,
				IsShared:  false,
				ShareID:   nil, // NULL，不设置分享ID
				CreatedAt: now,
				UpdatedAt: now,
			}
			createdAt = page.CreatedAt
			updatedAt = page.UpdatedAt
		}

		// 提取 Summary（从第一个文本块）
		summary := s.extractSummary(req.Blocks)
		page.Summary = summary

		// 处理标签（批量创建）
		if userID > 0 {
			tags, err := s.processTags(ctx, userID, req.Tags)
			if err != nil {
				return fmt.Errorf("process tags: %w", err)
			}
			page.Tags = tags
		}

		// 保存 Page
		if req.ID > 0 {
			if err := s.pageRepo.Update(ctx, page); err != nil {
				return fmt.Errorf("update page: %w", err)
			}
			// 更新标签关联：先清空旧的，再添加新的
			if userID > 0 {
				if err := s.savePageTags(ctx, page.ID, page.Tags, true); err != nil {
					return err
				}
			}
		} else {
			if err := s.pageRepo.Create(ctx, page); err != nil {
				return fmt.Errorf("create page: %w", err)
			}
			// 创建后需要显式保存标签关联（GORM Create 不会自动保存 many-to-many 关联）
			if userID > 0 {
				if err := s.savePageTags(ctx, page.ID, page.Tags, false); err != nil {
					return err
				}
			}
		}

		pageID = page.ID

		// 保存 Blocks
		blocks := make([]*entity.Block, 0, len(req.Blocks.Blocks))
		for i, block := range req.Blocks.Blocks {
			entityBlock := &entity.Block{
				ID:        block.ID,
				PageID:    pageID,
				Type:      block.Type,
				Data:      entity.JSONData(block.Data),
				SortOrder: uint(i),
				CreatedAt: time.Now(),
			}
			blocks = append(blocks, entityBlock)
		}

		if len(blocks) > 0 {
			if err := s.blockRepo.CreateBatch(ctx, blocks); err != nil {
				return fmt.Errorf("create blocks: %w", err)
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &CreatePageResponse{
		ID:        pageID,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}, nil
}

// cleanBlockData 清理 Block 数据，确保数据是纯 JSON 可序列化的
// 通过 JSON 序列化/反序列化来移除不可序列化的对象（如函数、循环引用等）
func cleanBlockData(data map[string]any) map[string]any {
	if data == nil {
		return make(map[string]any)
	}

	// 通过 JSON 序列化/反序列化来清理数据
	dataBytes, err := json.Marshal(data)
	if err != nil {
		// 如果序列化失败，返回空对象
		return make(map[string]any)
	}

	var cleanData map[string]any
	if err := json.Unmarshal(dataBytes, &cleanData); err != nil {
		// 如果反序列化失败，返回空对象
		return make(map[string]any)
	}

	return cleanData
}

// GetPageResponse 获取页面响应
type GetPageResponse struct {
	ID        uint64          `json:"id"`
	UserID    uint64          `json:"user_id"`
	Title     string          `json:"title"`
	Cover     string          `json:"cover"`
	Summary   string          `json:"summary"`
	IsShared  bool            `json:"is_shared"`
	ShareID   string          `json:"share_id"` // 前端显示用，nil 时返回空字符串
	Tags      []string        `json:"tags"`     // 标签列表
	Blocks    []EditorJSBlock `json:"blocks"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// GetPage 获取页面详情
func (s *PageService) GetPage(ctx context.Context, pageID uint64) (*GetPageResponse, error) {
	page, err := s.pageRepo.FindByID(ctx, pageID)
	if err != nil {
		return nil, fmt.Errorf("find page: %w", err)
	}
	if page == nil {
		return nil, fmt.Errorf("page not found")
	}

	blocks, err := s.blockRepo.FindByPageID(ctx, pageID)
	if err != nil {
		return nil, fmt.Errorf("find blocks: %w", err)
	}

	editorBlocks := s.convertBlocksToEditorJS(blocks)
	shareID := s.getShareIDString(page.ShareID)
	tags := s.convertTagsToStrings(page.Tags)

	return &GetPageResponse{
		ID:        page.ID,
		UserID:    page.UserID,
		Title:     page.Title,
		Cover:     page.Cover,
		Summary:   page.Summary,
		IsShared:  page.IsShared,
		ShareID:   shareID,
		Tags:      tags,
		Blocks:    editorBlocks,
		CreatedAt: page.CreatedAt,
		UpdatedAt: page.UpdatedAt,
	}, nil
}

// GetPageByShareID 通过 ShareID 获取公开页面
func (s *PageService) GetPageByShareID(ctx context.Context, shareID string) (*GetPageResponse, error) {
	page, err := s.pageRepo.FindByShareID(ctx, shareID)
	if err != nil {
		return nil, fmt.Errorf("find page: %w", err)
	}
	if page == nil {
		return nil, fmt.Errorf("page not found")
	}

	if !page.IsShared {
		return nil, fmt.Errorf("page is not shared")
	}

	blocks, err := s.blockRepo.FindByPageID(ctx, page.ID)
	if err != nil {
		return nil, fmt.Errorf("find blocks: %w", err)
	}

	editorBlocks := s.convertBlocksToEditorJS(blocks)
	shareIDStr := s.getShareIDString(page.ShareID)
	tags := s.convertTagsToStrings(page.Tags)

	return &GetPageResponse{
		ID:        page.ID,
		UserID:    page.UserID,
		Title:     page.Title,
		Cover:     page.Cover,
		Summary:   page.Summary,
		IsShared:  page.IsShared,
		ShareID:   shareIDStr,
		Tags:      tags,
		Blocks:    editorBlocks,
		CreatedAt: page.CreatedAt,
		UpdatedAt: page.UpdatedAt,
	}, nil
}

// SharePageRequest 分享页面请求
type SharePageRequest struct {
	Enable bool `json:"enable"`
}

// SharePageResponse 分享页面响应
type SharePageResponse struct {
	ShareID string `json:"share_id"`
	URL     string `json:"url"`
}

// SharePage 开启/关闭页面分享
func (s *PageService) SharePage(ctx context.Context, userID, pageID uint64, enable bool) (*SharePageResponse, error) {
	page, err := s.pageRepo.FindByID(ctx, pageID)
	if err != nil {
		return nil, fmt.Errorf("find page: %w", err)
	}
	if page == nil {
		return nil, fmt.Errorf("page not found")
	}
	if page.UserID != userID {
		return nil, fmt.Errorf("permission denied")
	}

	if enable {
		// 生成 ShareID（使用时间戳 + 随机数生成 UUID 格式字符串）
		if page.ShareID == nil || *page.ShareID == "" {
			shareID := fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().Unix()%10000)
			page.ShareID = &shareID
		}
		page.IsShared = true
	} else {
		page.IsShared = false
		// 关闭分享时，将 share_id 设为 nil
		page.ShareID = nil
	}

	if err := s.pageRepo.Update(ctx, page); err != nil {
		return nil, fmt.Errorf("update page: %w", err)
	}

	url := ""
	shareID := ""
	if enable && page.ShareID != nil {
		shareID = *page.ShareID
		url = fmt.Sprintf("/s/%s", shareID)
	}

	return &SharePageResponse{
		ShareID: shareID,
		URL:     url,
	}, nil
}

// DeletePage 删除 Page（软删除，级联删除 Blocks）
func (s *PageService) DeletePage(ctx context.Context, userID, pageID uint64) error {
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		page, err := s.pageRepo.FindByID(ctx, pageID)
		if err != nil {
			return fmt.Errorf("find page: %w", err)
		}
		if page == nil {
			return fmt.Errorf("page not found")
		}
		// 检查权限
		if page.UserID != userID {
			return fmt.Errorf("permission denied")
		}

		// 软删除 Page（GORM 会自动处理）
		if err := s.pageRepo.Delete(ctx, pageID); err != nil {
			return fmt.Errorf("delete page: %w", err)
		}

		// 级联软删除该 Page 下的所有 Blocks
		if err := s.blockRepo.DeleteByPageID(ctx, pageID); err != nil {
			return fmt.Errorf("delete blocks: %w", err)
		}

		return nil
	})

	return err
}

// savePageTags 保存页面标签关联
// clearFirst: true 表示先清空旧关联再添加（用于更新），false 表示直接添加（用于创建）
func (s *PageService) savePageTags(ctx context.Context, pageID uint64, tags []entity.Tag, clearFirst bool) error {
	if len(tags) == 0 && !clearFirst {
		return nil
	}

	db := s.tx.GetDB(ctx)
	pageModel := &entity.Page{ID: pageID}

	if clearFirst {
		// 先清空旧的标签关联
		if err := db.Model(pageModel).Association("Tags").Clear(); err != nil {
			return fmt.Errorf("clear tags: %w", err)
		}
	}

	// 添加新的标签关联
	if len(tags) > 0 {
		if err := db.Model(pageModel).Association("Tags").Append(tags); err != nil {
			return fmt.Errorf("save tags: %w", err)
		}
	}

	return nil
}

// processTags 处理标签：清理、验证并批量查找或创建
func (s *PageService) processTags(ctx context.Context, userID uint64, tagNames []string) ([]entity.Tag, error) {
	if len(tagNames) == 0 {
		return nil, nil
	}

	// 清理和过滤标签名
	validTagNames := make([]string, 0, len(tagNames))
	for _, tagName := range tagNames {
		tagName = strings.TrimSpace(tagName)
		if tagName != "" {
			validTagNames = append(validTagNames, tagName)
		}
	}

	if len(validTagNames) == 0 {
		return nil, nil
	}

	// 批量查找或创建标签
	tagPtrs, err := s.tagRepo.FindOrCreateBatch(ctx, userID, validTagNames)
	if err != nil {
		return nil, fmt.Errorf("find or create tags: %w", err)
	}

	// 转换为 entity.Tag 切片
	if len(tagPtrs) == 0 {
		return nil, nil
	}

	tags := make([]entity.Tag, 0, len(tagPtrs))
	for _, tagPtr := range tagPtrs {
		if tagPtr != nil && tagPtr.ID > 0 {
			tags = append(tags, *tagPtr)
		}
	}

	return tags, nil
}

// convertBlocksToEditorJS 将 entity.Block 转换为 EditorJSBlock
func (s *PageService) convertBlocksToEditorJS(blocks []*entity.Block) []EditorJSBlock {
	if len(blocks) == 0 {
		return []EditorJSBlock{}
	}

	editorBlocks := make([]EditorJSBlock, 0, len(blocks))
	for _, block := range blocks {
		// 清理数据，确保数据是纯 JSON 可序列化的
		cleanData := cleanBlockData(map[string]any(block.Data))

		editorBlocks = append(editorBlocks, EditorJSBlock{
			ID:   block.ID,
			Type: block.Type,
			Data: cleanData,
		})
	}

	return editorBlocks
}

// convertTagsToStrings 将 entity.Tag 切片转换为字符串切片
func (s *PageService) convertTagsToStrings(tags []entity.Tag) []string {
	if len(tags) == 0 {
		return []string{}
	}

	result := make([]string, len(tags))
	for i, tag := range tags {
		result[i] = tag.Name
	}

	return result
}

// getShareIDString 从 *string 获取字符串，nil 时返回空字符串
func (s *PageService) getShareIDString(shareID *string) string {
	if shareID == nil {
		return ""
	}
	return *shareID
}

// extractSummary 从 Editor.js 数据中提取摘要（前100字）
func (s *PageService) extractSummary(data EditorJSData) string {
	for _, block := range data.Blocks {
		if block.Type == "paragraph" {
			if text, ok := block.Data["text"].(string); ok {
				// 提取前100个字符
				summary := strings.TrimSpace(text)
				if len(summary) > 100 {
					summary = summary[:100]
				}
				return summary
			}
		}
	}
	return ""
}

package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"
)

type MemoService struct {
	memoRepo       repository.MemoRepository
	tagRepo        repository.TagRepository
	storageService repository.StorageService
	tx             *db.Client
}

func NewMemoService(
	memoRepo repository.MemoRepository,
	tagRepo repository.TagRepository,
	storageService repository.StorageService,
	tx *db.Client,
) *MemoService {
	return &MemoService{
		memoRepo:       memoRepo,
		tagRepo:        tagRepo,
		storageService: storageService,
		tx:             tx,
	}
}

// CreateMemoRequest 创建 Memo 请求
type CreateMemoRequest struct {
	Content string   `json:"content"`
	Images  []string `json:"images"` // 图片 URL 列表（已上传到七牛）
	Source  string   `json:"source"` // 来源：mobile, web
}

// CreateMemoResponse 创建 Memo 响应
type CreateMemoResponse struct {
	ID        uint64    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

// extractTags 从内容中提取标签（使用正则表达式）
func (s *MemoService) extractTags(content string) []string {
	re := regexp.MustCompile(`#(\S+)`)
	matches := re.FindAllStringSubmatch(content, -1)

	tagMap := make(map[string]bool)
	var tags []string

	for _, match := range matches {
		if len(match) > 1 {
			tagName := strings.TrimSpace(match[1])
			// 去除特殊符号，只保留字母、数字、中文
			tagName = regexp.MustCompile(`[^\p{L}\p{N}]+`).ReplaceAllString(tagName, "")
			if tagName != "" && !tagMap[tagName] {
				tagMap[tagName] = true
				tags = append(tags, tagName)
			}
		}
	}

	return tags
}

// CreateMemo 创建闪念
func (s *MemoService) CreateMemo(ctx context.Context, userID uint64, req CreateMemoRequest) (*CreateMemoResponse, error) {
	var memoID uint64
	var createdAt time.Time

	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		// 生成 ID（简化版：使用时间戳 + 随机数，实际生产环境应使用真正的雪花算法）
		now := time.Now()
		memoID = uint64(now.UnixNano() / 1000) // 微秒级时间戳

		memo := &entity.Memo{
			ID:        memoID,
			UserID:    userID,
			Content:   req.Content,
			Images:    entity.JSONStringArray(req.Images),
			Source:    req.Source,
			CreatedAt: now,
		}

		// 自动提取标签（批量创建）
		if userID > 0 { // 只有登录用户才提取标签
			tagNames := s.extractTags(req.Content)
			if len(tagNames) > 0 {
				tagPtrs, err := s.tagRepo.FindOrCreateBatch(ctx, userID, tagNames)
				if err != nil {
					// 标签创建失败不影响主流程，但记录错误
					return fmt.Errorf("find or create tags: %w", err)
				}
				// 转换为 entity.Tag 切片
				if len(tagPtrs) > 0 {
					tags := make([]entity.Tag, 0, len(tagPtrs))
					for _, tagPtr := range tagPtrs {
						if tagPtr != nil && tagPtr.ID > 0 {
							tags = append(tags, *tagPtr)
						}
					}
					memo.Tags = tags
				}
			}
		}

		if err := s.memoRepo.Create(ctx, memo); err != nil {
			return fmt.Errorf("create memo: %w", err)
		}

		// 创建后需要显式保存标签关联（GORM Create 不会自动保存 many-to-many 关联）
		if userID > 0 && len(memo.Tags) > 0 {
			db := s.tx.GetDB(ctx)
			// 使用 memo.ID 确保使用正确的实例
			if err := db.Model(&entity.Memo{ID: memo.ID}).Association("Tags").Append(memo.Tags); err != nil {
				return fmt.Errorf("save tags: %w", err)
			}
		}

		createdAt = memo.CreatedAt
		return nil
	})

	if err != nil {
		return nil, err
	}

	return &CreateMemoResponse{
		ID:        memoID,
		CreatedAt: createdAt,
	}, nil
}

// GetMemoResponse Memo 详情响应
type GetMemoResponse struct {
	ID        uint64    `json:"id"`
	Content   string    `json:"content"`
	Images    []string  `json:"images"`
	Source    string    `json:"source"`
	CreatedAt time.Time `json:"created_at"`
	Tags      []string  `json:"tags"`
}

// GetMemo 获取 Memo 详情
// 默认允许所有用户访问（公开访问）
func (s *MemoService) GetMemo(ctx context.Context, userID uint64, id uint64) (*GetMemoResponse, error) {
	memo, err := s.memoRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("find memo: %w", err)
	}

	// 默认允许所有用户访问（不再检查权限）
	tags := make([]string, len(memo.Tags))
	for i, tag := range memo.Tags {
		tags[i] = tag.Name
	}

	return &GetMemoResponse{
		ID:        memo.ID,
		Content:   memo.Content,
		Images:    []string(memo.Images),
		Source:    memo.Source,
		CreatedAt: memo.CreatedAt,
		Tags:      tags,
	}, nil
}

// UpdateMemoRequest 更新 Memo 请求
type UpdateMemoRequest struct {
	Content string   `json:"content"`
	Images  []string `json:"images"` // 图片 URL 列表（已上传到七牛）
}

// UpdateMemoResponse 更新 Memo 响应
type UpdateMemoResponse struct {
	ID        uint64    `json:"id"`
	UpdatedAt time.Time `json:"updated_at"`
}

// UpdateMemo 更新 Memo
// 默认允许所有用户更新（公开访问）
func (s *MemoService) UpdateMemo(ctx context.Context, userID uint64, id uint64, req UpdateMemoRequest) (*UpdateMemoResponse, error) {
	var updatedAt time.Time

	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		memo, err := s.memoRepo.FindByID(ctx, id)
		if err != nil {
			return fmt.Errorf("find memo: %w", err)
		}

		// 更新内容
		memo.Content = req.Content
		memo.Images = entity.JSONStringArray(req.Images)
		updatedAt = time.Now()

		// 重新提取标签（因为内容可能变化）
		if userID > 0 {
			tagNames := s.extractTags(req.Content)
			var tags []entity.Tag
			if len(tagNames) > 0 {
				tagPtrs, err := s.tagRepo.FindOrCreateBatch(ctx, userID, tagNames)
				if err != nil {
					return fmt.Errorf("find or create tags: %w", err)
				}
				if len(tagPtrs) > 0 {
					tags = make([]entity.Tag, 0, len(tagPtrs))
					for _, tagPtr := range tagPtrs {
						if tagPtr != nil && tagPtr.ID > 0 {
							tags = append(tags, *tagPtr)
						}
					}
				}
			}
			memo.Tags = tags
		}

		if err := s.memoRepo.Update(ctx, memo); err != nil {
			return fmt.Errorf("update memo: %w", err)
		}

		// 更新标签关联
		if userID > 0 {
			db := s.tx.GetDB(ctx)
			// 先清空旧的标签关联
			if err := db.Model(&entity.Memo{ID: id}).Association("Tags").Clear(); err != nil {
				return fmt.Errorf("clear tags: %w", err)
			}
			// 再添加新的标签关联
			if len(memo.Tags) > 0 {
				if err := db.Model(&entity.Memo{ID: id}).Association("Tags").Append(memo.Tags); err != nil {
					return fmt.Errorf("update tags: %w", err)
				}
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return &UpdateMemoResponse{
		ID:        id,
		UpdatedAt: updatedAt,
	}, nil
}

// DeleteMemo 删除 Memo（软删除）
func (s *MemoService) DeleteMemo(ctx context.Context, userID, id uint64) error {
	err := s.tx.WithTx(ctx, func(ctx context.Context) error {
		memo, err := s.memoRepo.FindByID(ctx, id)
		if err != nil {
			return fmt.Errorf("find memo: %w", err)
		}
		if memo == nil {
			return fmt.Errorf("memo not found")
		}
		// 检查权限
		if memo.UserID != userID {
			return fmt.Errorf("permission denied")
		}

		// GORM 会自动使用软删除（因为 Memo 有 DeletedAt 字段）
		if err := s.memoRepo.Delete(ctx, id); err != nil {
			return fmt.Errorf("delete memo: %w", err)
		}

		return nil
	})

	return err
}

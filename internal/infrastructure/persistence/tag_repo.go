package persistence

import (
	"context"
	"errors"
	"goKit/internal/domain/entity"
	"goKit/internal/domain/repository"
	"goKit/pkg/kit/db"

	"gorm.io/gorm"
)

type TagRepo struct {
	client *db.Client
}

func NewTagRepo(client *db.Client) repository.TagRepository {
	return &TagRepo{client: client}
}

func (r *TagRepo) Create(ctx context.Context, tag *entity.Tag) error {
	return r.client.GetDB(ctx).Create(tag).Error
}

func (r *TagRepo) FindOrCreate(ctx context.Context, userID uint64, tagName string) (*entity.Tag, error) {
	var tag entity.Tag

	// 先尝试查找
	err := r.client.GetDB(ctx).Where("user_id = ? AND name = ?", userID, tagName).First(&tag).Error
	if err == nil {
		return &tag, nil
	}

	// 不存在则创建
	if errors.Is(err, gorm.ErrRecordNotFound) {
		tag = entity.Tag{
			UserID: userID,
			Name:   tagName,
		}
		if err := r.client.GetDB(ctx).Create(&tag).Error; err != nil {
			return nil, err
		}
		return &tag, nil
	}

	return nil, err
}

func (r *TagRepo) FindOrCreateBatch(ctx context.Context, userID uint64, tagNames []string) ([]*entity.Tag, error) {
	if len(tagNames) == 0 {
		return []*entity.Tag{}, nil
	}

	// 1. 批量查询已存在的标签
	var existingTags []*entity.Tag
	err := r.client.GetDB(ctx).Where("user_id = ? AND name IN ?", userID, tagNames).Find(&existingTags).Error
	if err != nil {
		return nil, err
	}

	// 2. 构建已存在标签的 map，用于快速查找
	existingMap := make(map[string]*entity.Tag)
	for _, tag := range existingTags {
		existingMap[tag.Name] = tag
	}

	// 3. 找出需要创建的标签
	var newTagNames []string
	for _, tagName := range tagNames {
		if _, exists := existingMap[tagName]; !exists {
			newTagNames = append(newTagNames, tagName)
		}
	}

	// 4. 批量创建新标签
	var newTags []*entity.Tag
	if len(newTagNames) > 0 {
		newTags = make([]*entity.Tag, 0, len(newTagNames))
		for _, tagName := range newTagNames {
			newTags = append(newTags, &entity.Tag{
				UserID: userID,
				Name:   tagName,
			})
		}

		// 使用 CreateInBatches 批量创建
		err = r.client.GetDB(ctx).CreateInBatches(newTags, 100).Error
		if err != nil {
			return nil, err
		}
	}

	// 5. 合并已存在和新创建的标签，保持原有顺序
	result := make([]*entity.Tag, 0, len(tagNames))
	tagMap := make(map[string]*entity.Tag)

	// 先添加已存在的标签到 map
	for _, tag := range existingTags {
		tagMap[tag.Name] = tag
	}

	// 再添加新创建的标签到 map
	for _, tag := range newTags {
		tagMap[tag.Name] = tag
	}

	// 按照 tagNames 的顺序返回结果
	for _, tagName := range tagNames {
		if tag, ok := tagMap[tagName]; ok {
			result = append(result, tag)
		}
	}

	return result, nil
}

func (r *TagRepo) FindByUserID(ctx context.Context, userID uint64) ([]*entity.Tag, error) {
	var tags []*entity.Tag
	err := r.client.GetDB(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&tags).Error
	return tags, err
}

func (r *TagRepo) FindByName(ctx context.Context, userID uint64, name string) (*entity.Tag, error) {
	var tag entity.Tag
	err := r.client.GetDB(ctx).Where("user_id = ? AND name = ?", userID, name).First(&tag).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *TagRepo) FindByID(ctx context.Context, id uint64) (*entity.Tag, error) {
	var tag entity.Tag
	err := r.client.GetDB(ctx).First(&tag, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &tag, nil
}

func (r *TagRepo) Delete(ctx context.Context, id uint64) error {
	// 删除标签会自动删除关联表中的记录（因为设置了 CASCADE）
	return r.client.GetDB(ctx).Delete(&entity.Tag{}, id).Error
}

func (r *TagRepo) FindMemosByTagID(ctx context.Context, tagID uint64, limit, offset int) ([]*entity.Memo, error) {
	var memos []*entity.Memo
	err := r.client.GetDB(ctx).
		Joins("JOIN memo_tags ON memos.id = memo_tags.memo_id").
		Where("memo_tags.tag_id = ? AND memos.deleted_at IS NULL", tagID).
		Order("memos.created_at DESC").
		Limit(limit).
		Offset(offset).
		Preload("Tags").
		Find(&memos).Error
	return memos, err
}

func (r *TagRepo) FindPagesByTagID(ctx context.Context, tagID uint64, limit, offset int) ([]*entity.Page, error) {
	var pages []*entity.Page
	err := r.client.GetDB(ctx).
		Joins("JOIN page_tags ON pages.id = page_tags.page_id").
		Where("page_tags.tag_id = ? AND pages.deleted_at IS NULL", tagID).
		Order("pages.updated_at DESC").
		Limit(limit).
		Offset(offset).
		Preload("Tags").
		Find(&pages).Error
	return pages, err
}

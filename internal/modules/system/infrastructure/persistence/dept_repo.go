package persistence

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"goKit/internal/modules/system/domain/entity"
	"goKit/internal/modules/system/domain/repository"
	"goKit/pkg/kit/db"
)

type deptRepo struct {
	client *db.Client
}

func NewDeptRepository(client *db.Client) repository.DeptRepository {
	return &deptRepo{client: client}
}

func (r *deptRepo) Create(ctx context.Context, dept *entity.Dept) error {
	return r.client.GetDB(ctx).Create(dept).Error
}

func (r *deptRepo) Update(ctx context.Context, dept *entity.Dept) error {
	return r.client.GetDB(ctx).Model(&entity.Dept{}).Where("id = ?", dept.ID).
		Select("parent_id", "name", "sort", "status").Updates(dept).Error
}

func (r *deptRepo) Delete(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).Where("id = ?", id).Delete(&entity.Dept{}).Error
}

func (r *deptRepo) FindByID(ctx context.Context, id uint64) (*entity.Dept, error) {
	var d entity.Dept
	err := r.client.GetDB(ctx).Where("id = ?", id).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &d, err
}

func (r *deptRepo) List(ctx context.Context) ([]entity.Dept, error) {
	var depts []entity.Dept
	err := r.client.GetDB(ctx).Order("sort, id").Find(&depts).Error
	return depts, err
}

func (r *deptRepo) FindDescendantIDs(ctx context.Context, deptID uint64) ([]uint64, error) {
	depts, err := r.List(ctx)
	if err != nil {
		return nil, err
	}
	children := make(map[uint64][]uint64, len(depts))
	for _, d := range depts {
		children[d.ParentID] = append(children[d.ParentID], d.ID)
	}
	ids := []uint64{deptID}
	queue := []uint64{deptID}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, childID := range children[cur] {
			ids = append(ids, childID)
			queue = append(queue, childID)
		}
	}
	return ids, nil
}

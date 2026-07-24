package persistence

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"goKit/internal/domain/dept"
	"goKit/pkg/kit/db"
)

type deptRepo struct {
	client *db.Client
}

func NewDeptRepository(client *db.Client) dept.DeptRepository {
	return &deptRepo{client: client}
}

func (r *deptRepo) Create(ctx context.Context, d *dept.Dept) error {
	return r.client.GetDB(ctx).Create(d).Error
}

func (r *deptRepo) Update(ctx context.Context, d *dept.Dept) error {
	return r.client.GetDB(ctx).Model(&dept.Dept{}).Where("id = ?", d.ID).
		Select("parent_id", "name", "sort", "status").Updates(d).Error
}

func (r *deptRepo) Delete(ctx context.Context, id uint64) error {
	return r.client.GetDB(ctx).Where("id = ?", id).Delete(&dept.Dept{}).Error
}

func (r *deptRepo) FindByID(ctx context.Context, id uint64) (*dept.Dept, error) {
	var d dept.Dept
	err := r.client.GetDB(ctx).Where("id = ?", id).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &d, err
}

func (r *deptRepo) List(ctx context.Context) ([]dept.Dept, error) {
	var depts []dept.Dept
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

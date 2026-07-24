package dept

import "context"

// DeptRepository 部门仓储
type DeptRepository interface {
	Create(ctx context.Context, dept *Dept) error
	Update(ctx context.Context, dept *Dept) error
	Delete(ctx context.Context, id uint64) error
	FindByID(ctx context.Context, id uint64) (*Dept, error)
	List(ctx context.Context) ([]Dept, error)
	// FindDescendantIDs 返回含自身在内的所有子孙部门 ID
	FindDescendantIDs(ctx context.Context, deptID uint64) ([]uint64, error)
}

package dept

import (
	"context"

	"goKit/internal/application/shared"
	domaindept "goKit/internal/domain/dept"
)

type DeptService struct {
	deptRepo domaindept.DeptRepository
}

func NewDeptService(deptRepo domaindept.DeptRepository) *DeptService {
	return &DeptService{deptRepo: deptRepo}
}

func (s *DeptService) Create(ctx context.Context, req CreateDeptReq) (uint64, error) {
	dept := &domaindept.Dept{
		ParentID: req.ParentID,
		Name:     req.Name,
		Sort:     req.Sort,
		Status:   1,
	}
	if err := s.deptRepo.Create(ctx, dept); err != nil {
		return 0, err
	}
	return dept.ID, nil
}

func (s *DeptService) Update(ctx context.Context, id uint64, req UpdateDeptReq) error {
	dept, err := s.deptRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if dept == nil {
		return shared.ErrNotFound
	}
	if req.ParentID == id {
		return shared.ErrForbidden // 不允许将父部门设为自己
	}
	dept.ParentID = req.ParentID
	dept.Name = req.Name
	dept.Sort = req.Sort
	dept.Status = req.Status
	return s.deptRepo.Update(ctx, dept)
}

func (s *DeptService) Delete(ctx context.Context, id uint64) error {
	dept, err := s.deptRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if dept == nil {
		return shared.ErrNotFound
	}
	return s.deptRepo.Delete(ctx, id)
}

// Tree 部门树
func (s *DeptService) Tree(ctx context.Context) ([]*DeptNode, error) {
	depts, err := s.deptRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make(map[uint64]*DeptNode, len(depts))
	for _, d := range depts {
		nodes[d.ID] = &DeptNode{
			ID:       d.ID,
			ParentID: d.ParentID,
			Name:     d.Name,
			Sort:     d.Sort,
			Status:   d.Status,
		}
	}
	var roots []*DeptNode
	for _, d := range depts {
		node := nodes[d.ID]
		if parent, ok := nodes[d.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}
	return roots, nil
}

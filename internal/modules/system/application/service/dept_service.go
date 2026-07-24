package service

import (
	"context"

	"goKit/internal/modules/system/application/dto"
	"goKit/internal/modules/system/domain/entity"
	"goKit/internal/modules/system/domain/repository"
)

type DeptService struct {
	deptRepo repository.DeptRepository
}

func NewDeptService(deptRepo repository.DeptRepository) *DeptService {
	return &DeptService{deptRepo: deptRepo}
}

func (s *DeptService) Create(ctx context.Context, req dto.CreateDeptReq) (uint64, error) {
	dept := &entity.Dept{
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

func (s *DeptService) Update(ctx context.Context, id uint64, req dto.UpdateDeptReq) error {
	dept, err := s.deptRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if dept == nil {
		return ErrNotFound
	}
	if req.ParentID == id {
		return ErrForbidden // 不允许将父部门设为自己
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
		return ErrNotFound
	}
	return s.deptRepo.Delete(ctx, id)
}

// Tree 部门树
func (s *DeptService) Tree(ctx context.Context) ([]*dto.DeptNode, error) {
	depts, err := s.deptRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make(map[uint64]*dto.DeptNode, len(depts))
	for _, d := range depts {
		nodes[d.ID] = &dto.DeptNode{
			ID:       d.ID,
			ParentID: d.ParentID,
			Name:     d.Name,
			Sort:     d.Sort,
			Status:   d.Status,
		}
	}
	var roots []*dto.DeptNode
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

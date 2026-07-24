package menu

import (
	"context"

	"goKit/internal/application/shared"
	domainmenu "goKit/internal/domain/menu"
)

type MenuService struct {
	menuRepo domainmenu.MenuRepository
}

func NewMenuService(menuRepo domainmenu.MenuRepository) *MenuService {
	return &MenuService{menuRepo: menuRepo}
}

func (s *MenuService) Create(ctx context.Context, req CreateMenuReq) (uint64, error) {
	menu := &domainmenu.Menu{
		ParentID: req.ParentID,
		Title:    req.Title,
		Type:     req.Type,
		Path:     req.Path,
		PermCode: req.PermCode,
		Sort:     req.Sort,
		Status:   1,
	}
	if err := s.menuRepo.Create(ctx, menu); err != nil {
		return 0, err
	}
	return menu.ID, nil
}

func (s *MenuService) Update(ctx context.Context, id uint64, req UpdateMenuReq) error {
	menu, err := s.menuRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if menu == nil {
		return shared.ErrNotFound
	}
	menu.ParentID = req.ParentID
	menu.Title = req.Title
	menu.Type = req.Type
	menu.Path = req.Path
	menu.PermCode = req.PermCode
	menu.Sort = req.Sort
	menu.Status = req.Status
	return s.menuRepo.Update(ctx, menu)
}

func (s *MenuService) Delete(ctx context.Context, id uint64) error {
	menu, err := s.menuRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if menu == nil {
		return shared.ErrNotFound
	}
	return s.menuRepo.Delete(ctx, id)
}

// Tree 菜单树
func (s *MenuService) Tree(ctx context.Context) ([]*MenuNode, error) {
	menus, err := s.menuRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	nodes := make(map[uint64]*MenuNode, len(menus))
	for _, m := range menus {
		nodes[m.ID] = &MenuNode{
			ID:       m.ID,
			ParentID: m.ParentID,
			Title:    m.Title,
			Type:     m.Type,
			Path:     m.Path,
			PermCode: m.PermCode,
			Sort:     m.Sort,
			Status:   m.Status,
		}
	}
	var roots []*MenuNode
	for _, m := range menus {
		node := nodes[m.ID]
		if parent, ok := nodes[m.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}
	return roots, nil
}

package menu

import (
	"context"

	"goKit/internal/application/shared"
	domainmenu "goKit/internal/domain/menu"
	domainrole "goKit/internal/domain/role"
	kitauth "goKit/pkg/kit/auth"
)

type MenuService struct {
	menuRepo   domainmenu.MenuRepository
	assignRepo domainrole.AssignRepository
}

func NewMenuService(menuRepo domainmenu.MenuRepository, assignRepo domainrole.AssignRepository) *MenuService {
	return &MenuService{menuRepo: menuRepo, assignRepo: assignRepo}
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
	return buildTree(menus, func(m *domainmenu.Menu) bool { return true }), nil
}

// Mine 当前登录用户可见的菜单树（目录+菜单，不含按钮权限点）。
// 超管返回全部；普通用户按角色关联的菜单过滤，并自动补全祖先目录。
func (s *MenuService) Mine(ctx context.Context) ([]*MenuNode, error) {
	cu := kitauth.FromContext(ctx)
	if cu == nil {
		return nil, shared.ErrInvalidCredential
	}
	menus, err := s.menuRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	visible := make(map[uint64]bool, len(menus))
	if cu.IsSuper {
		for i := range menus {
			visible[menus[i].ID] = true
		}
	} else {
		roleIDs, err := s.assignRepo.GetRoleIDsByUser(ctx, cu.UserID)
		if err != nil {
			return nil, err
		}
		ids, err := s.menuRepo.FindIDsByRoleIDs(ctx, roleIDs)
		if err != nil {
			return nil, err
		}
		for _, id := range ids {
			visible[id] = true
		}
		// 补全祖先节点，避免子菜单可见但父目录缺失导致悬挂
		byID := make(map[uint64]*domainmenu.Menu, len(menus))
		for i := range menus {
			byID[menus[i].ID] = &menus[i]
		}
		for _, id := range ids {
			for m := byID[id]; m != nil && m.ParentID != 0 && !visible[m.ParentID]; m = byID[m.ParentID] {
				visible[m.ParentID] = true
			}
		}
	}

	return buildTree(menus, func(m *domainmenu.Menu) bool {
		return visible[m.ID] && m.Status == 1 &&
			(m.Type == domainmenu.MenuTypeDir || m.Type == domainmenu.MenuTypeMenu)
	}), nil
}

// buildTree 按 keep 过滤后组树，输入需已按 sort,id 排序
func buildTree(menus []domainmenu.Menu, keep func(*domainmenu.Menu) bool) []*MenuNode {
	nodes := make(map[uint64]*MenuNode, len(menus))
	for i := range menus {
		m := &menus[i]
		if !keep(m) {
			continue
		}
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
	for i := range menus {
		node, ok := nodes[menus[i].ID]
		if !ok {
			continue
		}
		if parent, ok := nodes[node.ParentID]; ok {
			parent.Children = append(parent.Children, node)
		} else {
			roots = append(roots, node)
		}
	}
	return roots
}

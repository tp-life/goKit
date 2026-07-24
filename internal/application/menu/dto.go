package menu

// CreateMenuReq 创建菜单
type CreateMenuReq struct {
	ParentID uint64 `json:"parent_id"`
	Title    string `json:"title" validate:"required"`
	Type     int8   `json:"type" validate:"omitempty,oneof=1 2 3"`
	Path     string `json:"path"`
	PermCode string `json:"perm_code"`
	Sort     int    `json:"sort"`
}

// UpdateMenuReq 更新菜单
type UpdateMenuReq struct {
	ParentID uint64 `json:"parent_id"`
	Title    string `json:"title" validate:"required"`
	Type     int8   `json:"type" validate:"omitempty,oneof=1 2 3"`
	Path     string `json:"path"`
	PermCode string `json:"perm_code"`
	Sort     int    `json:"sort"`
	Status   int8   `json:"status" validate:"oneof=0 1"`
}

// MenuNode 菜单树节点
type MenuNode struct {
	ID       uint64      `json:"id"`
	ParentID uint64      `json:"parent_id"`
	Title    string      `json:"title"`
	Type     int8        `json:"type"`
	Path     string      `json:"path"`
	PermCode string      `json:"perm_code"`
	Sort     int         `json:"sort"`
	Status   int8        `json:"status"`
	Children []*MenuNode `json:"children,omitempty"`
}

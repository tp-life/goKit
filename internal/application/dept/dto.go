package dept

// CreateDeptReq 创建部门
type CreateDeptReq struct {
	ParentID uint64 `json:"parent_id"`
	Name     string `json:"name" validate:"required"`
	Sort     int    `json:"sort"`
}

// UpdateDeptReq 更新部门
type UpdateDeptReq struct {
	ParentID uint64 `json:"parent_id"`
	Name     string `json:"name" validate:"required"`
	Sort     int    `json:"sort"`
	Status   int8   `json:"status" validate:"oneof=0 1"`
}

// DeptNode 部门树节点
type DeptNode struct {
	ID       uint64      `json:"id"`
	ParentID uint64      `json:"parent_id"`
	Name     string      `json:"name"`
	Sort     int         `json:"sort"`
	Status   int8        `json:"status"`
	Children []*DeptNode `json:"children,omitempty"`
}

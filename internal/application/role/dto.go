package role

import "time"

// CreateRoleReq 创建角色
type CreateRoleReq struct {
	Name      string   `json:"name" validate:"required"`
	Code      string   `json:"code" validate:"required"`
	DataScope int8     `json:"data_scope" validate:"omitempty,oneof=1 2 3 4 5"`
	Remark    string   `json:"remark"`
	MenuIDs   []uint64 `json:"menu_ids"`
	DeptIDs   []uint64 `json:"dept_ids"` // data_scope=2 时生效
}

// UpdateRoleReq 更新角色
type UpdateRoleReq struct {
	Name      string   `json:"name" validate:"required"`
	Code      string   `json:"code" validate:"required"`
	DataScope int8     `json:"data_scope" validate:"omitempty,oneof=1 2 3 4 5"`
	Status    int8     `json:"status" validate:"oneof=0 1"`
	Remark    string   `json:"remark"`
	DeptIDs   []uint64 `json:"dept_ids"` // 非 nil 时重置自定义部门
}

// RoleResp 角色信息
type RoleResp struct {
	ID        uint64    `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	DataScope int8      `json:"data_scope"`
	Status    int8      `json:"status"`
	Remark    string    `json:"remark"`
	MenuIDs   []uint64  `json:"menu_ids,omitempty"`
	DeptIDs   []uint64  `json:"dept_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// AssignMenusReq 给角色分配菜单权限
type AssignMenusReq struct {
	MenuIDs []uint64 `json:"menu_ids" validate:"required"`
}

// AssignUsersReq 给角色分配用户
type AssignUsersReq struct {
	UserIDs []uint64 `json:"user_ids" validate:"required"`
}

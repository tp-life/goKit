package dto

import "time"

// CreateUserReq 创建用户
type CreateUserReq struct {
	Username string   `json:"username"`
	Password string   `json:"password"`
	Nickname string   `json:"nickname"`
	Email    string   `json:"email"`
	DeptID   uint64   `json:"dept_id"`
	RoleIDs  []uint64 `json:"role_ids"`
}

// UpdateUserReq 更新用户
type UpdateUserReq struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	DeptID   uint64 `json:"dept_id"`
	Status   int8   `json:"status"`
}

// UserResp 用户信息（不含密码）
type UserResp struct {
	ID        uint64    `json:"id"`
	Username  string    `json:"username"`
	Nickname  string    `json:"nickname"`
	Email     string    `json:"email"`
	DeptID    uint64    `json:"dept_id"`
	Status    int8      `json:"status"`
	IsSuper   bool      `json:"is_super"`
	RoleIDs   []uint64  `json:"role_ids,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// AssignRolesReq 给用户分配角色
type AssignRolesReq struct {
	RoleIDs []uint64 `json:"role_ids"`
}

// ResetPwdReq 重置用户密码
type ResetPwdReq struct {
	Password string `json:"password"`
}

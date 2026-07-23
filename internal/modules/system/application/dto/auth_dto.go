package dto

// LoginReq 登录请求
type LoginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResp 登录响应
type LoginResp struct {
	Token string `json:"token"`
}

// ProfileResp 当前用户信息
type ProfileResp struct {
	ID       uint64   `json:"id"`
	Username string   `json:"username"`
	Nickname string   `json:"nickname"`
	Email    string   `json:"email"`
	DeptID   uint64   `json:"dept_id"`
	IsSuper  bool     `json:"is_super"`
	Roles    []string `json:"roles"`
	Perms    []string `json:"perms"`
}

// ChangePwdReq 修改密码
type ChangePwdReq struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

package dto

// PageReq 通用分页请求
type PageReq struct {
	Page     int `json:"page" query:"page"`
	PageSize int `json:"page_size" query:"page_size"`
}

// Normalize 修正非法分页参数
func (p *PageReq) Normalize() {
	if p.Page <= 0 {
		p.Page = 1
	}
	if p.PageSize <= 0 || p.PageSize > 100 {
		p.PageSize = 20
	}
}

// PageResp 通用分页响应
type PageResp[T any] struct {
	List  []T   `json:"list"`
	Total int64 `json:"total"`
}

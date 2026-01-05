package dto

type CreateUserReq struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type UserResp struct {
	ID    uint64 `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

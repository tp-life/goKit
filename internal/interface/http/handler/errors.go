package handler

import (
	"errors"

	"goKit/internal/application/shared"
	"goKit/internal/interface/http/response"
)

// mapErr 将应用层错误映射为统一响应错误
func mapErr(err error) *response.AppError {
	switch {
	case errors.Is(err, shared.ErrNotFound):
		return response.ErrNotFound("记录不存在")
	case errors.Is(err, shared.ErrInvalidCredential):
		return response.ErrUnauthorized("账号或密码错误")
	case errors.Is(err, shared.ErrForbidden):
		return response.ErrForbidden("")
	case errors.Is(err, shared.ErrDuplicate):
		return response.ErrBadRequest("记录已存在")
	default:
		return response.ErrInternal(err, "")
	}
}

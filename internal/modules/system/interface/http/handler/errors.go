package handler

import (
	"errors"

	"goKit/internal/interface/http/response"
	"goKit/internal/modules/system/application/service"
)

// mapErr 将应用层错误映射为统一响应错误
func mapErr(err error) *response.AppError {
	switch {
	case errors.Is(err, service.ErrNotFound):
		return response.ErrNotFound("记录不存在")
	case errors.Is(err, service.ErrInvalidCredential):
		return response.ErrUnauthorized("账号或密码错误")
	case errors.Is(err, service.ErrForbidden):
		return response.ErrForbidden("")
	case errors.Is(err, service.ErrDuplicate):
		return response.ErrBadRequest("记录已存在")
	default:
		return response.ErrInternal(err, "")
	}
}

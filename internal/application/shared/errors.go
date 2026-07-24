package shared

import "errors"

// 业务错误
var (
	ErrNotFound          = errors.New("record not found")
	ErrInvalidCredential = errors.New("invalid credential")
	ErrForbidden         = errors.New("forbidden")
	ErrDuplicate         = errors.New("duplicate")
)

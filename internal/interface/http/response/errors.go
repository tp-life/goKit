package response

import "fmt"

const (
	CodeSuccess        = 0
	CodeParamError     = 40000
	CodeUnauthorized   = 40100
	CodeForbidden      = 40300
	CodeNotFound       = 40400
	CodeInternalServer = 50000
)

type AppError struct {
	HTTPCode     int
	BusinessCode int
	Message      string
	RawError     error
}

func (e *AppError) Error() string {
	if e.RawError != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.RawError)
	}
	return e.Message
}

func ErrBadRequest(msg string) *AppError {
	return &AppError{HTTPCode: 400, BusinessCode: CodeParamError, Message: msg}
}

func ErrNotFound(msg string) *AppError {
	return &AppError{HTTPCode: 404, BusinessCode: CodeNotFound, Message: msg}
}

func ErrInternal(err error, msg string) *AppError {
	if msg == "" {
		msg = "服务器开小差了，请稍后再试"
	}
	return &AppError{HTTPCode: 500, BusinessCode: CodeInternalServer, Message: msg, RawError: err}
}

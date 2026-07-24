package handler

import (
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"goKit/internal/interface/http/response"
)

type validateReq struct {
	Username string `json:"username" validate:"required,min=3"`
	Email    string `json:"email" validate:"omitempty,email"`
	Status   int8   `json:"status" validate:"oneof=0 1"`
}

func TestValidate(t *testing.T) {
	// Validate 当前不读取 *fiber.Ctx，传 nil 即可
	var c *fiber.Ctx

	t.Run("合法输入放行", func(t *testing.T) {
		if err := Validate(c, &validateReq{Username: "alice", Email: "a@b.c", Status: 1}); err != nil {
			t.Errorf("Validate: %v", err)
		}
	})

	t.Run("必填缺失返回 400 并指出字段", func(t *testing.T) {
		err := Validate(c, &validateReq{Status: 1})
		appErr, ok := err.(*response.AppError)
		if !ok {
			t.Fatalf("err = %v, want *AppError", err)
		}
		if appErr.HTTPCode != 400 || appErr.BusinessCode != response.CodeParamError {
			t.Errorf("appErr = %+v", appErr)
		}
		if !strings.Contains(appErr.Message, "username") {
			t.Errorf("message = %q, 应使用 json 字段名", appErr.Message)
		}
	})

	t.Run("非法枚举返回 400", func(t *testing.T) {
		err := Validate(c, &validateReq{Username: "alice", Status: 9})
		appErr, ok := err.(*response.AppError)
		if !ok {
			t.Fatalf("err = %v, want *AppError", err)
		}
		if appErr.HTTPCode != 400 {
			t.Errorf("HTTPCode = %d, want 400", appErr.HTTPCode)
		}
		if !strings.Contains(appErr.Message, "status") {
			t.Errorf("message = %q, 应指出 status 字段", appErr.Message)
		}
	})

	t.Run("email 格式非法返回 400", func(t *testing.T) {
		err := Validate(c, &validateReq{Username: "alice", Email: "bad"})
		if err == nil {
			t.Fatal("email 非法应报错")
		}
		if !strings.Contains(err.Error(), "email") {
			t.Errorf("message = %q, 应指出 email 字段", err.Error())
		}
	})
}

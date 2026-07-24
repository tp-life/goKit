package handler

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"

	"goKit/internal/interface/http/response"
)

// validate 单例校验器，字段名取 json tag 便于前端定位
var validate = func() *validator.Validate {
	v := validator.New()
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "" {
			return fld.Name
		}
		return name
	})
	return v
}()

// Validate 校验请求体，失败时返回 400 参数错误，message 带具体字段原因
func Validate(c *fiber.Ctx, v any) error {
	err := validate.Struct(v)
	if err == nil {
		return nil
	}
	if errs, ok := err.(validator.ValidationErrors); ok {
		return response.ErrBadRequest(fieldErrorMsg(errs[0]))
	}
	return response.ErrBadRequest("请求参数校验失败")
}

// fieldErrorMsg 将单个字段错误翻译为可读提示
func fieldErrorMsg(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s 为必填项", fe.Field())
	case "min":
		return fmt.Sprintf("%s 长度不能小于 %s", fe.Field(), fe.Param())
	case "max":
		return fmt.Sprintf("%s 长度不能超过 %s", fe.Field(), fe.Param())
	case "email":
		return fmt.Sprintf("%s 格式不正确", fe.Field())
	case "oneof":
		return fmt.Sprintf("%s 取值不合法", fe.Field())
	default:
		return fmt.Sprintf("%s 不符合校验规则 %s", fe.Field(), fe.Tag())
	}
}

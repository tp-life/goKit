package http

import (
	"github.com/gofiber/fiber/v2"
)

// ==========================================
// 1. 定义全局业务状态码字典 (可根据你的业务线横向扩展)
// ==========================================
const (
	CodeSuccess        = 0     // 成功
	CodeParamError     = 40000 // 客户端参数错误
	CodeUnauthorized   = 40100 // 未登录/Token失效
	CodeForbidden      = 40300 // 登录了但无权限访问该资源
	CodeNotFound       = 40400 // 资源不存在
	CodeInternalServer = 50000 // 服务器内部错误 (如 DB 挂了、空指针等)
)

// Response 标准 API 返回结构
type Response struct {
	Code    int    `json:"code"`           // 业务状态码
	Message string `json:"message"`        // 提示信息
	Data    any    `json:"data,omitempty"` // 核心数据，omitempty 可避免 data 为 nil 时输出 null
}

// ==========================================
// 2. 基础响应函数
// ==========================================

// Success 统一成功返回
func Success(c *fiber.Ctx, data any) error {
	return c.JSON(Response{
		Code:    CodeSuccess,
		Message: "success",
		Data:    data,
	})
}

// Error 基础错误返回 (保留，用于一些需要高度自定义状态码的特殊场景)
func Error(c *fiber.Ctx, status int, businessCode int, message string) error {
	return c.Status(status).JSON(Response{
		Code:    businessCode,
		Message: message,
	})
}

// ==========================================
// 3. 语义化的快捷错误返回函数 (强烈推荐 Handler 中直接调用这些)
// ==========================================

// BadRequest 400 请求参数错误
// 场景: 表单校验失败、JSON 解析失败、缺少必填项
func BadRequest(c *fiber.Ctx, message string) error {
	if message == "" {
		message = "bad request parameters"
	}
	return Error(c, fiber.StatusBadRequest, CodeParamError, message)
}

// Unauthorized 401 认证失败
// 场景: Header 没带 Token、Token 伪造或已过期
func Unauthorized(c *fiber.Ctx, message string) error {
	if message == "" {
		message = "unauthorized access"
	}
	return Error(c, fiber.StatusUnauthorized, CodeUnauthorized, message)
}

// Forbidden 403 权限不足
// 场景: 普通用户试图调用管理员专属 API
func Forbidden(c *fiber.Ctx, message string) error {
	if message == "" {
		message = "permission denied"
	}
	return Error(c, fiber.StatusForbidden, CodeForbidden, message)
}

// NotFound 404 资源不存在
// 场景: 根据 ID 查数据库没查到数据 (比如 /users/999 不存在)
func NotFound(c *fiber.Ctx, message string) error {
	if message == "" {
		message = "resource not found"
	}
	return Error(c, fiber.StatusNotFound, CodeNotFound, message)
}

// InternalServer 500 服务器内部错误
// 场景: 数据库查询报错、调用外部第三方 RPC 超时、以及任何不想暴露给前端的底层错误
func InternalServer(c *fiber.Ctx, message string) error {
	if message == "" {
		message = "internal server error"
	}
	return Error(c, fiber.StatusInternalServerError, CodeInternalServer, message)
}

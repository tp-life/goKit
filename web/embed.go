package frontend

import (
	"embed"
	"io/fs"
	"mime"
	"path"
	"strings"

	"github.com/gofiber/fiber/v2"
)

// assetsFS 内嵌整个前端目录。
// 这样最终编译后的二进制会自带前端页面，分发时不需要再额外携带 web 目录。
//
//go:embed index.html assets/*
var assetsFS embed.FS

// RegisterRoutes 注册嵌入式前端页面路由。
// 页面入口固定为 / ，静态资源走 /assets/* 。
func RegisterRoutes(app *fiber.App) {
	sub, err := fs.Sub(assetsFS, ".")
	if err != nil {
		panic(err)
	}

	app.Get("/", func(c *fiber.Ctx) error {
		return serveFile(c, sub, "index.html")
	})

	app.Get("/assets/*", func(c *fiber.Ctx) error {
		name := strings.TrimPrefix(c.Path(), "/")
		return serveFile(c, sub, name)
	})
}

func serveFile(c *fiber.Ctx, fileSystem fs.FS, name string) error {
	body, err := fs.ReadFile(fileSystem, name)
	if err != nil {
		return fiber.ErrNotFound
	}

	ext := path.Ext(name)
	if ext != "" {
		if contentType := mime.TypeByExtension(ext); contentType != "" {
			c.Type(ext)
			c.Set(fiber.HeaderContentType, contentType)
		}
	}
	return c.Send(body)
}

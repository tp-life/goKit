// Package migrations 内嵌版本化数据库迁移脚本（golang-migrate 格式），
// 由 pkg/kit/db.RunMigrations 在启动时执行。
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS

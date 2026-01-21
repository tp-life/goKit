# 软删除与全文检索功能实现总结

## 已完成的功能

### 模块 A：软删除与回收站 (Soft Delete & Trash)

#### 1. 数据库变更
- ✅ 为 `memos`、`pages`、`blocks` 三张表添加了 `deleted_at` 字段
- ✅ 创建了相应的索引以优化查询性能
- ✅ 迁移文件：`docs/migrations/005_add_soft_delete_and_fulltext_search.sql`

#### 2. Entity 升级
- ✅ `Memo`、`Page`、`Block` 实体都嵌入了 `gorm.DeletedAt` 字段
- ✅ GORM 现在会自动处理软删除逻辑

#### 3. 级联软删除
- ✅ Page 删除时，其下的 Blocks 会自动软删除
- ✅ 恢复 Page 时，会同时恢复其下的所有 Blocks
- ✅ 在 `TrashRepo.RestorePage` 和 `TrashRepo.PermanentlyDeletePage` 中实现

#### 4. API 接口

| 方法 | 路径 | 功能 | 状态 |
| --- | --- | --- | --- |
| GET | `/api/v1/trash` | 获取回收站列表 | ✅ |
| POST | `/api/v1/trash/:type/:id/restore` | 恢复项目 | ✅ |
| DELETE | `/api/v1/trash/:type/:id` | 彻底删除 | ✅ |

**注意：** 回收站相关接口需要 JWT 认证（需要登录）

#### 5. 实现文件
- `internal/domain/repository/trash_repo.go` - 回收站仓库接口
- `internal/infrastructure/persistence/trash_repo.go` - 回收站仓库实现
- `internal/application/service/trash_service.go` - 回收站服务
- `internal/interface/http/trash_handler.go` - 回收站处理器

---

### 模块 B：全文检索 (Full-Text Search)

#### 1. 数据库变更
- ✅ 在 `blocks` 表中添加了虚拟列 `search_content`，自动从 JSON 中提取 `text` 字段
- ✅ 为 `blocks.search_content` 和 `memos.content` 创建了全文索引（使用 ngram 分词器支持中文）
- ✅ 迁移文件：`docs/migrations/005_add_soft_delete_and_fulltext_search.sql`

#### 2. 搜索功能
- ✅ 支持搜索 Memos 和 Pages（通过 Blocks 关联）
- ✅ 使用 MySQL 的 `MATCH...AGAINST` 语法进行全文检索
- ✅ 支持 BOOLEAN MODE，提供更灵活的搜索功能
- ✅ 搜索结果去重（防止同一 Page 多次出现）

#### 3. API 接口

| 方法 | 路径 | 功能 | 状态 |
| --- | --- | --- | --- |
| GET | `/api/v1/search?q=关键词` | 执行搜索 | ✅ |

**注意：** 搜索接口默认公开访问（无需登录）

#### 4. 实现文件
- `internal/domain/repository/trash_repo.go` - 搜索仓库接口（与回收站接口在同一文件）
- `internal/infrastructure/persistence/search_repo.go` - 搜索仓库实现
- `internal/application/service/search_service.go` - 搜索服务
- `internal/interface/http/search_handler.go` - 搜索处理器

---

## 数据库迁移说明

### 执行迁移

```bash
mysql -u root -p notion_lite < docs/migrations/005_add_soft_delete_and_fulltext_search.sql
```

### 注意事项

1. **MySQL 版本要求：** 推荐使用 MySQL 8.0+，最低版本 5.7.6（支持 Generated Columns）
2. **锁表风险：** `ADD FULLTEXT INDEX` 可能会锁表，建议在低峰期执行
3. **ngram 分词器：** 仅 MySQL 8.0+ 支持，旧版本可能需要手动分词或使用其他方案

---

## 使用示例

### 回收站功能

#### 1. 获取回收站列表
```bash
curl -X GET http://localhost:8080/api/v1/trash \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

#### 2. 恢复项目
```bash
# 恢复 Memo
curl -X POST http://localhost:8080/api/v1/trash/memo/123456/restore \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"

# 恢复 Page
curl -X POST http://localhost:8080/api/v1/trash/page/789012/restore \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

#### 3. 彻底删除
```bash
# 彻底删除 Memo
curl -X DELETE http://localhost:8080/api/v1/trash/memo/123456 \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"

# 彻底删除 Page
curl -X DELETE http://localhost:8080/api/v1/trash/page/789012 \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"
```

### 搜索功能

#### 执行搜索
```bash
curl -X GET "http://localhost:8080/api/v1/search?q=Golang" \
  -H "Authorization: Bearer YOUR_JWT_TOKEN"

# 带分页参数
curl -X GET "http://localhost:8080/api/v1/search?q=Golang&limit=20&offset=0"
```

---

## 技术细节

### 软删除实现

1. **GORM 自动处理：** 使用 `gorm.DeletedAt` 后，GORM 会自动：
   - 查询时过滤 `deleted_at IS NULL` 的记录
   - `Delete()` 方法只会设置 `deleted_at` 时间戳，不会物理删除

2. **查询已删除记录：** 使用 `Unscoped()` 方法：
   ```go
   db.Unscoped().Where("deleted_at IS NOT NULL").Find(&items)
   ```

3. **永久删除：** 使用 `Unscoped().Delete()` 进行物理删除

### 全文检索实现

1. **虚拟列：** 使用 `GENERATED ALWAYS AS` 创建虚拟列，自动从 JSON 中提取文本
2. **ngram 分词器：** 支持中文分词，适合中文内容搜索
3. **BOOLEAN MODE：** 支持更灵活的搜索语法，如 `+keyword*` 表示必须包含且支持前缀匹配

---

## 后续工作建议

1. **前端集成：**
   - 添加回收站 UI 页面
   - 在搜索栏中添加搜索功能
   - 实现关键词高亮显示

2. **性能优化：**
   - 考虑添加搜索结果的缓存
   - 对于大量数据，可以考虑使用 Elasticsearch

3. **功能扩展：**
   - 支持搜索历史记录
   - 支持高级搜索（按时间范围、类型筛选等）
   - 回收站的自动清理功能（定期清理超过一定时间的已删除项）

---

## 相关文件清单

### 新增文件
- `internal/domain/repository/trash_repo.go`
- `internal/infrastructure/persistence/trash_repo.go`
- `internal/infrastructure/persistence/search_repo.go`
- `internal/application/service/trash_service.go`
- `internal/application/service/search_service.go`
- `internal/interface/http/trash_handler.go`
- `internal/interface/http/search_handler.go`
- `docs/migrations/005_add_soft_delete_and_fulltext_search.sql`

### 修改文件
- `internal/domain/entity/memo.go` - 添加 `gorm.DeletedAt`
- `internal/domain/entity/page.go` - 添加 `gorm.DeletedAt`
- `internal/domain/entity/block.go` - 添加 `gorm.DeletedAt`
- `internal/infrastructure/persistence/block_repo.go` - 软删除注释更新
- `internal/interface/http/routes.go` - 添加新路由
- `cmd/server/main.go` - 添加依赖注入

---

## 测试建议

1. **软删除测试：**
   - 创建 Memo/Page 后删除，验证不会物理删除
   - 验证查询时不会返回已删除的记录
   - 测试恢复功能
   - 测试级联软删除（删除 Page 时，Blocks 也被软删除）

2. **搜索测试：**
   - 测试中文搜索
   - 测试英文搜索
   - 测试搜索结果去重
   - 测试分页功能

3. **边界情况：**
   - 搜索空字符串
   - 搜索不存在的关键词
   - 恢复不存在的项目
   - 权限验证

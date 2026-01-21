# 数据库迁移脚本

## 使用说明

### 1. 初始化数据库结构

```bash
mysql -u root -p < docs/migrations/001_initial_schema.sql
```

这将创建数据库和所有表结构。

### 2. 修复 share_id 索引问题（重要）

**必须先执行此脚本，否则创建页面时会报错：**

```bash
mysql -u root -p < docs/migrations/004_fix_share_id_index.sql
```

这将：
- 将 `share_id` 字段改为允许 NULL
- 将现有的空字符串 `''` 转换为 NULL
- 创建唯一索引（NULL 值不会冲突）

### 3. 插入测试数据（可选）

```bash
mysql -u root -p < docs/migrations/002_insert_test_data.sql
```

这将插入以下测试数据：
- 2 个测试用户（test@example.com, demo@example.com）
- 密码均为：`123456`
- 5 条 Memos（闪念）
- 4 个 Pages（页面）
- 15 个 Blocks（Editor.js 块数据）
- 1 个用户会话

### 4. 验证数据（可选）

```bash
mysql -u root -p < docs/migrations/003_verify_test_data.sql
```

### 5. 测试账号

| 邮箱 | 密码 | 说明 |
|------|------|------|
| test@example.com | 123456 | 主测试账号，有多个 Memos 和 Pages |
| demo@example.com | 123456 | 辅助测试账号，用于测试多用户场景 |

### 6. 添加软删除和全文搜索功能

```bash
mysql -u root -p < docs/migrations/005_add_soft_delete_and_fulltext_search.sql
```

这将：
- 为 `memos`、`pages`、`blocks` 表添加 `deleted_at` 字段（软删除）
- 为 `blocks` 表添加 `search_content` 虚拟列（从 JSON 中提取文本）
- 为 `blocks` 和 `memos` 表创建全文索引（支持中文搜索）

**注意：**
- 需要 MySQL 8.0+ 版本
- 创建全文索引可能会锁表，建议在低峰期执行
- 如果索引创建失败，请执行修复脚本

### 7. 修复全文索引（如果 005 脚本失败）

如果执行 005 脚本后，blocks 表的全文索引没有创建成功，请按以下步骤操作：

**步骤 1: 诊断问题**
```bash
mysql -u root -p < docs/migrations/008_diagnose_fulltext_index.sql
```

这将显示：
- `search_content` 虚拟列是否存在
- 当前所有索引状态
- MySQL 版本信息
- 虚拟列数据情况

**步骤 2: 修复索引**
```bash
mysql -u root -p < docs/migrations/009_fix_blocks_fulltext_index.sql
```

或者使用通用修复脚本：
```bash
mysql -u root -p < docs/migrations/007_fix_fulltext_index.sql
```

**常见问题：**
1. **错误：'Fulltext index on virtual generated column' is not supported**
   - **原因**：MySQL 不支持在 VIRTUAL 生成列上创建全文索引
   - **解决**：必须使用 STORED 生成列。执行转换脚本：
     ```bash
     mysql -u root -p < docs/migrations/010_convert_search_content_to_stored.sql
     ```
   - **说明**：
     - VIRTUAL 生成列：不占磁盘空间，实时计算，但**无法创建全文索引**
     - STORED 生成列：占用磁盘空间，可以创建全文索引
     - 所有相关脚本已更新为使用 STORED

2. **错误：Can't find FULLTEXT index matching the column list**
   - 原因：`blocks` 表的 `search_content` 列没有全文索引
   - 解决：执行修复脚本 `009_fix_blocks_fulltext_index.sql` 或 `010_convert_search_content_to_stored.sql`

3. **错误：WITH PARSER ngram 不支持**
   - 原因：MySQL 版本 < 8.0 或不支持 ngram 解析器
   - 解决：修改脚本，移除 `WITH PARSER ngram` 部分（但会失去中文分词支持）

4. **索引创建很慢或超时**
   - 原因：表中数据量很大
   - 解决：在低峰期执行，或分批处理数据

### 8. 分享链接示例

- Page 1 (已分享): `/s/share-page-001`
- Page 3 (已分享): `/s/share-page-002`

## 注意事项

1. **密码哈希**: 测试数据中的密码使用 Bcrypt 加密，对应密码为 `123456`
2. **ID 生成**: Memos 和 Pages 的 ID 使用时间戳（微秒级），实际生产环境应使用雪花算法
3. **JSON 字段**: Blocks 的 `data` 字段和 Memos 的 `images` 字段使用 JSON 格式存储
4. **时间戳**: 所有时间戳使用 `DATETIME(3)` 格式，精确到毫秒
5. **share_id 索引**: 必须执行 `004_fix_share_id_index.sql`，否则创建页面时会报唯一索引冲突错误

## 清空测试数据

如果需要清空测试数据重新插入，可以执行：

```sql
USE notion_lite;
TRUNCATE TABLE blocks;
TRUNCATE TABLE pages;
TRUNCATE TABLE memos;
TRUNCATE TABLE user_sessions;
TRUNCATE TABLE users;
```

然后重新执行 `002_insert_test_data.sql`。

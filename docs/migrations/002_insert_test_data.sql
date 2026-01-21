-- Notion-Lite 测试数据插入脚本
-- 注意：密码使用 Bcrypt 加密，默认密码为 "123456"

USE `notion_lite`;

-- 清空现有数据（可选，谨慎使用）
-- TRUNCATE TABLE `blocks`;
-- TRUNCATE TABLE `pages`;
-- TRUNCATE TABLE `memos`;
-- TRUNCATE TABLE `user_sessions`;
-- TRUNCATE TABLE `users`;

-- 插入测试用户
-- 密码: 123456 (Bcrypt 哈希)
INSERT INTO `users` (`id`, `email`, `password_hash`, `salt`, `created_at`) VALUES
(1, 'test@example.com', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', '', '2024-01-01 10:00:00.000'),
(2, 'demo@example.com', '$2a$10$N9qo8uLOickgx2ZMRZoMyeIjZAgcfl7p92ldGxad68LJZdL17lhWy', '', '2024-01-02 10:00:00.000');

-- 插入用户会话（Refresh Token）
INSERT INTO `user_sessions` (`id`, `user_id`, `refresh_token`, `device_info`, `expires_at`, `created_at`) VALUES
(1, 1, 'test_refresh_token_123456789', 'Chrome/Windows', '2024-02-01 10:00:00.000', '2024-01-01 10:00:00.000');

-- 插入 Memos（闪念）
-- 注意：ID 使用时间戳（微秒级）
INSERT INTO `memos` (`id`, `user_id`, `content`, `images`, `source`, `created_at`) VALUES
(1704067200000000, 1, '今天天气真好，适合出去走走！', '[]', 'mobile', '2024-01-01 10:00:00.000'),
(1704153600000000, 1, '记录一个想法：Notion-Lite 项目进展顺利，前端和后端都已经基本完成。', '[]', 'mobile', '2024-01-02 10:00:00.000'),
(1704240000000000, 1, '今天学习了 Go 语言的依赖注入，使用 Uber Fx 框架。', '[]', 'web', '2024-01-03 10:00:00.000'),
(1704326400000000, 1, '周末计划：
- 完成项目文档
- 测试所有功能
- 准备演示', '[]', 'mobile', '2024-01-04 10:00:00.000'),
(1704412800000000, 2, '这是第二个用户的测试 Memo', '[]', 'mobile', '2024-01-05 10:00:00.000');

-- 插入 Pages（页面）
INSERT INTO `pages` (`id`, `user_id`, `title`, `cover`, `summary`, `is_shared`, `share_id`, `created_at`, `updated_at`) VALUES
(1704500000000000, 1, 'Notion-Lite 开发笔记', '', '这是关于 Notion-Lite 项目的开发笔记，记录了整个开发过程中的思考和遇到的问题。', 1, 'share-page-001', '2024-01-06 10:00:00.000', '2024-01-06 10:00:00.000'),
(1704586400000000, 1, 'Go 语言学习笔记', '', 'Go 语言是一门简洁高效的编程语言，特别适合构建微服务和分布式系统。', 0, '', '2024-01-07 10:00:00.000', '2024-01-07 10:00:00.000'),
(1704672800000000, 1, 'Vue 3 最佳实践', '', 'Vue 3 引入了 Composition API，提供了更好的代码组织和复用能力。', 1, 'share-page-002', '2024-01-08 10:00:00.000', '2024-01-08 10:00:00.000'),
(1704759200000000, 2, '测试页面', '', '这是第二个用户创建的测试页面', 0, '', '2024-01-09 10:00:00.000', '2024-01-09 10:00:00.000');

-- 插入 Blocks（Editor.js 格式的块数据）
-- Page 1: Notion-Lite 开发笔记
INSERT INTO `blocks` (`id`, `page_id`, `type`, `data`, `sort_order`, `created_at`) VALUES
('block-001', 1704500000000000, 'header', '{"text": "Notion-Lite 开发笔记", "level": 1}', 0, '2024-01-06 10:00:00.000'),
('block-002', 1704500000000000, 'paragraph', '{"text": "这是关于 Notion-Lite 项目的开发笔记，记录了整个开发过程中的思考和遇到的问题。"}', 1, '2024-01-06 10:00:00.000'),
('block-003', 1704500000000000, 'header', '{"text": "项目架构", "level": 2}', 2, '2024-01-06 10:00:00.000'),
('block-004', 1704500000000000, 'paragraph', '{"text": "项目采用 DDD 分层架构，分为 Domain、Application、Infrastructure 和 Interface 四层。"}', 3, '2024-01-06 10:00:00.000'),
('block-005', 1704500000000000, 'list', '{"style": "unordered", "items": ["使用 Uber Fx 进行依赖注入", "GORM 作为 ORM 框架", "Fiber 作为 Web 框架"]}', 4, '2024-01-06 10:00:00.000');

-- Page 2: Go 语言学习笔记
INSERT INTO `blocks` (`id`, `page_id`, `type`, `data`, `sort_order`, `created_at`) VALUES
('block-006', 1704586400000000, 'header', '{"text": "Go 语言学习笔记", "level": 1}', 0, '2024-01-07 10:00:00.000'),
('block-007', 1704586400000000, 'paragraph', '{"text": "Go 语言是一门简洁高效的编程语言，特别适合构建微服务和分布式系统。"}', 1, '2024-01-07 10:00:00.000'),
('block-008', 1704586400000000, 'header', '{"text": "核心特性", "level": 2}', 2, '2024-01-07 10:00:00.000'),
('block-009', 1704586400000000, 'list', '{"style": "ordered", "items": ["并发编程（Goroutine）", "垃圾回收（GC）", "快速编译", "静态类型系统"]}', 3, '2024-01-07 10:00:00.000');

-- Page 3: Vue 3 最佳实践
INSERT INTO `blocks` (`id`, `page_id`, `type`, `data`, `sort_order`, `created_at`) VALUES
('block-010', 1704672800000000, 'header', '{"text": "Vue 3 最佳实践", "level": 1}', 0, '2024-01-08 10:00:00.000'),
('block-011', 1704672800000000, 'paragraph', '{"text": "Vue 3 引入了 Composition API，提供了更好的代码组织和复用能力。"}', 1, '2024-01-08 10:00:00.000'),
('block-012', 1704672800000000, 'header', '{"text": "Composition API 优势", "level": 2}', 2, '2024-01-08 10:00:00.000'),
('block-013', 1704672800000000, 'paragraph', '{"text": "1. 更好的逻辑复用\n2. 更灵活的组织方式\n3. 更好的 TypeScript 支持"}', 3, '2024-01-08 10:00:00.000');

-- Page 4: 测试页面
INSERT INTO `blocks` (`id`, `page_id`, `type`, `data`, `sort_order`, `created_at`) VALUES
('block-014', 1704759200000000, 'header', '{"text": "测试页面", "level": 1}', 0, '2024-01-09 10:00:00.000'),
('block-015', 1704759200000000, 'paragraph', '{"text": "这是第二个用户创建的测试页面，用于测试多用户场景。"}', 1, '2024-01-09 10:00:00.000');

-- 查询验证数据
SELECT 'Users' as table_name, COUNT(*) as count FROM users
UNION ALL
SELECT 'Memos', COUNT(*) FROM memos
UNION ALL
SELECT 'Pages', COUNT(*) FROM pages
UNION ALL
SELECT 'Blocks', COUNT(*) FROM blocks
UNION ALL
SELECT 'Sessions', COUNT(*) FROM user_sessions;

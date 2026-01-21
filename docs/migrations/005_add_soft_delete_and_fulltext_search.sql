-- 迁移脚本：添加软删除和全文检索功能
-- 执行前请确保 MySQL 版本 >= 5.7.6 (推荐 8.0)
-- 注意：ADD FULLTEXT INDEX 可能会锁表，建议在低峰期执行

USE `notion_lite`;

-- ========== 模块 A：软删除 (Soft Delete) ==========

-- 为 memos 表添加 deleted_at 字段
ALTER TABLE `memos` 
ADD COLUMN `deleted_at` DATETIME(3) DEFAULT NULL, 
ADD INDEX `idx_deleted` (`deleted_at`);

-- 为 pages 表添加 deleted_at 字段
ALTER TABLE `pages` 
ADD COLUMN `deleted_at` DATETIME(3) DEFAULT NULL, 
ADD INDEX `idx_deleted` (`deleted_at`);

-- 为 blocks 表添加 deleted_at 字段
ALTER TABLE `blocks` 
ADD COLUMN `deleted_at` DATETIME(3) DEFAULT NULL, 
ADD INDEX `idx_deleted` (`deleted_at`);

-- ========== 模块 B：全文检索 (Full-Text Search) ==========

-- 在 blocks 表增加生成列，自动提取 JSON 中的 text 字段
-- 注意: data->>"$.text" 语法仅提取 value，去除了引号
-- STORED 表示占用磁盘空间，但可以在其上创建全文索引
-- 注意：MySQL 不支持在 VIRTUAL 生成列上创建全文索引，必须使用 STORED
ALTER TABLE `blocks`
ADD COLUMN `search_content` VARCHAR(1000) 
GENERATED ALWAYS AS (JSON_UNQUOTE(JSON_EXTRACT(data, '$.text'))) STORED;

-- 对虚拟列建立全文索引 (使用 ngram 分词器支持中文)
-- 注意：MySQL 8.0+ 支持 WITH PARSER ngram，旧版本可能需要手动分词
ALTER TABLE `blocks`
ADD FULLTEXT INDEX `idx_ft_content` (`search_content`) WITH PARSER ngram;

-- 对 memos 表的 content 字段也建立全文索引
ALTER TABLE `memos`
ADD FULLTEXT INDEX `idx_ft_memo` (`content`) WITH PARSER ngram;

-- 验证索引创建
SHOW INDEX FROM `memos` WHERE Key_name = 'idx_ft_memo';
SHOW INDEX FROM `blocks` WHERE Key_name = 'idx_ft_content';

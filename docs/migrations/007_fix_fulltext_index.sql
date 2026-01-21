-- 修复脚本：确保全文索引正确创建
-- 如果迁移脚本 005 执行失败或索引丢失，可以执行此脚本修复
-- 执行前请确保 MySQL 版本 >= 8.0

USE `notion_lite`;

-- ========== 检查并创建 blocks 表的 search_content 虚拟列 ==========

-- 检查 search_content 列是否存在
SET @col_exists = (
    SELECT COUNT(*) 
    FROM INFORMATION_SCHEMA.COLUMNS 
    WHERE TABLE_SCHEMA = 'notion_lite' 
    AND TABLE_NAME = 'blocks' 
    AND COLUMN_NAME = 'search_content'
);

-- 如果列不存在，创建生成列（使用 STORED，因为全文索引不支持 VIRTUAL）
SET @sql = IF(@col_exists = 0,
    'ALTER TABLE `blocks` ADD COLUMN `search_content` VARCHAR(1000) GENERATED ALWAYS AS (JSON_UNQUOTE(JSON_EXTRACT(data, ''$.text''))) STORED',
    'SELECT ''Column search_content already exists'' AS message'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========== 检查并创建 blocks 表的全文索引 ==========

-- 首先验证 search_content 列是否存在
SELECT 
    '=== 验证 search_content 列 ===' AS info;

SELECT 
    COLUMN_NAME,
    COLUMN_TYPE,
    GENERATION_EXPRESSION
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND TABLE_NAME = 'blocks'
    AND COLUMN_NAME = 'search_content';

-- 检查索引是否存在
SET @idx_exists = (
    SELECT COUNT(*) 
    FROM INFORMATION_SCHEMA.STATISTICS 
    WHERE TABLE_SCHEMA = 'notion_lite' 
    AND TABLE_NAME = 'blocks' 
    AND INDEX_NAME = 'idx_ft_content'
);

-- 如果索引已存在，先删除（确保重新创建）
SET @sql = IF(@idx_exists > 0,
    'ALTER TABLE `blocks` DROP INDEX `idx_ft_content`',
    'SELECT ''Index idx_ft_content does not exist, will create new one...'' AS message'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- 创建全文索引（直接执行，不使用动态 SQL，以便看到错误信息）
-- 注意：如果失败，请检查：
-- 1. search_content 列是否存在
-- 2. MySQL 版本是否 >= 8.0
-- 3. ngram 解析器是否可用
-- 4. 是否有足够的权限

ALTER TABLE `blocks` 
ADD FULLTEXT INDEX `idx_ft_content` (`search_content`) WITH PARSER ngram;

-- ========== 检查并创建 memos 表的全文索引 ==========

-- 检查索引是否存在
SET @idx_exists = (
    SELECT COUNT(*) 
    FROM INFORMATION_SCHEMA.STATISTICS 
    WHERE TABLE_SCHEMA = 'notion_lite' 
    AND TABLE_NAME = 'memos' 
    AND INDEX_NAME = 'idx_ft_memo'
);

-- 如果索引不存在，创建全文索引
SET @sql = IF(@idx_exists = 0,
    'ALTER TABLE `memos` ADD FULLTEXT INDEX `idx_ft_memo` (`content`) WITH PARSER ngram',
    'SELECT ''Index idx_ft_memo already exists'' AS message'
);
PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========== 验证索引创建 ==========

-- 显示所有全文索引
SELECT 
    TABLE_NAME,
    INDEX_NAME,
    COLUMN_NAME,
    INDEX_TYPE
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND TABLE_NAME IN ('blocks', 'memos')
    AND INDEX_TYPE = 'FULLTEXT'
ORDER BY TABLE_NAME, INDEX_NAME;

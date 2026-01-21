-- 转换脚本：将 search_content 列从 VIRTUAL 改为 STORED
-- MySQL 不支持在 VIRTUAL 生成列上创建全文索引，必须使用 STORED
-- 如果之前创建的是 VIRTUAL 类型，使用此脚本转换为 STORED
--
-- 重要说明：
-- - VIRTUAL 生成列：不占磁盘空间，实时计算，但无法创建全文索引
-- - STORED 生成列：占用磁盘空间，可以创建全文索引
--
-- 使用方法：
-- 1. 如果 search_content 列不存在，直接执行 005 或 009 脚本即可
-- 2. 如果 search_content 列已存在且是 VIRTUAL 类型，执行此脚本转换
-- 3. 如果 search_content 列已存在且是 STORED 类型，不需要执行此脚本

USE `notion_lite`;

-- ========== 步骤 1: 检查当前列类型 ==========

SELECT 
    '=== 检查 search_content 列类型 ===' AS info;

SELECT 
    COLUMN_NAME,
    COLUMN_TYPE,
    EXTRA,
    GENERATION_EXPRESSION
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND TABLE_NAME = 'blocks'
    AND COLUMN_NAME = 'search_content';

-- ========== 步骤 2: 删除全文索引（如果存在） ==========

-- 先删除索引（如果存在），否则无法删除列
ALTER TABLE `blocks` DROP INDEX IF EXISTS `idx_ft_content`;

-- ========== 步骤 3: 删除旧列（如果存在） ==========

-- 删除旧列（无论是什么类型）
-- 如果列不存在，会报错但可以忽略，继续执行下一步
ALTER TABLE `blocks` DROP COLUMN IF EXISTS `search_content`;

-- 注意：MySQL 8.0.19+ 支持 IF EXISTS，旧版本需要手动检查
-- 如果报错 "Unknown column 'search_content'"，说明列不存在，可以忽略

-- ========== 步骤 4: 创建新的 STORED 生成列 ==========

-- 创建生成列（使用 STORED，而不是 VIRTUAL）
ALTER TABLE `blocks`
ADD COLUMN `search_content` VARCHAR(1000) 
GENERATED ALWAYS AS (JSON_UNQUOTE(JSON_EXTRACT(data, '$.text'))) STORED;

SELECT 'Column search_content created as STORED' AS message;

-- ========== 步骤 5: 创建全文索引 ==========

-- 现在可以在 STORED 生成列上创建全文索引
ALTER TABLE `blocks` 
ADD FULLTEXT INDEX `idx_ft_content` (`search_content`) WITH PARSER ngram;

SELECT 'Fulltext index idx_ft_content created successfully' AS message;

-- ========== 步骤 6: 验证结果 ==========

SELECT 
    '=== 验证列类型 ===' AS info;

SELECT 
    COLUMN_NAME,
    COLUMN_TYPE,
    EXTRA,
    GENERATION_EXPRESSION
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND TABLE_NAME = 'blocks'
    AND COLUMN_NAME = 'search_content';

SELECT 
    '=== 验证索引 ===' AS info;

SELECT 
    TABLE_NAME,
    INDEX_NAME,
    COLUMN_NAME,
    INDEX_TYPE
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND TABLE_NAME = 'blocks'
    AND INDEX_NAME = 'idx_ft_content';

-- ========== 步骤 7: 显示所有全文索引 ==========

SELECT 
    '=== 所有全文索引 ===' AS info;

SELECT 
    TABLE_NAME,
    INDEX_NAME,
    COLUMN_NAME,
    INDEX_TYPE
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND INDEX_TYPE = 'FULLTEXT'
ORDER BY TABLE_NAME, INDEX_NAME;

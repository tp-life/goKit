-- 修复脚本：强制创建 blocks 表的全文索引
-- 如果之前的脚本失败，使用此脚本强制修复
-- 执行前请确保 MySQL 版本 >= 8.0
-- 
-- 使用方法：
-- 1. 先执行诊断脚本 008_diagnose_fulltext_index.sql 查看当前状态
-- 2. 执行此脚本修复
-- 3. 如果仍有问题，检查错误信息并手动执行相应步骤

USE `notion_lite`;

-- ========== 步骤 1: 确保 search_content 虚拟列存在 ==========

-- 先检查列是否存在
SET @col_exists = (
    SELECT COUNT(*) 
    FROM INFORMATION_SCHEMA.COLUMNS 
    WHERE TABLE_SCHEMA = 'notion_lite' 
    AND TABLE_NAME = 'blocks' 
    AND COLUMN_NAME = 'search_content'
);

-- 如果列不存在，创建生成列（使用 STORED，因为全文索引不支持 VIRTUAL）
-- 注意：如果列已经存在，需要先删除再重新创建
-- MySQL 8.0.19+ 支持 IF EXISTS，旧版本需要手动检查

-- 先尝试删除索引（如果存在）
ALTER TABLE `blocks` DROP INDEX IF EXISTS `idx_ft_content`;

-- 删除旧列（如果存在），然后重新创建为 STORED 类型
-- 如果列不存在，会报错但可以忽略（MySQL 8.0.19+ 支持 IF EXISTS）
ALTER TABLE `blocks` DROP COLUMN IF EXISTS `search_content`;

-- 检查列是否存在（删除后重新检查）
SET @col_exists = (
    SELECT COUNT(*) 
    FROM INFORMATION_SCHEMA.COLUMNS 
    WHERE TABLE_SCHEMA = 'notion_lite' 
    AND TABLE_NAME = 'blocks' 
    AND COLUMN_NAME = 'search_content'
);

-- 如果列不存在，创建生成列（使用 STORED）
SET @sql = IF(@col_exists = 0,
    'ALTER TABLE `blocks` ADD COLUMN `search_content` VARCHAR(1000) GENERATED ALWAYS AS (JSON_UNQUOTE(JSON_EXTRACT(data, ''$.text''))) STORED',
    'SELECT ''Column search_content already exists, skipping...'' AS message'
);

PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========== 步骤 2: 验证 search_content 列是否有数据 ==========

SELECT 
    '=== 检查 search_content 列数据 ===' AS info;

SELECT 
    COUNT(*) AS total_blocks,
    COUNT(search_content) AS blocks_with_content,
    COUNT(CASE WHEN search_content IS NOT NULL AND search_content != '' THEN 1 END) AS blocks_with_non_empty_content
FROM blocks;

-- ========== 步骤 3: 删除可能存在的旧索引（如果存在） ==========

-- 检查并删除旧的全文索引（如果存在）
SET @idx_exists = (
    SELECT COUNT(*) 
    FROM INFORMATION_SCHEMA.STATISTICS 
    WHERE TABLE_SCHEMA = 'notion_lite' 
    AND TABLE_NAME = 'blocks' 
    AND INDEX_NAME = 'idx_ft_content'
);

-- 如果索引存在，先删除
SET @sql = IF(@idx_exists > 0,
    'ALTER TABLE `blocks` DROP INDEX `idx_ft_content`',
    'SELECT ''Index idx_ft_content does not exist, skipping drop...'' AS message'
);

PREPARE stmt FROM @sql;
EXECUTE stmt;
DEALLOCATE PREPARE stmt;

-- ========== 步骤 4: 创建全文索引 ==========
-- 注意：如果表中有大量数据，创建索引可能需要较长时间，请耐心等待

-- 创建全文索引（使用 STORED 生成列）
-- 注意：MySQL 不支持在 VIRTUAL 生成列上创建全文索引，必须使用 STORED

-- 方法 1: 使用 ngram 解析器（推荐，支持中文，MySQL 8.0+）
ALTER TABLE `blocks` 
ADD FULLTEXT INDEX `idx_ft_content` (`search_content`) WITH PARSER ngram;

-- 如果上面的语句失败（例如不支持 ngram），请注释掉上面的语句，然后取消注释下面的语句：
-- 方法 2: 不使用 ngram 解析器（备选方案，不支持中文分词）
-- ALTER TABLE `blocks` 
-- ADD FULLTEXT INDEX `idx_ft_content` (`search_content`);

-- ========== 步骤 5: 验证索引创建 ==========

SELECT 
    '=== 验证 blocks 表全文索引 ===' AS info;

SELECT 
    TABLE_NAME,
    INDEX_NAME,
    COLUMN_NAME,
    INDEX_TYPE,
    SEQ_IN_INDEX
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND TABLE_NAME = 'blocks'
    AND INDEX_NAME = 'idx_ft_content';

-- 如果上面的查询返回空结果，说明索引创建失败
-- 请检查：
-- 1. MySQL 版本是否 >= 8.0
-- 2. ngram 解析器是否可用
-- 3. 是否有足够的权限
-- 4. 表是否被锁定

-- ========== 步骤 6: 显示所有全文索引（用于对比） ==========

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

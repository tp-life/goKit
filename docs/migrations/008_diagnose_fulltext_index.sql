-- 诊断脚本：检查全文索引状态
-- 用于诊断为什么 blocks 表的全文索引没有创建成功

USE `notion_lite`;

-- ========== 1. 检查 blocks 表的列 ==========
SELECT 
    '=== blocks 表列信息 ===' AS info;
    
SELECT 
    COLUMN_NAME,
    COLUMN_TYPE,
    IS_NULLABLE,
    COLUMN_DEFAULT,
    EXTRA,
    GENERATION_EXPRESSION
FROM INFORMATION_SCHEMA.COLUMNS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND TABLE_NAME = 'blocks'
    AND COLUMN_NAME = 'search_content';

-- ========== 2. 检查 blocks 表的所有索引 ==========
SELECT 
    '=== blocks 表索引信息 ===' AS info;
    
SELECT 
    TABLE_NAME,
    INDEX_NAME,
    COLUMN_NAME,
    INDEX_TYPE,
    NON_UNIQUE
FROM INFORMATION_SCHEMA.STATISTICS
WHERE TABLE_SCHEMA = 'notion_lite'
    AND TABLE_NAME = 'blocks'
ORDER BY INDEX_NAME, SEQ_IN_INDEX;

-- ========== 3. 检查全文索引 ==========
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

-- ========== 4. 检查 MySQL 版本和 ngram 支持 ==========
SELECT 
    '=== MySQL 版本信息 ===' AS info;
    
SELECT 
    VERSION() AS mysql_version,
    @@innodb_ft_min_token_size AS innodb_ft_min_token_size,
    @@innodb_ft_server_stopword_table AS innodb_ft_server_stopword_table;

-- ========== 5. 测试虚拟列数据 ==========
SELECT 
    '=== 测试 search_content 虚拟列（前5条） ===' AS info;
    
SELECT 
    id,
    type,
    search_content,
    LENGTH(search_content) AS content_length
FROM blocks
WHERE search_content IS NOT NULL
    AND search_content != ''
LIMIT 5;

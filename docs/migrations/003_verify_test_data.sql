-- 验证测试数据的 SQL 查询脚本

USE `notion_lite`;

-- 查看所有用户
SELECT '=== 用户列表 ===' as info;
SELECT id, email, created_at FROM users;

-- 查看所有 Memos
SELECT '=== Memos 列表 ===' as info;
SELECT id, user_id, LEFT(content, 50) as content_preview, source, created_at 
FROM memos 
ORDER BY created_at DESC;

-- 查看所有 Pages
SELECT '=== Pages 列表 ===' as info;
SELECT id, user_id, title, is_shared, share_id, created_at 
FROM pages 
ORDER BY created_at DESC;

-- 查看某个 Page 的所有 Blocks
SELECT '=== Page 1 的 Blocks ===' as info;
SELECT id, type, data, sort_order 
FROM blocks 
WHERE page_id = 1704500000000000 
ORDER BY sort_order;

-- 统计信息
SELECT '=== 数据统计 ===' as info;
SELECT 
    (SELECT COUNT(*) FROM users) as users_count,
    (SELECT COUNT(*) FROM memos) as memos_count,
    (SELECT COUNT(*) FROM pages) as pages_count,
    (SELECT COUNT(*) FROM blocks) as blocks_count,
    (SELECT COUNT(*) FROM user_sessions) as sessions_count;

-- 查看分享的页面
SELECT '=== 已分享的页面 ===' as info;
SELECT id, title, share_id, 
       CONCAT('http://localhost:3000/s/', share_id) as share_url
FROM pages 
WHERE is_shared = 1;

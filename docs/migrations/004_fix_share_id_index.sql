-- 修复 share_id 唯一索引问题
-- 问题：空字符串 '' 在 UNIQUE 约束下会被视为重复值
-- 解决：将 share_id 改为允许 NULL，并只在非 NULL 时唯一

USE `notion_lite`;

-- 删除旧的索引
ALTER TABLE `pages` DROP INDEX `idx_share`;

-- 修改字段：允许 NULL，移除默认空字符串
ALTER TABLE `pages` 
  MODIFY COLUMN `share_id` VARCHAR(64) DEFAULT NULL COMMENT '公开访问UUID';

-- 将现有的空字符串改为 NULL
UPDATE `pages` SET `share_id` = NULL WHERE `share_id` = '';

-- 创建新的唯一索引（NULL 值不会冲突）
ALTER TABLE `pages` 
  ADD UNIQUE INDEX `idx_share` (`share_id`);

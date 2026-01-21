-- 迁移脚本：添加标签系统
-- 执行前请确保 MySQL 版本 >= 5.7

USE `notion_lite`;

-- ========== 标签系统表 ==========

-- 1. 标签主表 (所有标签都在这里)
CREATE TABLE IF NOT EXISTS `tags` (
  `id` BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  `user_id` BIGINT UNSIGNED NOT NULL,
  `name` VARCHAR(50) NOT NULL,
  `created_at` DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
  -- 保证同一个用户下标签名不重复
  UNIQUE KEY `uk_user_tag` (`user_id`, `name`),
  INDEX `idx_user_id` (`user_id`),
  FOREIGN KEY (`user_id`) REFERENCES `users`(`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 2. Memo 关联表 (Many-to-Many)
CREATE TABLE IF NOT EXISTS `memo_tags` (
  `memo_id` BIGINT UNSIGNED NOT NULL,
  `tag_id` BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (`memo_id`, `tag_id`),
  INDEX `idx_tag_memo` (`tag_id`),
  FOREIGN KEY (`memo_id`) REFERENCES `memos`(`id`) ON DELETE CASCADE,
  FOREIGN KEY (`tag_id`) REFERENCES `tags`(`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 3. Page 关联表 (Many-to-Many)
CREATE TABLE IF NOT EXISTS `page_tags` (
  `page_id` BIGINT UNSIGNED NOT NULL,
  `tag_id` BIGINT UNSIGNED NOT NULL,
  PRIMARY KEY (`page_id`, `tag_id`),
  INDEX `idx_tag_page` (`tag_id`),
  FOREIGN KEY (`page_id`) REFERENCES `pages`(`id`) ON DELETE CASCADE,
  FOREIGN KEY (`tag_id`) REFERENCES `tags`(`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Notion-Lite 数据库初始化脚本
-- 执行前请确保 MySQL 版本 >= 8.0（需要 JSON 字段支持）

CREATE DATABASE IF NOT EXISTS `notion_lite` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

USE `notion_lite`;

-- 用户表
CREATE TABLE IF NOT EXISTS `users` (
  `id` BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  `email` VARCHAR(128) NOT NULL UNIQUE,
  `password_hash` VARCHAR(255) NOT NULL COMMENT 'Bcrypt 加密后的密码',
  `salt` VARCHAR(32) DEFAULT '' COMMENT '预留字段，Bcrypt 不需要额外 salt',
  `created_at` DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
  INDEX `idx_email` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 用户会话表（Refresh Token）
CREATE TABLE IF NOT EXISTS `user_sessions` (
  `id` BIGINT UNSIGNED PRIMARY KEY AUTO_INCREMENT,
  `user_id` BIGINT UNSIGNED NOT NULL,
  `refresh_token` VARCHAR(255) NOT NULL UNIQUE COMMENT '30天长效Token',
  `device_info` VARCHAR(255) DEFAULT '' COMMENT '设备信息',
  `expires_at` DATETIME(3) NOT NULL,
  `created_at` DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
  INDEX `idx_refresh` (`refresh_token`),
  INDEX `idx_user_id` (`user_id`),
  FOREIGN KEY (`user_id`) REFERENCES `users`(`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 移动端闪念表（唯快不破）
CREATE TABLE IF NOT EXISTS `memos` (
  `id` BIGINT UNSIGNED PRIMARY KEY,
  `user_id` BIGINT UNSIGNED NOT NULL,
  `content` TEXT NOT NULL COMMENT '纯文本内容',
  `images` JSON COMMENT '图片URL数组 ["url1", "url2"]',
  `source` VARCHAR(20) DEFAULT 'mobile' COMMENT '来源：mobile, web',
  `created_at` DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
  INDEX `idx_user_time` (`user_id`, `created_at` DESC),
  FOREIGN KEY (`user_id`) REFERENCES `users`(`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- PC端页面表（容器）
CREATE TABLE IF NOT EXISTS `pages` (
  `id` BIGINT UNSIGNED PRIMARY KEY,
  `user_id` BIGINT UNSIGNED NOT NULL,
  `title` VARCHAR(255) DEFAULT '' COMMENT '页面标题',
  `cover` VARCHAR(255) DEFAULT '' COMMENT '封面图片URL',
  `summary` VARCHAR(500) DEFAULT '' COMMENT '冗余字段: 存前100字摘要，优化列表加载',
  `is_shared` TINYINT(1) DEFAULT 0 COMMENT '分享开关',
  `share_id` VARCHAR(64) DEFAULT NULL COMMENT '公开访问UUID',
  `created_at` DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
  `updated_at` DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
  UNIQUE INDEX `idx_share` (`share_id`),
  INDEX `idx_user_id` (`user_id`),
  FOREIGN KEY (`user_id`) REFERENCES `users`(`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 块数据表（适配 Editor.js）
CREATE TABLE IF NOT EXISTS `blocks` (
  `id` VARCHAR(64) PRIMARY KEY COMMENT 'Editor.js 生成的 Block ID',
  `page_id` BIGINT UNSIGNED NOT NULL,
  `type` VARCHAR(50) NOT NULL COMMENT 'paragraph, header, list, image 等',
  `data` JSON NOT NULL COMMENT 'Editor.js Block 数据 {"text": "...", "level": 1}',
  `sort_order` INT UNSIGNED NOT NULL COMMENT '排序顺序',
  `created_at` DATETIME(3) DEFAULT CURRENT_TIMESTAMP(3),
  INDEX `idx_page_sort` (`page_id`, `sort_order`),
  FOREIGN KEY (`page_id`) REFERENCES `pages`(`id`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

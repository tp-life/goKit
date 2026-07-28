-- 初始表结构：与 internal/domain/{user,role,dept,menu} 的 gorm tag 严格一致
-- MySQL 8 / InnoDB / utf8mb4

CREATE TABLE IF NOT EXISTS `sys_users` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `username`   VARCHAR(64)     NOT NULL,
    `password`   VARCHAR(128)    NOT NULL COMMENT 'bcrypt 哈希',
    `nickname`   VARCHAR(64)     NULL,
    `email`      VARCHAR(128)    NULL,
    `dept_id`    BIGINT UNSIGNED NULL,
    `status`     TINYINT         NULL DEFAULT 1 COMMENT '1 正常 0 停用',
    `is_super`   TINYINT(1)      NULL DEFAULT 0,
    `created_at` DATETIME(3)     NULL,
    `updated_at` DATETIME(3)     NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_sys_users_username` (`username`),
    KEY `idx_sys_users_dept_id` (`dept_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `sys_roles` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`       VARCHAR(64)     NOT NULL,
    `code`       VARCHAR(64)     NOT NULL,
    `data_scope` TINYINT         NULL DEFAULT 1 COMMENT '数据权限范围 1全部 2自定义 3本部门及以下 4本部门 5仅本人',
    `status`     TINYINT         NULL DEFAULT 1 COMMENT '1 正常 0 停用',
    `remark`     VARCHAR(255)    NULL,
    `created_at` DATETIME(3)     NULL,
    `updated_at` DATETIME(3)     NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `idx_sys_roles_code` (`code`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `sys_depts` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `parent_id`  BIGINT UNSIGNED NULL DEFAULT 0,
    `name`       VARCHAR(64)     NOT NULL,
    `sort`       BIGINT          NULL DEFAULT 0,
    `status`     TINYINT         NULL DEFAULT 1,
    `created_at` DATETIME(3)     NULL,
    `updated_at` DATETIME(3)     NULL,
    PRIMARY KEY (`id`),
    KEY `idx_sys_depts_parent_id` (`parent_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `sys_menus` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `parent_id`  BIGINT UNSIGNED NULL DEFAULT 0,
    `title`      VARCHAR(64)     NOT NULL,
    `type`       TINYINT         NULL DEFAULT 2 COMMENT '1 目录 2 菜单 3 按钮/权限点',
    `path`       VARCHAR(128)    NULL,
    `perm_code`  VARCHAR(128)    NULL COMMENT '如 system:user:list，仅按钮/权限点需要',
    `sort`       BIGINT          NULL DEFAULT 0,
    `status`     TINYINT         NULL DEFAULT 1,
    `created_at` DATETIME(3)     NULL,
    `updated_at` DATETIME(3)     NULL,
    PRIMARY KEY (`id`),
    KEY `idx_sys_menus_parent_id` (`parent_id`),
    KEY `idx_sys_menus_perm_code` (`perm_code`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `sys_user_roles` (
    `user_id` BIGINT UNSIGNED NOT NULL,
    `role_id` BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (`user_id`, `role_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `sys_role_menus` (
    `role_id` BIGINT UNSIGNED NOT NULL,
    `menu_id` BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (`role_id`, `menu_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

CREATE TABLE IF NOT EXISTS `sys_role_depts` (
    `role_id` BIGINT UNSIGNED NOT NULL,
    `dept_id` BIGINT UNSIGNED NOT NULL,
    PRIMARY KEY (`role_id`, `dept_id`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

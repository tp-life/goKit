-- 操作日志表：对应 internal/domain/oplog.OperationLog

CREATE TABLE IF NOT EXISTS `sys_operation_logs` (
    `id`         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `user_id`    BIGINT UNSIGNED NULL COMMENT '操作人 ID（未登录为 0）',
    `username`   VARCHAR(64)     NULL COMMENT '操作人账号',
    `method`     VARCHAR(10)     NULL COMMENT 'HTTP 方法',
    `path`       VARCHAR(256)    NULL COMMENT '路由模板，如 /api/v1/users/:id',
    `ip`         VARCHAR(64)     NULL COMMENT '客户端 IP',
    `status`     INT             NULL COMMENT 'HTTP 状态码',
    `latency_ms` BIGINT          NULL COMMENT '耗时（毫秒）',
    `created_at` DATETIME(3)     NULL,
    PRIMARY KEY (`id`),
    KEY `idx_sys_operation_logs_user_id` (`user_id`),
    KEY `idx_sys_operation_logs_path` (`path`),
    KEY `idx_sys_operation_logs_status` (`status`)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_general_ci;

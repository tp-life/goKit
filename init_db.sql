-- ========================================================
-- GoKit 量化投研系统 - 数据库初始化脚本
-- 数据库: MySQL 5.7+ / 8.0+
-- 字符集: utf8mb4 (支持中文字符和 Emoji)
-- ========================================================

-- 1. 创建数据库 (如果不存在)
CREATE DATABASE IF NOT EXISTS `stocks` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci;
USE `stocks`;

-- ========================================================
-- 表 1: 股票维度表 (stock_info)
-- 作用: 存储 A 股上市公司的静态/半静态基础信息
-- ========================================================
DROP TABLE IF EXISTS `stock_info`;
CREATE TABLE `stock_info` (
  `stock_code` varchar(16) NOT NULL COMMENT '股票代码 (如 600519)',
  `stock_name` varchar(64) NOT NULL COMMENT '股票简称',
  `industry` varchar(64) DEFAULT NULL COMMENT '申万一级/二级行业',
  `exchange` varchar(16) DEFAULT NULL COMMENT '交易所 (SH, SZ, BJ)',
  `list_date` date DEFAULT NULL COMMENT '上市日期',
  `status` tinyint(4) NOT NULL DEFAULT '1' COMMENT '状态: 1-正常, 0-退市/ST',
  `created_at` datetime(3) DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  `updated_at` datetime(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  PRIMARY KEY (`stock_code`),
  KEY `idx_industry` (`industry`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='股票维度表';


-- ========================================================
-- 表 2: 机构/股东维度表 (institution_info)
-- 作用: 存储持仓机构的基础信息，并进行标签化打标
-- ========================================================
DROP TABLE IF EXISTS `institution_info`;
CREATE TABLE `institution_info` (
  `inst_id` bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '机构内部自增ID',
  `inst_name` varchar(128) NOT NULL COMMENT '机构全称 (如: 中央汇金...)',
  `inst_type` varchar(32) DEFAULT NULL COMMENT '机构大类 (国家队/社保/险资/公募等)',
  `tags` json DEFAULT NULL COMMENT '扩展标签 (预留 JSON 格式)',
  `created_at` datetime(3) DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  `updated_at` datetime(3) DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT '更新时间',
  PRIMARY KEY (`inst_id`),
  UNIQUE KEY `uk_inst_name` (`inst_name`),
  KEY `idx_inst_type` (`inst_type`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='机构股东维度表';


-- ========================================================
-- 表 3: 持仓明细事实表 (stock_holding_records)
-- 作用: 记录特定财报期内，某机构对某股票的持仓情况
-- ========================================================
DROP TABLE IF EXISTS `stock_holding_records`;
CREATE TABLE `stock_holding_records` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '自增主键',
  `stock_code` varchar(16) NOT NULL COMMENT '关联 stock_info.stock_code',
  `inst_id` bigint(20) unsigned NOT NULL COMMENT '关联 institution_info.inst_id',
  `report_date` date NOT NULL COMMENT '财报报告期 (如 2023-09-30)',
  `hold_count` bigint(20) NOT NULL COMMENT '持股数量 (股)',
  `hold_ratio` decimal(10,4) NOT NULL COMMENT '占总流通股本比例 (%)',
  `change_type` varchar(16) NOT NULL COMMENT '变动类型 (新进/增持/减持/不变)',
  `change_count` bigint(20) NOT NULL DEFAULT '0' COMMENT '较上期变动数量 (股)',
  `created_at` datetime(3) DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  PRIMARY KEY (`id`),
  -- 【核心防重索引】确保同一股票、同一机构、同一报告期只有一条记录
  UNIQUE KEY `uk_holding` (`stock_code`,`inst_id`,`report_date`),
  -- 方便根据财报期和机构类型快速筛选的联合索引
  KEY `idx_report_inst` (`report_date`,`inst_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='持仓明细事实表';


-- ========================================================
-- 表 4: 股票日线行情表 (stock_daily_quotes)
-- 作用: 记录股票的每日开高低收，用于计算持仓成本和黄金坑策略
-- ========================================================
DROP TABLE IF EXISTS `stock_daily_quotes`;
CREATE TABLE `stock_daily_quotes` (
  `id` bigint(20) unsigned NOT NULL AUTO_INCREMENT COMMENT '自增主键',
  `stock_code` varchar(16) NOT NULL COMMENT '股票代码',
  `trade_date` date NOT NULL COMMENT '交易日期',
  `open` decimal(10,3) DEFAULT NULL COMMENT '开盘价',
  `close` decimal(10,3) DEFAULT NULL COMMENT '收盘价',
  `high` decimal(10,3) DEFAULT NULL COMMENT '最高价',
  `low` decimal(10,3) DEFAULT NULL COMMENT '最低价',
  `volume` bigint(20) NOT NULL COMMENT '成交量 (手)',
  `turnover_rate` decimal(10,4) DEFAULT NULL COMMENT '换手率 (%)',
  `created_at` datetime(3) DEFAULT CURRENT_TIMESTAMP(3) COMMENT '创建时间',
  PRIMARY KEY (`id`),
  -- 【核心防重索引】确保同一股票同一天只有一条 K 线记录
  UNIQUE KEY `uk_daily_quote` (`stock_code`,`trade_date`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='股票日线行情表';

-- 初始化完成提示
SELECT 'GoKit 量化数据库 (GoKit_db) 及 4 张核心数据表初始化成功！' AS 'Success Message';

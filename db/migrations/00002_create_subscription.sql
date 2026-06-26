-- +goose NO TRANSACTION

-- +goose Up
CREATE TABLE IF NOT EXISTS `report_subscription` (
    `id`          INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `report_id`   INT UNSIGNED NOT NULL COMMENT '所属报表 id',
    `name`        VARCHAR(128) NOT NULL DEFAULT '' COMMENT '订阅名称',
    `cron`        VARCHAR(64)  NOT NULL COMMENT 'cron 表达式 (标准 5 段, 不含秒)',
    `channel`     VARCHAR(16)  NOT NULL DEFAULT 'lark' COMMENT '渠道: lark 飞书 / wework 企业微信',
    `webhook`     VARCHAR(512) NOT NULL DEFAULT '' COMMENT '群机器人 webhook 完整地址',
    `params`      TEXT         NULL COMMENT '固定过滤参数 JSON, 决定订阅跑哪份数据',
    `enabled`     TINYINT(1)   NOT NULL DEFAULT 1 COMMENT '是否启用',
    `last_run_at` DATETIME     NULL COMMENT '上次触发的整分钟',
    `last_status` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '上次执行结果 (ok / 错误信息)',
    `created_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_report` (`report_id`),
    KEY `idx_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='报表定时订阅推送';

-- +goose Down
DROP TABLE IF EXISTS `report_subscription`;

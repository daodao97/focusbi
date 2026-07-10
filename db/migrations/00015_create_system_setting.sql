-- +goose NO TRANSACTION

-- +goose Up
CREATE TABLE IF NOT EXISTS `system_setting` (
    `name`       VARCHAR(128) NOT NULL COMMENT '配置键, 如 engine.script_fetch',
    `value`      TEXT         NULL COMMENT '配置值',
    `updated_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='可动态调整的系统运行配置';

-- +goose Down
DROP TABLE IF EXISTS `system_setting`;

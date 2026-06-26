-- +goose NO TRANSACTION

-- +goose Up
CREATE TABLE IF NOT EXISTS `report_version` (
    `id`         INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `report_id`  INT UNSIGNED NOT NULL COMMENT '所属报表 id',
    `content`    MEDIUMTEXT   NULL COMMENT '该次发布的内容快照',
    `user_id`    INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '发布操作人 id',
    `user_nick`  VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '发布操作人昵称 (冗余, 便于展示)',
    `remark`     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '版本备注 (预留)',
    `created_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_report` (`report_id`, `id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='报表发布版本历史';

-- +goose Down
DROP TABLE IF EXISTS `report_version`;

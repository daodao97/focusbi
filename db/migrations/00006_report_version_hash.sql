-- +goose NO TRANSACTION

-- +goose Up
ALTER TABLE `report_version`
    ADD COLUMN `content_hash` CHAR(64) NOT NULL DEFAULT '' COMMENT '内容 sha256, 用于版本去重';
-- 回填旧数据: 用 SHA2 计算已存内容的 hash (MySQL 内置)
UPDATE `report_version` SET `content_hash` = SHA2(COALESCE(`content`, ''), 256) WHERE `content_hash` = '';
ALTER TABLE `report_version` ADD KEY `idx_report_hash` (`report_id`, `content_hash`);

-- +goose Down
ALTER TABLE `report_version` DROP KEY `idx_report_hash`;
ALTER TABLE `report_version` DROP COLUMN `content_hash`;

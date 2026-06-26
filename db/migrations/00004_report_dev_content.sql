-- +goose NO TRANSACTION

-- +goose Up
ALTER TABLE `report`
    ADD COLUMN `dev_content` MEDIUMTEXT NULL COMMENT '开发版草稿; 发布后同步到 content';
-- 旧数据: 草稿初始化为当前线上内容, 保持行为不变
UPDATE `report` SET `dev_content` = `content` WHERE `dev_content` IS NULL;

-- +goose Down
ALTER TABLE `report` DROP COLUMN `dev_content`;

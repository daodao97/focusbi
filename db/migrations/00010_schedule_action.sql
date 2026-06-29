-- +goose NO TRANSACTION

-- 定时任务的"动作": 推送只是其中一种。none=只跑报表不推送 (刷缓存/预热),
-- webhook=跑完推群机器人。默认 webhook 以兼容现有任务。后续可扩展 mail / 落库等。

-- +goose Up
ALTER TABLE `report_schedule`
    ADD COLUMN `action` VARCHAR(16) NOT NULL DEFAULT 'webhook' COMMENT '动作: none 只跑不推 / webhook 推群机器人';

-- +goose Down
ALTER TABLE `report_schedule` DROP COLUMN `action`;

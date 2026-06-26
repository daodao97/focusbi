-- +goose NO TRANSACTION

-- +goose Up
ALTER TABLE `report_subscription`
    ADD COLUMN `condition` TEXT NULL COMMENT '触发条件 JSON (列+聚合+操作符+值); 空=定时推送, 非空=命中才推';

-- +goose Down
ALTER TABLE `report_subscription` DROP COLUMN `condition`;

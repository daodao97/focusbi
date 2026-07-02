-- +goose NO TRANSACTION

-- 阈值告警静默期: 记录上次告警推送时间, 配合 trigger_cond JSON 里的 silence_minutes,
-- 命中后静默期内不再重复推送, 防止每分钟 cron + 持续命中造成的告警风暴。

-- +goose Up
ALTER TABLE `report_schedule`
    ADD COLUMN `last_alarm_at` DATETIME NULL COMMENT '上次告警推送时间 (静默期判断用)';

-- +goose Down
ALTER TABLE `report_schedule` DROP COLUMN `last_alarm_at`;

-- +goose NO TRANSACTION

-- +goose Up
-- 清理历史孤儿记录后建立数据库级级联约束。
DELETE s FROM `report_schedule` s LEFT JOIN `report` r ON r.id = s.report_id WHERE r.id IS NULL;
DELETE v FROM `report_version` v LEFT JOIN `report` r ON r.id = v.report_id WHERE r.id IS NULL;

ALTER TABLE `report_schedule`
    ADD CONSTRAINT `fk_schedule_report` FOREIGN KEY (`report_id`) REFERENCES `report` (`id`) ON DELETE CASCADE;
ALTER TABLE `report_version`
    ADD CONSTRAINT `fk_version_report` FOREIGN KEY (`report_id`) REFERENCES `report` (`id`) ON DELETE CASCADE;

-- +goose Down
ALTER TABLE `report_version` DROP FOREIGN KEY `fk_version_report`;
ALTER TABLE `report_schedule` DROP FOREIGN KEY `fk_schedule_report`;

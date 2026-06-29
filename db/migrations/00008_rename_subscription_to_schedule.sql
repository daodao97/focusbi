-- +goose NO TRANSACTION

-- 订阅本质是定时任务 (cron job: 到点跑报表, 推送只是其动作之一), 改名以贴合本质。
-- 仅重命名表, 保留全部数据与列结构。

-- +goose Up
RENAME TABLE `report_subscription` TO `report_schedule`;

-- +goose Down
RENAME TABLE `report_schedule` TO `report_subscription`;

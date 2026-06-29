-- +goose NO TRANSACTION

-- condition 是 MySQL 保留字, 做列名需处处加反引号, 换个 ORM/手写查询极易踩坑。
-- 改名为 trigger_cond 根治 (本服务无存量数据需兼容)。

-- +goose Up
ALTER TABLE `report_schedule` CHANGE `condition` `trigger_cond` TEXT NULL COMMENT '触发条件 JSON (列+聚合+操作符+值); 空=定时推送, 非空=命中才推';

-- +goose Down
ALTER TABLE `report_schedule` CHANGE `trigger_cond` `condition` TEXT NULL COMMENT '触发条件 JSON (列+聚合+操作符+值); 空=定时推送, 非空=命中才推';

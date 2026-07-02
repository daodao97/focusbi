-- +goose NO TRANSACTION

-- 报表权限从"两维度"合并为"单一维度": 去掉全局 report.manage 开关。
-- 历史上 report.manage:rw 单独就代表"可写所有报表" (写路由只校验它), 故迁移时:
--   1. 凡 report.manage 含写权限的角色, 把 report 提升到 Rrw (递归可读写所有报表),
--      保住其原有的全站写能力;
--   2. 删除已废弃的 report.manage 键 (dot 在 JSON path 里须加引号: $."report.manage")。

-- +goose Up
UPDATE `role`
SET `resource` = JSON_SET(`resource`, '$.report', 'Rrw')
WHERE `resource` IS NOT NULL
  AND JSON_VALID(`resource`)
  AND JSON_CONTAINS_PATH(`resource`, 'one', '$."report.manage"')
  AND JSON_UNQUOTE(JSON_EXTRACT(`resource`, '$."report.manage"')) LIKE '%w%';

UPDATE `role`
SET `resource` = JSON_REMOVE(`resource`, '$."report.manage"')
WHERE `resource` IS NOT NULL
  AND JSON_VALID(`resource`)
  AND JSON_CONTAINS_PATH(`resource`, 'one', '$."report.manage"');

-- +goose Down
-- 尽力还原: 对拥有 report 递归写的角色重新补上 report.manage:rw (无法精确复原被合并前的原值)。
UPDATE `role`
SET `resource` = JSON_SET(`resource`, '$."report.manage"', 'rw')
WHERE `resource` IS NOT NULL
  AND JSON_VALID(`resource`)
  AND JSON_CONTAINS_PATH(`resource`, 'one', '$.report')
  AND JSON_UNQUOTE(JSON_EXTRACT(`resource`, '$.report')) LIKE '%w%';

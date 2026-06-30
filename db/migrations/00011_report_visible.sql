-- +goose NO TRANSACTION

-- 报表是否在侧边菜单可见。默认 1 (可见); 0 时不出现在左侧菜单, 仍可在"全部报表"中管理与直接访问。

-- +goose Up
ALTER TABLE `report`
    ADD COLUMN `visible` TINYINT(1) NOT NULL DEFAULT 1 COMMENT '是否在侧边菜单可见: 1 可见 / 0 隐藏';

-- +goose Down
ALTER TABLE `report` DROP COLUMN `visible`;

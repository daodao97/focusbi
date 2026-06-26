-- FocusBI 数据库 schema
-- 在报表系统使用的主库 (conf.database[default]) 中执行

-- 数据源定义: 系统内可定义多个 dsn, 报表通过 -- @dsn=name 选择
CREATE TABLE IF NOT EXISTS `dsn` (
    `id`         INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`       VARCHAR(64)  NOT NULL COMMENT '数据源名称, 报表中通过 @dsn 引用',
    `driver`     VARCHAR(32)  NOT NULL DEFAULT 'mysql' COMMENT '驱动: mysql / postgres / ...',
    `dsn`        TEXT         NOT NULL COMMENT '连接串, 形如 user:pass@tcp(host:port)/db',
    `remark`     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '备注',
    -- SSH 隧道 (仅 mysql 驱动支持): 启用后数据库连接经由 ssh 主机转发
    `ssh_enabled`  TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '是否启用 ssh 隧道',
    `ssh_host`     VARCHAR(128) NOT NULL DEFAULT '' COMMENT 'ssh 主机',
    `ssh_port`     INT          NOT NULL DEFAULT 22 COMMENT 'ssh 端口',
    `ssh_user`     VARCHAR(64)  NOT NULL DEFAULT '' COMMENT 'ssh 用户',
    `ssh_auth`     VARCHAR(16)  NOT NULL DEFAULT 'password' COMMENT 'ssh 认证方式: password / key',
    `ssh_password` VARCHAR(255) NOT NULL DEFAULT '' COMMENT 'ssh 密码 (auth=password)',
    `ssh_key`      TEXT         NULL COMMENT 'ssh 私钥 PEM (auth=key)',
    `ssh_key_passphrase` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '私钥口令 (可选)',
    `created_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='数据源';

-- 报表模板: content 为 dataddy 风格的报表模板
CREATE TABLE IF NOT EXISTS `report` (
    `id`          INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`        VARCHAR(128) NOT NULL COMMENT '报表名称',
    `parent_id`   INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '父节点 (文件夹) id, 0 为根',
    `type`        VARCHAR(16)  NOT NULL DEFAULT 'report' COMMENT 'report 报表 / folder 文件夹',
    `sort`        INT          NOT NULL DEFAULT 0 COMMENT '同级排序',
    `dsn`         VARCHAR(64)  NOT NULL DEFAULT 'default' COMMENT '默认数据源',
    `content`     MEDIUMTEXT   NULL COMMENT '发布版内容 (查看者/run/订阅看的); 文件夹为空',
    `dev_content` MEDIUMTEXT   NULL COMMENT '开发版草稿; 发布后同步到 content',
    `settings`    TEXT         NULL COMMENT '页面级配置 JSON',
    `remark`      VARCHAR(255) NOT NULL DEFAULT '',
    `is_public`   TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '是否开启公开分享',
    `share_token` VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '公开访问令牌 (不可枚举)',
    `created_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_share_token` (`share_token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='报表模板';

-- 报表定时订阅: 到点跑报表并把结果推送到飞书/企业微信群机器人
CREATE TABLE IF NOT EXISTS `report_subscription` (
    `id`          INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `report_id`   INT UNSIGNED NOT NULL COMMENT '所属报表 id',
    `name`        VARCHAR(128) NOT NULL DEFAULT '' COMMENT '订阅名称',
    `cron`        VARCHAR(64)  NOT NULL COMMENT 'cron 表达式 (标准 5 段, 不含秒)',
    `channel`     VARCHAR(16)  NOT NULL DEFAULT 'lark' COMMENT '渠道: lark 飞书 / wework 企业微信',
    `webhook`     VARCHAR(512) NOT NULL DEFAULT '' COMMENT '群机器人 webhook 完整地址',
    `params`      TEXT         NULL COMMENT '固定过滤参数 JSON',
    `condition`   TEXT         NULL COMMENT '触发条件 JSON; 空=定时推送, 非空=命中才推',
    `enabled`     TINYINT(1)   NOT NULL DEFAULT 1 COMMENT '是否启用',
    `last_run_at` DATETIME     NULL COMMENT '上次触发的整分钟',
    `last_status` VARCHAR(255) NOT NULL DEFAULT '' COMMENT '上次执行结果',
    `created_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_report` (`report_id`),
    KEY `idx_enabled` (`enabled`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='报表定时订阅推送';

-- 报表发布版本历史: 每次发布记一条 content 快照, 可回滚 (每报表保留最近 N 条)
CREATE TABLE IF NOT EXISTS `report_version` (
    `id`         INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `report_id`    INT UNSIGNED NOT NULL COMMENT '所属报表 id',
    `content`      MEDIUMTEXT   NULL COMMENT '该次发布的内容快照',
    `content_hash` CHAR(64)     NOT NULL DEFAULT '' COMMENT '内容 sha256, 用于版本去重',
    `user_id`      INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '发布操作人 id',
    `user_nick`    VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '发布操作人昵称 (冗余)',
    `remark`       VARCHAR(255) NOT NULL DEFAULT '' COMMENT '版本备注 (预留)',
    `created_at`   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_report` (`report_id`, `id`),
    KEY `idx_report_hash` (`report_id`, `content_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='报表发布版本历史';

-- ----------------------------------------------------------------------------
-- 用户体系 (RBAC): user + role。首位注册用户自动成为超管 (is_admin=1)。
-- ----------------------------------------------------------------------------

-- 后台账号
CREATE TABLE IF NOT EXISTS `user` (
    `id`              INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `username`        VARCHAR(64)  NOT NULL COMMENT '登录名',
    `password`        VARCHAR(255) NOT NULL COMMENT 'bcrypt 哈希',
    `nick`            VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '昵称',
    `roles`           VARCHAR(255) NOT NULL DEFAULT '' COMMENT '角色 id 列表, 逗号分隔',
    `is_admin`        TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '超级管理员, 全权限',
    `email`           VARCHAR(128) NOT NULL DEFAULT '',
    `avatar`          VARCHAR(255) NOT NULL DEFAULT '',
    `created_at`      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`      DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_username` (`username`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='后台用户';

-- 角色: resource 为 JSON 权限定义, 形如 {"report":"Rr","report.5":"rw","dsn":"r"}
--   值由 R(递归) / r(读) / w(写) 组合; 资源串支持点分段树与 * 通配。
CREATE TABLE IF NOT EXISTS `role` (
    `id`         INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`       VARCHAR(64)  NOT NULL COMMENT '角色名',
    `parent_id`  INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '父角色, 子角色继承父权限',
    `resource`   TEXT         NULL COMMENT '资源权限 JSON',
    `remark`     VARCHAR(255) NOT NULL DEFAULT '',
    `created_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at` DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_name` (`name`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='角色';

-- ----------------------------------------------------------------------------
-- 示例数据 (demo): 销售表 + 销售日报模板。仅用于演示, 可按需删除。
-- ----------------------------------------------------------------------------

-- demo 数据表
CREATE TABLE IF NOT EXISTS `sales` (
    `id`      INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `day`     DATE         NOT NULL COMMENT '日期',
    `channel` VARCHAR(20)  NOT NULL COMMENT '渠道',
    `amount`  INT          NOT NULL DEFAULT 0 COMMENT '金额',
    PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='示例销售表';

INSERT INTO `sales` (`day`, `channel`, `amount`) VALUES
    ('2026-06-20', 'web', 120), ('2026-06-20', 'app', 80),
    ('2026-06-21', 'web', 150), ('2026-06-21', 'app', 95),
    ('2026-06-22', 'web', 130), ('2026-06-22', 'app', 110);

-- demo 报表模板: 顶部 date_range 过滤器 {range} 展开为 {from_range} / {to_range},
-- 在 SQL 的 WHERE 中引用以按日期过滤。
INSERT INTO `report` (`name`, `dsn`, `content`, `remark`) VALUES (
    '销售日报',
    'default',
    '${range|日期|-7 days,today|date_range}

-- @id=按渠道汇总
-- @chart=pie:channel,total
SELECT channel, SUM(amount) AS total
FROM sales
WHERE day >= ''{from_range}'' AND day <= ''{to_range}''
GROUP BY channel;

-- @id=每日趋势
-- @chart=__auto__
SELECT day, SUM(amount) AS 金额
FROM sales
WHERE day >= ''{from_range}'' AND day <= ''{to_range}''
GROUP BY day
ORDER BY day;',
    '按日期过滤的渠道占比与每日趋势'
);

-- +goose NO TRANSACTION

-- +goose Up
CREATE TABLE IF NOT EXISTS `dsn` (
    `id`         INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`       VARCHAR(64)  NOT NULL COMMENT '数据源名称, 报表中通过 @dsn 引用',
    `driver`     VARCHAR(32)  NOT NULL DEFAULT 'mysql' COMMENT '驱动: mysql / postgres / ...',
    `dsn`        TEXT         NOT NULL COMMENT '连接串, 形如 user:pass@tcp(host:port)/db',
    `remark`     VARCHAR(255) NOT NULL DEFAULT '' COMMENT '备注',
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

CREATE TABLE IF NOT EXISTS `report` (
    `id`          INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`        VARCHAR(128) NOT NULL COMMENT '报表名称',
    `parent_id`   INT UNSIGNED NOT NULL DEFAULT 0 COMMENT '父节点 (文件夹) id, 0 为根',
    `type`        VARCHAR(16)  NOT NULL DEFAULT 'report' COMMENT 'report 报表 / folder 文件夹',
    `sort`        INT          NOT NULL DEFAULT 0 COMMENT '同级排序',
    `dsn`         VARCHAR(64)  NOT NULL DEFAULT 'default' COMMENT '默认数据源',
    `content`     MEDIUMTEXT   NULL COMMENT '报表模板内容 (SQL + 注解 + 过滤器); 文件夹为空',
    `settings`    TEXT         NULL COMMENT '页面级配置 JSON',
    `remark`      VARCHAR(255) NOT NULL DEFAULT '',
    `is_public`   TINYINT(1)   NOT NULL DEFAULT 0 COMMENT '是否开启公开分享',
    `share_token` VARCHAR(64)  NOT NULL DEFAULT '' COMMENT '公开访问令牌 (不可枚举)',
    `created_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_share_token` (`share_token`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='报表模板';

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

-- +goose Down
DROP TABLE IF EXISTS `role`;
DROP TABLE IF EXISTS `user`;
DROP TABLE IF EXISTS `report`;
DROP TABLE IF EXISTS `dsn`;

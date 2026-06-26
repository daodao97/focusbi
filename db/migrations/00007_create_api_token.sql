-- +goose NO TRANSACTION

-- +goose Up
CREATE TABLE IF NOT EXISTS `api_token` (
    `id`           INT UNSIGNED NOT NULL AUTO_INCREMENT,
    `user_id`      INT UNSIGNED NOT NULL COMMENT '所属用户 id',
    `name`         VARCHAR(128) NOT NULL DEFAULT '' COMMENT '令牌名称 (便于辨识用途)',
    `token_hash`   CHAR(64)     NOT NULL COMMENT '令牌明文的 SHA-256 (不存明文)',
    `token_prefix` VARCHAR(16)  NOT NULL DEFAULT '' COMMENT '令牌前缀明文 (列表展示辨识用)',
    `last_used_at` DATETIME     NULL COMMENT '上次使用时间',
    `expires_at`   DATETIME     NULL COMMENT '过期时间 (NULL = 永不过期)',
    `created_at`   DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_hash` (`token_hash`),
    KEY `idx_user` (`user_id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='API 令牌 (供 MCP 等程序化访问)';

-- +goose Down
DROP TABLE IF EXISTS `api_token`;

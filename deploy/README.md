# FocusBI 部署脚本

这个目录提供 Docker 部署用的快速安装脚本。脚本只负责生成部署文件和启动提示, 不会直接启动服务。

## 一行安装

```bash
curl -fsSL https://raw.githubusercontent.com/daodao97/focusbi/main/deploy/install.sh | bash
```

脚本会让你选择部署模式:

- `stack`: FocusBI + MySQL + Redis, 适合新服务器从零部署。
- `external`: 只部署 FocusBI, 连接已有 MySQL 和 Redis。

安装目录里会生成:

- `.env`
- `conf.dev.yaml`
- `docker-compose.yml`
- `README.deploy.md`
- `data/`, 仅 `stack` 模式用于 MySQL/Redis 持久化

确认配置无误后, 手动启动:

```bash
docker compose pull
docker compose up -d
docker compose logs -f app
```

## 无交互安装

新服务器, 同时部署 MySQL 和 Redis:

```bash
curl -fsSL https://raw.githubusercontent.com/daodao97/focusbi/main/deploy/install.sh | \
  FOCUSBI_ASSUME_YES=1 \
  FOCUSBI_MODE=stack \
  FOCUSBI_INSTALL_DIR=/opt/focusbi \
  SITE_URL=https://bi.example.com \
  bash
```

连接已有 MySQL 和 Redis:

```bash
curl -fsSL https://raw.githubusercontent.com/daodao97/focusbi/main/deploy/install.sh | \
  FOCUSBI_ASSUME_YES=1 \
  FOCUSBI_MODE=external \
  FOCUSBI_INSTALL_DIR=/opt/focusbi \
  SITE_URL=https://bi.example.com \
  MYSQL_DSN='user:pass@tcp(mysql.example.com:3306)/focusbi?charset=utf8mb4&parseTime=True&loc=Local' \
  REDIS_ADDR='redis.example.com:6379' \
  bash
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `FOCUSBI_MODE` | 交互选择 | `stack` 或 `external` |
| `FOCUSBI_INSTALL_DIR` | `./focusbi` | 安装目录 |
| `FOCUSBI_IMAGE` | `ghcr.io/daodao97/focusbi:latest` | Docker 镜像 |
| `FOCUSBI_PORT` | `8080` | 宿主机端口 |
| `ENABLE_CRON` | `true` | 是否启用任务调度 |
| `SITE_URL` | `http://127.0.0.1:<port>` | 外部访问地址 |
| `SITE_JWT_SECRET` | 自动生成 | 登录令牌签名密钥 |
| `JWT_SECRET` | 自动生成 | `SITE_JWT_SECRET` 的兼容别名 |
| `ENGINE_QUERY_TIMEOUT` | `3m` | 单次 SQL 查询超时 |
| `AI_PROVIDER` | 空 | AI provider, 如 `claude` / `openai` |
| `AI_BASE_URL` | 空 | AI 服务地址 |
| `AI_API_KEY` | 空 | AI API Key |
| `AI_MODEL` | 空 | AI 模型名 |
| `TURNSTILE_SITE_KEY` | 空 | Cloudflare Turnstile site key |
| `TURNSTILE_SECRET_KEY` | 空 | Cloudflare Turnstile secret key |
| `MYSQL_ROOT_PASSWORD` | 自动生成 | `stack` 模式 MySQL root 密码 |
| `MYSQL_DSN` | 交互输入 | `external` 模式 MySQL DSN |
| `REDIS_ADDR` | 交互输入 | `external` 模式 Redis 地址 |
| `FOCUSBI_FORCE` | 未设置 | 设为 `1` 时覆盖已生成文件 |
| `FOCUSBI_ASSUME_YES` | 未设置 | 设为 `1` 时使用默认值进行无交互安装 |

安装后，管理员可在后台「系统设置」动态覆盖 SQL 查询超时/并发数、脚本超时/网络访问、
任务调度和公开分享开关。动态值保存在主库 `system_setting` 表，无需重新发布服务。

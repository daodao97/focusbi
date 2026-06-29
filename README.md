# FocusBI

> **AI 原生 BI**: 把报表开发交给 Claude Code / Codex。Agent 直接探库、写报表、试跑、发布, 你只审核结果。

FocusBI 不是拖拽式 BI, 也不是贴一段 SQL 的查询页面。它把整条「数据库 → 报表」开发链路
做成了 **MCP 工具集**: agent 连上数据源、看懂表结构、写出可版本化的 SQL 报表、试跑校验、
创建发布——全程在你的 RBAC 权限内, 不绕过任何授权。

```
你: “看下 sales 库, 做一个本月各渠道销售额的日趋势报表, 带饼图占比, 发布到「运营」目录”

Claude Code:
  → list_datasources / list_tables / describe_table   探清库表结构
  → query_raw                                          采样几行确认口径
  → get_syntax_doc                                     学会模板语法
  → preview_template                                   试跑, 看到真实结果和每个区块的报错
  → create_report → publish_report                     落库并发布
完成。报表链接: https://bi.example.com/...
```

报表写完即上线, 业务可看、可分享、可定时推送。整个过程你不必手写一行 SQL, 也不用在页面里反复调试。

## 为什么是 AI 原生

| 传统 BI / SQL 页面 | FocusBI |
| --- | --- |
| 人手探库 → 写 SQL → 页面里反复调 → 截图给业务 | agent 探库 → 生成模板 → 自动试跑修正 → 发布 |
| 报表是黑盒, 改一次要重做 | 报表是**文本模板**, agent 能读懂、能改、能 diff |
| AI 只能聊天给建议 | AI 拿到 12 个 MCP 工具, 直接动手干活 |
| 权限靠人工把关 | agent 的每个动作都走用户 RBAC, 越权直接报错 |

### MCP 工具集 (agent 的双手)

接入后 agent 可调用这些工具 (全部继承令牌所属用户的权限):

**探库** — `list_datasources` · `list_databases` · `list_tables` · `describe_table` · `query_raw` (只读采样)
**学语法** — `get_syntax_doc` (写模板前先读)
**读已有** — `list_reports` · `get_report`
**写 & 验** — `preview_template` (试跑不落库, 返回每个区块的结构化结果与报错)
**落库** — `create_report` · `update_report` (只改草稿) · `publish_report` (发布 + 版本快照)

这套工具让 agent 形成闭环: **探 → 学 → 写 → 试 → 修 → 发**。`preview_template` 的结构化报错
是关键——agent 能看到自己写的模板哪一块跑挂了, 自己改, 不需要人来回贴错误。

### 报表是文本, 所以 agent 能驾驭

一份报表就是一段可版本化的文本——过滤器、SQL、展示注解写在一起。正因为是文本,
agent 才能读懂它、增量改它、做 code review:

```sql
${range|日期|-7 days,today|date_range}

-- @title=每日销售趋势
-- @chart=__auto__
SELECT day AS 日期, SUM(amount) AS 销售额  -- @{"format":"money"}
FROM sales WHERE day >= '{from_range}' AND day <= '{to_range}'
GROUP BY day ORDER BY day;
```

引擎内置常见报表变换 (透视、转置、`@join`/`@union`、合计、格式化、条件着色、波动检测),
agent 少写重复 CTE。完整语法见 [`docs/SYNTAX.md`](docs/SYNTAX.md)——它同时是人读的规范、
agent 的 system prompt、和 `get_syntax_doc` 返回的内容, 一处维护。

### 两条 AI 工作流

- **MCP 自动化开发** (主线): Claude Code / Codex 通过令牌全自动探库、生成、试跑、发布。
- **页面内 AI 改模板**: 在编辑器里对话改报表, 流式说明 + SEARCH/REPLACE 补丁 + 即时预览。

### 从开发到交付的闭环

- 多数据源: MySQL / PostgreSQL / SQLite, MySQL 支持 SSH 隧道。
- 草稿 / 发布版分离, 版本历史和回滚——agent 改草稿, 你审了再发。
- RBAC: 数据源、报表、目录三级权限, agent 与人共用同一套。
- 公开分享: 不可枚举令牌, 随时关闭。
- 定时任务: cron 定时跑, 推飞书 / 企业微信, 支持阈值触发的异常提醒。

## 本地开发

依赖:

- Go 1.25+
- Node 20+ 和 pnpm
- MySQL 主库, 用于保存 FocusBI 自身数据; 报表数据源可另配 MySQL / PostgreSQL / SQLite

```bash
# 1. 配置主库和站点密钥
$EDITOR conf.dev.yaml

# 2. 构建前端并启动后端
make run

# 3. 可选: 导入 demo 数据
mysql -u<user> -p<pass> <db> < docs/schema.sql

# 4. 打开控制台, 首次进入注册管理员
open http://127.0.0.1:8099
```

`make run` 等价于 `make web && go run ./cmd --app-env dev --bind :8099`。启动时会自动执行
`db/migrations` 建表。

## 部署方式

FocusBI 是一个单二进制应用, 运行时依赖一个 MySQL 主库和 Redis。MySQL 用来保存系统数据
(用户、报表、版本、数据源配置等), Redis 用于缓存和任务调度锁。

推荐只使用安装脚本部署:

```bash
curl -fsSL https://raw.githubusercontent.com/daodao97/focusbi/main/deploy/install.sh | bash
```

脚本会让你选择部署模式:

- `stack`: 同时生成 FocusBI、MySQL、Redis 的 compose 配置, 适合新服务器。
- `external`: 只生成 FocusBI 应用配置, 连接已有 MySQL 和 Redis。

安装脚本只生成文件, 不直接启动服务。它会写入 `.env`、`conf.dev.yaml`、`docker-compose.yml`
和 `README.deploy.md`, 并随机生成 `site.jwt_secret`。确认配置后按提示启动:

```bash
cd <install_dir>
docker compose pull
docker compose up -d
docker compose logs -f app
```

无交互部署可以通过环境变量完成, 例如:

```bash
curl -fsSL https://raw.githubusercontent.com/daodao97/focusbi/main/deploy/install.sh | \
  FOCUSBI_ASSUME_YES=1 \
  FOCUSBI_MODE=stack \
  FOCUSBI_INSTALL_DIR=/opt/focusbi \
  SITE_URL=https://bi.example.com \
  bash
```

更多参数见 [`deploy/README.md`](deploy/README.md)。

### 部署配置检查

配置项完整说明见下文 [配置](#配置)。上线前两个部署特有的点:

- `site.jwt_secret` 必须改成随机密钥 (`openssl rand -hex 32`), 不能用默认占位值。
- `database[default]` / `redis[default]` 容器部署时要填**容器内可访问**的地址, 不是宿主机的 `127.0.0.1`。

镜像由 GitHub Actions 在 push 到 `main` 或打 `v*` tag 时构建并推送到 GHCR; 官方默认镜像名为
`ghcr.io/daodao97/focusbi:latest`。自建 fork 可把镜像地址替换为自己的 `ghcr.io/<owner>/focusbi`。

## 常用命令

```bash
make run        # 构建前端并启动后端 (dev, :8099)
make web        # 仅构建前端 -> web/dist (由 Go embed 内嵌进二进制)
make web_dev    # 前端开发服务器 (HMR, 代理 /api 到 :8099)
make build      # 生产构建单二进制 -> build/server (内嵌前端)

go test ./...                       # 全部 Go 测试
go test ./internal/engine/          # 报表引擎 (核心逻辑所在)

ENABLE_CRON=true go run ./cmd --app-env dev   # 启用定时任务调度
```

## MCP 安装 

系统在 `/mcp` 暴露 MCP 服务 (Streamable HTTP)。先在控制台「API 令牌」生成一个令牌
(`fbt_...`), 再配置到 AI 客户端。

**Claude Code** 一键添加:

```bash
claude mcp add --scope local --transport http focusbi http://127.0.0.1:8099/mcp \
  --header "Authorization: Bearer fbt_xxxxx"
```

这个命令写入本机 `~/.claude.json`, 只在当前项目生效, 适合放令牌。团队共享配置可写项目根目录
`.mcp.json`, 令牌通过环境变量传入:

```json
{
  "mcpServers": {
    "focusbi": {
      "type": "http",
      "url": "http://127.0.0.1:8099/mcp",
      "headers": { "Authorization": "Bearer ${FOCUSBI_TOKEN}" }
    }
  }
}
```

```bash
export FOCUSBI_TOKEN="fbt_xxxxx"
```

**Codex** 一键添加:

```bash
codex mcp add focusbi --url http://127.0.0.1:8099/mcp \
  --bearer-token-env-var FOCUSBI_TOKEN
```

令牌通过环境变量传入:

```bash
export FOCUSBI_TOKEN="fbt_xxxxx"
```

也可以手写 `~/.codex/config.toml`:

```toml
[mcp_servers.focusbi]
url = "http://127.0.0.1:8099/mcp"
bearer_token_env_var = "FOCUSBI_TOKEN"
```

之后即可让 AI 列报表 → 看表结构 → 写模板 → `preview_template` 试跑 → 创建并发布。
所有操作严格受该令牌所属用户的权限限制。详见 [`docs/REPORT.md`](docs/REPORT.md) 的 MCP 章节。

## 配置

开发配置在 `conf.dev.yaml` (由 `--app-env dev` 加载), 主要段:

- `site` —— 站点 / 服务级配置:
  - `site.jwt_secret` —— **必填**, 登录 token 的签名密钥。为空或仍是默认占位值时**拒绝
    启动** (否则任何人都能伪造 token)。用 `openssl rand -hex 32` 生成一个。
  - `site.url` —— 站点对外地址, 用于定时任务里拼报表链接。
- `database` —— 主库 (`default`, MySQL) 与连接串。
- `redis` —— 缓存 / 分布式锁 (任务调度多实例去重需要)。
- `engine.query_timeout` —— 单次 SQL 查询超时, 默认 `3m`, 支持 `30s` / `3m` 这类 Go duration。
- `ai` —— AI provider (`claude` / `openai`)、`base_url`、`api_key`、`model`。
- `turnstile` —— 登录人机验证 (Cloudflare Turnstile)。

启动时会读取当前目录 `.env`, 再加载 `conf.dev.yaml`, 最后用环境变量覆盖支持 env tag 的配置。
常用覆盖项是 `SITE_JWT_SECRET`、`SITE_URL`、`ENGINE_QUERY_TIMEOUT`、`AI_API_KEY`、
`TURNSTILE_SECRET_KEY`; 已存在的系统环境变量优先于 `.env`。

## 技术栈

后端 Go + Gin + [xgo](https://github.com/daodao97/xgo) (xdb/xredis/xcron/xapp)、
goose 迁移、goja (JS 脚本)、Eino (LLM)。
前端 Vue3 + Vite (多页) + Element Plus + ECharts, 构建产物由 Go `embed` 内嵌为单二进制。

# FocusBI

> AI 报表系统 —— 用 SQL 模板写报表, 与 AI 对话改报表, 一键分享与定时推送。

FocusBI 是一个基于 **Go + Gin** 的 SQL 报表管理系统, 移植并扩展了
[dataddy](https://github.com/daodao97/dataddy) 的报表模板理念。你用一段
「SQL + 注解 + 过滤器」的模板描述一张报表, 后端解析执行、前端渲染成表格与
ECharts 图表; 还能通过与 AI 对话实时修改模板、把报表公开分享或定时推送到群机器人。

## 特性

- **dataddy 风格模板**：一段文本即一张报表 —— 多个 SQL 区块 + 注解 (`-- @chart`、
  `@sum`、`@join` …) + 交互过滤器 (`${日期|...|date_range}`) + 宏 (`{name}`)。
  完整语法见 [`docs/SYNTAX.md`](docs/SYNTAX.md)。
- **多数据源**：MySQL / PostgreSQL / SQLite, 报表用 `-- @dsn=name` 选择;
  MySQL 支持 **SSH 隧道**连接跳板机后的库。
- **丰富的数据变换**：结果过滤、服务端排序、日期补全、行转列透视、行列转置、
  区块横向 `@join` / 纵向 `@union` 合并、合计/平均、单元格标签与条件着色。
- **动态脚本报表**：`#!SCRIPT` 区块内写 JavaScript (goja), 运行时拼 SQL、产出
  多区块与图表。
- **AI 对话改报表**：基于 Eino 统一接口 (Claude / OpenAI), 流式说明 + SEARCH/REPLACE
  补丁, 改完即时预览。
- **版本管理**：报表分开发版 / 发布版, 支持版本历史与回滚。
- **权限体系**：基于角色 (RBAC) 的数据源 / 报表读写权限, 报表目录层级继承。
- **公开分享**：生成不可枚举的分享令牌, 免登录查看 (可随时关闭)。
- **定时订阅推送**：cron 定时跑报表, 推送到飞书 / 企业微信; 支持阈值告警与数据波动检测。
- **MCP 服务**：内置 Model Context Protocol 服务, 可在 Codex / Claude Code 等 AI 工具里
  直接探数据源、试跑模板、开发报表; 凭 API Token 鉴权, 严格继承用户 RBAC 权限。

## 快速开始

依赖: Go 1.25+、Node 20+ (含 `pnpm`)、一个 **MySQL** 库 (系统主库, 启动时自动建表)。

```bash
# 1. 准备主库: 在 conf.dev.yaml 的 database[default] 填好 MySQL 连接串
#    (启动时会用 goose 自动执行 db/migrations 建表)

# 2. 构建前端 + 启动后端 (dev)
make run        # = make web && go run ./cmd --app-env dev --bind :8099

# 3. (可选) 导入 demo 数据: sales 示例表 + 一张销售日报模板
mysql -u<user> -p<pass> <db> < docs/schema.sql

# 4. 浏览器访问
#    http://127.0.0.1:8099              管理控制台 (首次进入注册管理员)
#    http://127.0.0.1:8099/view.html#/1 独立查看报表 1
```

> 提示: 主库必须是 MySQL (迁移仅支持 MySQL); 而报表数据源 (`dsn` 表) 可以是
> MySQL / PostgreSQL / SQLite。

## Docker 部署 (拉取预构建镜像)

每次 push 到 `main` 或打 `v*` tag, GitHub Action 会自动构建并推送多架构镜像
(amd64 + arm64) 到 GitHub Container Registry (`ghcr.io/<owner>/focusbi`)。

### 一键启动栈 (推荐, 自带 MySQL + Redis)

仓库提供 `compose.ghcr.yaml` —— 拉镜像 + 起 MySQL/Redis + 等它们 ready 再启动
(启动即自动跑数据库迁移):

```bash
# <owner> 换成你的 GitHub 用户/组织名
IMAGE=ghcr.io/<owner>/focusbi:latest docker compose -f compose.ghcr.yaml up -d

# 访问 http://127.0.0.1:8080 (首次进入注册管理员)
```

配置在 `conf.dev.yaml` (挂载进容器, `APP_ENV=dev` 加载)。用本栈前把它的 `database` / `redis`
地址改成 compose 的服务名 `mysql` / `redis`; **生产务必改 `site.jwt_secret` / 数据库密码 /
`site.url`**。MySQL/Redis 数据持久化到本地 `./data/` 目录 (绑定挂载), 想清库重来直接
`docker compose -f compose.ghcr.yaml down` 后删掉 `./data/` 即可。

### 单容器运行 (自备 MySQL + Redis)

```bash
docker run -d --name focusbi -p 8080:8080 \
  -v $(pwd)/conf.dev.yaml:/app/conf.dev.yaml \
  -e ENABLE_CRON=true \
  ghcr.io/<owner>/focusbi:latest --bind 0.0.0.0:8080
```

把 `conf.dev.yaml` 里的 `database` / `redis` 改成你的实际地址。注意 `site.jwt_secret` 必填,
为空或默认占位值会拒绝启动。

## 常用命令

```bash
make run        # 构建前端并启动后端 (dev, :8099)
make web        # 仅构建前端 -> web/dist (由 Go embed 内嵌进二进制)
make web_dev    # 前端开发服务器 (HMR, 代理 /api 到 :8099)
make build      # 生产构建单二进制 -> build/server (内嵌前端)

go test ./...                       # 全部 Go 测试
go test ./internal/engine/          # 报表引擎 (核心逻辑所在)

ENABLE_CRON=true go run ./cmd --app-env dev   # 启用定时订阅调度
```

## 模板速览

```sql
-- 顶部声明交互过滤器
${range|日期|-7 days,today|date_range}
${channel|渠道||enum(web:网页,app:客户端)}

-- 区块一: 每日趋势 (折线图 + 表格)
-- @id=每日趋势
-- @chart=__auto__
SELECT
    day            AS 日期,
    SUM(amount)    AS 金额    -- @{"header":"销售额","count":true}
FROM sales
WHERE day >= '{from_range}' AND day <= '{to_range}'
  AND channel = '{channel}'           -- {?channel}   (channel 为空时删除此行)
GROUP BY day ORDER BY day;

-- 区块二: 渠道占比 (饼图)
-- @id=渠道占比
-- @chart=pie:channel,total
SELECT channel, SUM(amount) AS total FROM sales GROUP BY channel;
```

- 过滤器 `${...}` 渲染成前端输入控件, 在 SQL 里以宏 `{name}` 引用;
  `date_range` 自动展开为 `{from_range}` / `{to_range}`。
- 每条以 `;` 结尾的 SQL 为一个表格区块; 区块前的 `-- @key=value` 是该区块注解。
- 完整语法 (注解 / 列配置 / 图表 / 透视 / 合并 / 脚本区块 / 宏) 见
  [`docs/SYNTAX.md`](docs/SYNTAX.md)。

## 架构概览

```
cmd/main.go            程序入口 (xgo/xapp 装配: 配置 -> redis -> dao -> Gin)
conf/                  配置 (数据库 / redis / AI / 站点)
dao/                   各表数据访问 (report / dsn / user / role / subscription …)
db/migrations/         goose 迁移文件 (启动自动执行, 仅 MySQL)
internal/engine/       报表模板引擎 (解析 / 过滤器 / 宏 / 数据管线 / 图表)  ← 核心
internal/datasource/   多数据源连接池 + SSH 隧道 + 原始查询
internal/ai/           AI 对话改模板 (Eino: Claude / OpenAI)
internal/subscription/ 订阅推送 (渲染 + 飞书/企微 webhook + 条件告警)
internal/auth/         登录 / JWT / RBAC 权限
job/                   xcron 调度 (每分钟扫描订阅, 分布式锁)
api/                   REST 接口 (setup.go) + 内嵌前端挂载 (ui.go)
web/                   Vue3 + Vite 多页前端 (index.html 控制台 / view.html 查看页)
docs/SYNTAX.md         报表模板语法 (唯一权威, 同时是 AI system prompt 与应用内文档)
docs/REPORT.md         架构与运维说明
```

报表执行的核心是 `internal/engine` 的 `Runner.Run(content, params)`: 解析模板 →
按过滤器生成宏并替换进 SQL → 执行查询 → 跑数据管线 (过滤/排序/透视/合并/波动…) →
产出 `Result` (过滤器定义 + 区块) 交前端渲染。详见
[`docs/REPORT.md`](docs/REPORT.md) 与 [`CLAUDE.md`](CLAUDE.md)。

## 在 AI 工具中开发报表 (MCP)

系统在 `/mcp` 暴露 MCP 服务 (Streamable HTTP)。先在控制台「API 令牌」生成一个令牌
(`fbt_...`), 再配置到 AI 客户端。

**Claude Code** (`.mcp.json` 或 `claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "focusbi": {
      "url": "http://127.0.0.1:8099/mcp",
      "headers": { "Authorization": "Bearer fbt_xxxxx" }
    }
  }
}
```

**Codex** (`~/.codex/config.toml`) —— 令牌经环境变量传入:

```toml
[mcp_servers.focusbi]
url = "http://127.0.0.1:8099/mcp"
bearer_token_env_var = "FOCUSBI_TOKEN"
```

```bash
# 启动 Codex 前设置令牌环境变量
export FOCUSBI_TOKEN="fbt_xxxxx"
```

> 也可用 CLI 添加: `codex mcp add focusbi --url http://127.0.0.1:8099/mcp --bearer-token-env-var FOCUSBI_TOKEN`。

之后即可让 AI 列报表 → 看表结构 → 写模板 → `preview_template` 试跑 → 创建并发布。
所有操作严格受该令牌所属用户的权限限制。详见 [`docs/REPORT.md`](docs/REPORT.md) 的 MCP 章节。

## 配置

开发配置在 `conf.dev.yaml` (由 `--app-env dev` 加载), 主要段:

- `site` —— 站点 / 服务级配置:
  - `site.jwt_secret` —— **必填**, 登录 token 的签名密钥。为空或仍是默认占位值时**拒绝
    启动** (否则任何人都能伪造 token)。用 `openssl rand -hex 32` 生成一个。
  - `site.url` —— 站点对外地址, 用于订阅推送里拼报表链接。
- `database` —— 主库 (`default`, MySQL) 与连接串。
- `redis` —— 缓存 / 分布式锁 (订阅调度多实例去重需要)。
- `ai` —— AI provider (`claude` / `openai`)、`base_url`、`api_key`、`model`。
  仅从配置文件读取, 不走环境变量。
- `turnstile` —— 登录人机验证 (Cloudflare Turnstile)。

## 技术栈

后端 Go + Gin + [xgo](https://github.com/daodao97/xgo) (xdb/xredis/xcron/xapp)、
goose 迁移、goja (JS 脚本)、Eino (LLM)。
前端 Vue3 + Vite (多页) + Element Plus + ECharts, 构建产物由 Go `embed` 内嵌为单二进制。

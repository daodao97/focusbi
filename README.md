# FocusBI

> 面向工程团队和数据团队的 SQL 报表平台。通过 MCP 让 Codex / Claude Code 自动探库、写报表、试跑和发布。

FocusBI 不是拖拽式 BI, 也不是只能贴一段 SQL 的查询页面。它把报表定义成一份可版本化的
**SQL 模板**: SQL 负责口径, 注解负责展示, 脚本负责复杂编排, MCP/AI 负责自动化开发。

适合这类场景:

- 希望在 Codex / Claude Code 等 AI 工具里直接探库、写模板、试跑、创建和发布报表。
- 业务口径必须可审查、可复制、可版本管理。
- 同一份数据要生成多个图表和表格, 且指标口径不能漂。
- 报表需要公开分享、权限隔离、定时推送和异常提醒。

## 核心特色

### 1. MCP 自动化开发报表

FocusBI 内置 MCP 服务, AI 工具拿到 API Token 后可以直接完成报表开发链路:

- 探数据源、查表结构、看字段样例。
- 读取已有报表模板和版本。
- 编写 SQL 模板并调用 `preview_template` 试跑。
- 创建报表、更新草稿、发布版本。

这让报表开发可以从“人手写 SQL + 页面里反复调试”, 变成“AI 在受控权限内探库、生成、试跑、修正、发布”。
所有 MCP 操作都继承用户 RBAC 权限, 不绕过数据源和报表授权。

### 2. SQL 模板即报表

一份报表就是一段文本: 过滤器、SQL、展示注解、列配置都写在一起。

```sql
${range|日期|-7 days,today|date_range}
${channel|渠道||enum(web:网页,app:客户端)}

-- @id=daily_sales
-- @title=每日销售趋势
-- @chart=__auto__
-- @sum=true
SELECT
  day AS 日期,
  SUM(amount) AS 销售额 -- @{"count":true,"format":"money"}
FROM sales
WHERE day >= '{from_range}' AND day <= '{to_range}'
  AND channel = '{channel}' -- {?channel}
GROUP BY day
ORDER BY day;
```

模板可以放进 Git 做 code review, 也可以在页面里编辑、预览、发布和回滚。

### 3. 一次取数, 多种展示

普通 SQL block 和脚本都可以产出图表/表格。需要复用同一份数据时, 可以把前置 block 当作
数据源引用:

```sql
-- @id=sales_by_business_line
-- @hidden=true
SELECT 月份, 业务线, 订单数, 销售额, 利润 FROM ...;

#!SCRIPT
const rows = dataset('sales_by_business_line')

result.chart({
  id: 'compare_chart',
  title: '本月 vs 上月销售对比',
  type: 'bar',
  x: '业务线',
  y: ['销售额', '利润'],
  rows
})

result.table({
  id: 'detail_table',
  title: '业务线收入明细',
  columns: ['月份', '业务线', '订单数', '销售额', '利润'],
  rows
})
#!END
```

这能减少复制 SQL 带来的口径不一致问题。

### 4. 面向复杂报表的数据管线

内置常见报表变换, 尽量少写重复 CTE:

- 结果过滤、服务端排序、日期补全。
- 行转列透视、行列转置。
- 多 SQL 横向 `@join` / 纵向 `@union`。
- 合计/平均、列格式化、枚举映射、单元格标签、条件着色。
- 数据波动检测, 可用于订阅推送里的异常提醒。

SQL block 和 `#!SCRIPT query()` 只允许单条只读 `SELECT/WITH`, 并有可配置查询超时, 适合给团队安全使用。

### 5. AI 不是聊天玩具, 是报表开发工具

FocusBI 内置两条 AI 工作流:

- **页面内 AI 修改模板**: 流式说明 + SEARCH/REPLACE 补丁 + 即时预览。
- **MCP 自动化开发**: Codex / Claude Code 可通过 API Token 探数据源、查表结构、试跑模板、创建和发布报表。

AI 的所有操作都继承用户 RBAC 权限, 不绕过报表和数据源授权。

### 6. 从报表开发到交付闭环

- 多数据源: MySQL / PostgreSQL / SQLite, MySQL 支持 SSH 隧道。
- 报表开发版 / 发布版分离, 支持版本历史和回滚。
- RBAC: 数据源、报表、目录权限控制。
- 公开分享: 生成不可枚举分享令牌, 可随时关闭。
- 订阅推送: cron 定时跑报表, 推送到飞书 / 企业微信, 支持条件触发。

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
(用户、报表、版本、数据源配置等), Redis 用于缓存和订阅调度锁。

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

上线前至少检查这些配置:

- `site.jwt_secret`: 必须改成随机密钥, 不能用默认占位值。
- `site.url`: 外部访问地址, 订阅推送里的报表链接会用到。
- `database[default]`: FocusBI 主库。容器部署时要填写容器内可访问的地址。
- `redis[default]`: 缓存和订阅调度锁。容器部署时要填写容器内可访问的地址。
- `engine.query_timeout`: 单次 SQL 查询超时, 默认 `3m`。

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

## 在 AI 工具中开发报表 (MCP)

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
  - `site.url` —— 站点对外地址, 用于订阅推送里拼报表链接。
- `database` —— 主库 (`default`, MySQL) 与连接串。
- `redis` —— 缓存 / 分布式锁 (订阅调度多实例去重需要)。
- `engine.query_timeout` —— 单次 SQL 查询超时, 默认 `3m`, 支持 `30s` / `3m` 这类 Go duration。
- `ai` —— AI provider (`claude` / `openai`)、`base_url`、`api_key`、`model`。
  仅从配置文件读取, 不走环境变量。
- `turnstile` —— 登录人机验证 (Cloudflare Turnstile)。

## 技术栈

后端 Go + Gin + [xgo](https://github.com/daodao97/xgo) (xdb/xredis/xcron/xapp)、
goose 迁移、goja (JS 脚本)、Eino (LLM)。
前端 Vue3 + Vite (多页) + Element Plus + ECharts, 构建产物由 Go `embed` 内嵌为单二进制。

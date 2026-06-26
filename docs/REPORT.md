# FocusBI — AI 报表系统

在 focusbi (Go + Gin + xgo) 基础上实现的 AI 报表系统, 移植了
[dataddy](https://github.com/daodao97/dataddy) 的报表模板理念:

- 系统内可定义多个**数据源 (DSN)**, 报表通过 `-- @dsn=name` 选择;
- 报表使用 **dataddy 风格模板** (SQL + 注解 + 过滤器 + 宏);
- 后端解析模板、执行 SQL, 前端渲染表格 + ECharts 图表;
- 报表可通过**与 AI 对话**实时修改模板。

## 目录结构

```
conf/conf.go                     配置 (新增 AIConf)
dao/dsn.go  dao/report.go        dsn / report 表的数据访问
internal/datasource/             多数据源连接管理 + 原始查询
internal/engine/                 报表模板引擎 (parser / filter / macro / chart)
internal/ai/                     OpenAI 兼容对话客户端
api/setup.go                     REST 接口
api/ui.go                        内嵌前端 (web/dist) 的挂载
web/                             Vue3 + Vite 多页前端 (源码)
  ├─ index.html  src/console/    管理控制台 (报表列表/编辑/数据源)
  ├─ view.html   src/viewer/     独立报表查看页 (类似 dataddy /open)
  ├─ src/components/             ChartBlock / ReportFilters / ReportBlocks / AiChat
  └─ src/api.js                  接口封装
web/embed.go                     go:embed web/dist
db/migrations/*.sql              goose 数据库迁移文件 (启动自动执行)
docs/schema.sql                  完整示例 SQL (含 demo 数据)
```

## 前端 (Vue3 + Vite 多页)

采用 Vite MPA, 两个入口:

- `index.html` → **管理控制台**: 报表列表、模板编辑器 (左侧编辑 + 右侧 AI 对话 + 下方实时预览)、数据源管理。Hash 路由 (`/reports`、`/dsn`)。
- `view.html` → **独立查看页**: 按 `view.html#/<报表id>` 渲染单张报表, 含过滤器, 适合分享/嵌入。

技术栈: Vue3 + vue-router + Element Plus + ECharts。构建产物 `web/dist` 由 Go `embed` 内嵌进二进制。

```bash
# 前端开发 (代理 /api 到 :8099, 热更新)
make web_dev          # 或 pnpm --dir web run dev

# 构建前端
make web              # 或 cd web && pnpm install && pnpm build
```

## 初始化与运行

```bash
# 1. 构建前端 + 启动后端 (dev)
# 启动时会在 conf.database[default] 指向的库自动执行 goose 迁移
make run              # = make web + go run ./cmd --app-env dev --bind :8099

# 可选: 导入 demo 数据 (sales 示例表 + 销售日报模板)
mysql ... < docs/schema.sql

# 浏览器打开 http://127.0.0.1:8099       (控制台)
#           http://127.0.0.1:8099/view.html#/1  (查看报表 1)

# 生产构建 (单二进制, 内嵌前端)
make build            # -> build/server
```

## 报表模板语法

报表模板语法 (区块/注解/列配置/过滤器/宏/图表/透视) 的**完整权威说明**见
[`docs/SYNTAX.md`](./SYNTAX.md)。该文档同时作为:

- **AI 助手**生成/修改模板的 system prompt 依据 (`internal/ai` 内嵌 `docs.SyntaxMarkdown`);
- 报表**编辑页「开发文档」按钮**展示的内容 (前端 fetch `/SYNTAX.md`)。

三者同源, 改语法只需改 `docs/SYNTAX.md` 一处。

## 数据源 (DSN)

系统内可定义多个数据源, 报表通过默认 `dsn` 或区块注解 `-- @dsn=name` 选择。
支持三种驱动 (均为纯 Go 实现, 无需 CGO):

| 驱动 | driver 值 | 连接串示例 |
|------|-----------|-----------|
| MySQL | `mysql` | `user:pass@tcp(127.0.0.1:3306)/db?charset=utf8mb4&parseTime=true` |
| PostgreSQL | `postgres` (别名 `postgresql`/`pg`) | `postgres://user:pass@127.0.0.1:5432/db?sslmode=disable` |
| SQLite | `sqlite` (别名 `sqlite3`) | `/path/to/data.db` 或 `file:data.db?cache=shared` |

报表 SQL 中的过滤器宏在执行前已内联展开 (不使用占位符), 因此 Postgres 的 `$1` 与
MySQL/SQLite 的 `?` 差异不影响报表查询。注意各库 SQL 方言 (如日期函数) 需按目标库书写。

### SSH 隧道 (仅 mysql)

MySQL 数据源可通过 SSH 跳板机连接 (在数据源弹窗中开启「SSH 隧道」):

- **认证方式**: 密码 (`ssh_auth=password`) 或私钥 (`ssh_auth=key`, 支持带口令的 PEM)。
- 连接串里的 host 应填「从跳板机视角」可达的数据库地址 (常见 `127.0.0.1:3306`)。
- 实现: 基于 `golang.org/x/crypto/ssh` 建立到跳板机的连接, 通过 `RegisterDialContext`
  为该数据源注册自定义网络, mysql 连接经 `direct-tcpip` 通道转发。隧道按数据源缓存,
  配置变更/删除时自动失效重建 (`datasource.Invalidate`)。
- **保活与自动重连**: 每 30s 发送 `keepalive@openssh.com` 防止空闲断开; 若跳板机连接
  仍失效, 拨号时自动重连一次再重试。SSH 数据源的 mysql 连接池缩短存活/空闲时间
  (5min / 60s), 让连接池尽快淘汰挂在已断隧道上的旧连接, 避免 `unexpected EOF`。
- 主机指纹当前使用 `InsecureIgnoreHostKey` (内部工具, 跳板机由使用者自行管理)。

## 用户体系与权限 (RBAC)

移植自 dataddy 的角色权限模型, 实现于 `internal/auth`。

**账号**: 独立 `user` 表 (bcrypt 密码)。**首位注册即超级管理员** —— 系统空表时
`POST /api/auth/register` 开放, 注册者 `is_admin=1`; 表非空后注册关闭, 后续用户由管理员
在「用户管理」建号。认证用 **JWT** (前端 localStorage 存 token, 请求带 `Authorization: Bearer`)。

**权限模型**: 用户挂多个角色 (`user.roles` 逗号分隔角色 id), 角色持有 `resource` JSON 权限,
支持 `parent_id` **父角色继承**。资源串按 `.` 分段成树:

- 模式字符: `r` 读 / `w` 写 / `R` 递归 (覆盖所有更深层子资源)
- 资源: `report`(全部报表) / `report.{id}`(单个) / `report.manage`(建改删) /
  `dsn`(全部数据源) / `dsn.{id}`(单个数据源, 主库用 `dsn.default`)
- 例: `{"report":"Rr","report.5":"rw","dsn.3":"r"}` = 所有报表可读 + 报表5可读写 +
  仅数据源 3 可用
- 超管 (`is_admin`) 全权; 角色不能授出超过自身的权限 (转委校验)

**按数据源授权 (`dsn.{id}`)**: 控制"角色只能用哪些数据源"。

- 全局 `dsn:r` = 可用**所有**数据源 (含主库与未来新增的); `dsn:rw` 额外含**管理数据源**
  (增删改连接串)。给具体 `dsn.{id}:r` 则只能用该数据源 (主库为 `dsn.default`)。
- **运行时强制**: 报表执行经过的**每个**数据源都校验 —— 不只报表绑定的默认源,
  还包括块注解 `@dsn=` 覆盖、脚本 `query(sql,args,dsn)`、`enum_sql` 自定义源,
  无权的块以错误返回。作者无法借 `@dsn=other` 绕到无权的库。
- 资源键用**数据源 id** (与 `report.{id}` 一致), 不受数据源**改名**影响。
- **例外 (不强制)**: 公开分享页 (作者已主动公开) 与订阅推送 (系统定时任务) 按预授权执行。
- 升级兼容: 旧角色若"能读报表但没配过任何 dsn 权限", 启动时自动回填 `dsn:r` (保持
  可用所有源), 之后管理员可在「角色管理 → 按数据源」逐个收紧。

**报表层级 (文件夹)**: `report` 表带 `parent_id` + `type`(report/folder)。文件夹是无 content
的 report 记录, 可任意层级嵌套, 旧报表默认在根 (parent_id=0)。侧边栏与列表页按树渲染。

- **按文件夹授权**: 给某文件夹授 `report.{folderId}:Rr` (整夹可读) / `Rrw` (可读写),
  后端鉴权时沿报表的 **parent_id 祖先链**向上判定 —— 只要任一祖先文件夹被授权即放行,
  从而"父级文件夹的权限自动覆盖其下所有子报表"。
- 列表过滤会保留含可读后代的文件夹 (树不断链); 删除非空文件夹被拒。

**鉴权**: Gin 中间件解析 token → 构建用户权限 → 注入 context。报表列表按权限过滤,
单报表查看校验 `report.{id}:r`, 建改删需 `report.manage:rw`; 数据源列表/schema 探查/运行
按 `dsn.{id}:r` (或全局 `dsn:r`) 细粒度判定, 增删改数据源需 `dsn:rw`; 用户/角色管理仅超管。

## REST 接口

| 方法 | 路径 | 说明 | 权限 |
|------|------|------|------|
| POST | `/api/auth/register` | 注册 (仅首位) | 公开 |
| POST | `/api/auth/login` | 登录, 返回 token | 公开 |
| GET | `/api/auth/bootstrap` | 是否需注册 (空表) | 公开 |
| GET | `/api/auth/me` | 当前用户 + 权限资源 | 登录 |
| GET/POST/PUT/DELETE | `/api/dsn[/:id]` | 数据源 CRUD | `dsn:r`/`dsn:rw` |
| POST | `/api/dsn/test` | 测试连接 | `dsn:rw` |
| GET | `/api/report` | 报表列表 (按权限过滤) | 登录 |
| GET | `/api/report/:id` | 查看报表 | `report.{id}:r` |
| POST/PUT/DELETE | `/api/report[/:id]` | 报表增删改 | `report.manage:rw` |
| POST | `/api/report/:id/run` | 执行报表 | `report.{id}:r` |
| POST | `/api/report/preview` | 模板预览 | `report.manage:rw` |
| POST | `/api/report/ai` | AI 修改模板 | `report.manage:rw` |
| POST | `/api/report/:id/share` | 开/关公开分享 | `report.manage:rw` |
| GET | `/api/public/report/:token` | 公开: 报表元信息 | 公开 |
| POST | `/api/public/report/:token/run` | 公开: 执行报表 | 公开 |
| GET/POST/PUT/DELETE | `/api/user[/:id]` | 用户管理 | 超管 |
| GET/POST/PUT/DELETE | `/api/role[/:id]` | 角色管理 | 超管 |

## 公开分享 (无需登录)

报表可生成**公开链接**, 让未登录用户查看 (类似 dataddy `/open`):

- 每个报表带 `is_public` 开关 + 不可枚举的 `share_token` (32 字符随机十六进制)。
- 在报表查看页点「分享」→ 开启 → 复制链接 `view.html#<token>?过滤参数`。
- **默认关闭**; 关闭后旧链接立即失效。`share_token` 由后端管理, 普通报表
  编辑无法篡改 (`Record()` 不含该字段)。
- 公开页 (`view.html`) 走 `/api/public/report/:token[/run]`, 与登录鉴权完全解耦,
  访客可调过滤器重新查询。仅 `is_public=1` 的报表能被令牌取到。

## 订阅推送 (定时 / 告警)

报表可配置**定时订阅**: 到点自动跑报表, 把结果推送到群机器人 (飞书 / 企业微信)。
入口在报表编辑器「报表设置 → 订阅推送」, 数据存于 `report_subscription` 表,
REST 接口在 `/api/report/:id/subscription`(需 `report.manage:rw`)。

**调度**: `job/job.go` 注册了一个 xcron 任务 `ReportSubscriptionTick`, 每分钟第 0 秒
触发 `subscription.Tick`(开启 `EnableDistLock`, 多实例只跑一次)。Tick 扫描所有
启用订阅, 对 cron 到期的逐条执行。每条订阅在执行前还会**原子抢占本分钟执行权**
(`ClaimSubscriptionRun`), 杜绝重入 / 多实例重复推送。

**一条订阅的字段** (`SubscriptionRecord`, 见 `dao/subscription.go`):

| 字段 | 说明 |
|------|------|
| `cron` | 标准 **5 段** cron (分 时 日 月 周, 不含秒) |
| `channel` | `lark`(飞书)/ `wework`(企业微信) |
| `webhook` | 群机器人完整 webhook 地址 |
| `params` | 固定过滤参数 JSON, 决定这条订阅跑哪份数据 |
| `condition` | 阈值告警条件; 空 = 无条件定时推送 |
| `enabled` | 是否启用 |
| `last_run_at` / `last_status` | 上次触发的整分钟 / 执行结果 (ok 或错误信息) |

**两种触发模式**:

- **定时推送** (`condition` 为空): 到点即推送报表摘要。
- **阈值告警** (`condition` 非空): 对目标区块某列按聚合方式取值, 与阈值比较,
  **命中才推送**, 正文带 `⚠️ 告警:` 前缀。条件结构 `SubCondition`:
  - `block` 目标区块 `Block.ID`(空=首个表格区块)、`column` 目标列、
    `agg` 聚合方式 (`any`/`first`/`sum`/`max`/`min`/`count`)、`op` 比较符
    (`=` `!=` `>` `>=` `<` `<=`)、`value` 阈值。

**报表内嵌波动** (`@data_fluctuations`, 见 SYNTAX.md §5.6): 报表执行时产出的波动
消息会汇总到 `Result.Messages`, 订阅推送时作为 `⚠️ 波动:` 前缀并入正文 —— 与上面的
订阅级 `condition` 告警相互独立, 可叠加。

执行链路: `Execute`(`internal/subscription/runner.go`)取报表 → 跑引擎 →(判定条件)
→ `RenderText` 渲染为纯文本摘要 (限长: 区块/行/列/单元格均有上限) → `push` 按渠道
组装消息体发出。消息可附报表查看链接 (站点地址 `site.url` 已配置时)。

## AI 配置

AI 助手通过 **Eino** (`cloudwego/eino`) 的统一 ChatModel 接口调用模型, 支持两种
provider, **默认优先 claude**:

- `claude` — Anthropic Messages API (`/v1/messages`), 默认模型 `claude-sonnet-4-6`
- `openai` — Chat Completions (`/v1/chat/completions`), 默认模型 `gpt-4o-mini`

配置**只读 `conf.dev.yaml` 的 `ai` 段** (`provider` / `base_url` / `api_key` / `model`),
不再支持环境变量覆盖 —— 避免本机 shell 里残留的 `ANTHROPIC_*` / `OPENAI_*` 误覆盖
配置文件里正确的密钥。示例:

```yaml
ai:
  provider: "claude"                  # claude | openai
  base_url: "https://api.anthropic.com"
  api_key: "sk-xxx"
  model: ""                           # 留空走上面的默认模型
```

实现要点 (`internal/ai/llm.go`):

- `base_url` 可填 provider 根地址或 `/v1` 地址; 代码会规范化到 SDK 期望的根路径
  (Claude 去掉尾部 `/v1`、`/messages`; OpenAI 补全到 `/v1`)。
- 自建/代理网关 (非 `api.anthropic.com`) 会同时带上 `x-api-key` 与 `Authorization`。
- Claude 调用启用 **1 小时 prompt caching** (system / tool / 消息均打 cache 断点)。
- 底层 anthropic-sdk-go 默认 `User-Agent: Anthropic/Go x.y.z` 会被部分 Cloudflare
  网关拦成 403; 共用 HTTP 客户端的 transport 已把 UA 改写为 `focusbi/1.0`。
- 交互为「先流式输出一句话说明, 再用 `propose_template_patch` 工具提交 patch」,
  patch 走 SEARCH/REPLACE 块 (见 `internal/ai/diff.go`), 失配回退整模板重写。

未配置 (base_url/api_key 为空) 时 `/api/report/ai` 返回明确的“未配置”提示。

## MCP 服务 (在 AI 工具中开发报表)

系统内置 **MCP (Model Context Protocol) 服务**, 挂在 `/mcp` (Streamable HTTP),
让 Codex / Claude Code 等 AI 工具直接读语法、探数据源、试跑模板、开发报表。
基于官方 `github.com/modelcontextprotocol/go-sdk`。

**鉴权与权限 (核心)**: 不绕过现有体系。

- 凭证: **API Token** (前缀 `fbt_`, 在控制台「API 令牌」生成; 也兼容直接用登录 JWT)。
  令牌只存 SHA-256 哈希, 明文仅创建时显示一次; 可设过期。
- 请求头 `Authorization: Bearer <token>`; SDK 的 `RequireBearerToken` 中间件校验,
  无效令牌返回 401。令牌解析出用户后, **每个工具按该用户的 RBAC 权限判定** ——
  与 REST 接口同一套权限 (`report.manage:rw`、`dsn:r` 等), AI 不会获得超出本人的权限。

**工具集** (范围: 只读 + 报表开发, 不含用户/角色/数据源增删):

| 工具 | 作用 | 所需权限 |
|------|------|---------|
| `get_syntax_doc` / 资源 `focusbi://syntax` | 报表模板语法权威说明 | 登录 |
| `list_reports` / `get_report` | 列出/读取报表 (按读权限过滤) | 报表读 |
| `list_datasources` / `list_databases` / `list_tables` / `describe_table` | 探数据源 schema (脱敏, 不回连接串/密钥) | `dsn:r` |
| `query_raw` | 只读 SELECT 探数据 (拒非 SELECT/多语句, 限 200 行) | `dsn:r` |
| `preview_template` | 试跑模板返回结构化结果 (含每块 error), 不落库 | `report.manage:rw` |
| `create_report` / `update_report` / `publish_report` | 创建/改草稿/发布 | `report.manage:rw` |

**客户端配置**:

Claude Code (`.mcp.json`):

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

Codex (`~/.codex/config.toml`, 令牌经环境变量传入):

```toml
[mcp_servers.focusbi]
url = "http://127.0.0.1:8099/mcp"
bearer_token_env_var = "FOCUSBI_TOKEN"
```

启动前 `export FOCUSBI_TOKEN="fbt_xxxxx"`; 或用 CLI:
`codex mcp add focusbi --url http://127.0.0.1:8099/mcp --bearer-token-env-var FOCUSBI_TOKEN`。

实现: `internal/mcpserver/` (server/tools/auth), 接入 `api/mcp.go`; 令牌持久层
`dao/api_token.go` + 迁移 `00007`; REST 管理 `api/api_token.go` (`/api/token`)。

## 测试

```bash
go test ./internal/engine/   # 解析/过滤器/宏/图表 单测
```

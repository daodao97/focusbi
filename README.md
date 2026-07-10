# FocusBI

> **让 AI Agent 全自动开发 BI 报表** —— 从探库到上线，5 分钟，零手写 SQL。

## 传统 BI 开发有多痛？

业务提需求："做个本月各渠道销售趋势，按日展示，加个饼图看占比"。

你要做的：
1. 登录数据库，`SHOW TABLES`，找销售表
2. `DESC sales`，确认字段名，采样几行确认数据质量
3. 写 SQL，在本地跑一遍，发现字段名记错了，改
4. 复制到 BI 页面，配置时间过滤器，调日期格式
5. 调图表，金额要千分位，渠道要饼图，日期要折线图
6. 预览，发现合计行不对，回去改 SQL
7. 再预览，图表样式不满意，继续调
8. 终于 OK，保存，发链接给业务
9. 业务说："能再加个周环比吗？" —— 从步骤 3 重新来

**2 小时起步，改一次需求又是半小时。**

## FocusBI 的方式

**你只需一句话：**

```
你: "看下 sales 库，做本月各渠道销售额的日趋势，带饼图占比，发布到运营目录"
```

**AI Agent 自己完成全流程：**

```
Claude Code (5 分钟后):
  ✓ 已连接 sales 数据源
  ✓ 找到 orders 表 (23 个字段), daily_sales 表 (8 个字段)
  ✓ 采样确认: channel 字段是渠道, amount 是金额
  ✓ 已生成报表模板 (日期过滤器 + 趋势图 + 占比饼图)
  ✓ 试跑成功: 30 行数据, 5 个渠道
  ✓ 已发布到「运营/销售分析」目录
  
报表链接: https://bi.example.com/view/abc123
```

**你甚至不用打开数据库。** Agent 自己探库、写模板、试跑、修 bug、发布。

业务说要加周环比？再一句话，agent 读取已有报表、增量修改、自己验证、更新草稿。

---

## 它为什么能做到？

### 1. MCP 工具集 —— Agent 的 12 双手

传统 AI 只能"给建议"。FocusBI 给了 agent **12 个 MCP 工具**，让它真正能动手干活：

| 工具类型 | 工具列表 | 作用 |
|---------|---------|------|
| **探库** | `list_datasources` · `list_databases` · `list_tables` · `describe_table` · `query_raw` | Agent 自己连数据源、看表结构、采样数据 |
| **学语法** | `get_syntax_doc` | 实时获取模板语法规范（过滤器、图表、透视等） |
| **读已有** | `list_reports` · `get_report` | 读取已有报表，做增量修改 |
| **试跑验证** | `preview_template` | **核心**：试跑模板，返回**结构化报错**，agent 自己改 bug |
| **落库发布** | `create_report` · `update_report` · `publish_report` | 创建草稿、修改、发布上线 |

**关键创新：`preview_template` 返回结构化报错**

```json
{
  "blocks": [
    {
      "type": "sql",
      "status": "error",
      "error": "列 'channal' 不存在。你是否想用 'channel'？",
      "sql": "SELECT channal, SUM(amount) ..."
    }
  ]
}
```

Agent 看到报错后，**自己修正拼写错误**，再调 `preview_template`，直到成功 —— 不需要你来回粘贴错误信息。

### 2. 报表是文本 —— 所以 Agent 能理解、能改、能 Diff

传统 BI 的报表是"配置"（JSON/GUI 点击记录），AI 读不懂。FocusBI 的报表是**纯文本**：

```sql
${range|日期|-7 days,today|date_range}

-- @title=每日销售趋势
-- @chart=line
SELECT 
  day AS 日期, 
  SUM(amount) AS 销售额  -- @{"format":"money"}
FROM sales 
WHERE day >= '{range_from}' AND day <= '{range_to}'
GROUP BY day ORDER BY day;

-- @title=渠道占比
-- @chart=pie
SELECT channel AS 渠道, SUM(amount) AS 金额
FROM sales WHERE day >= '{range_from}' AND day <= '{range_to}'
GROUP BY channel;
```

**纯文本的好处：**
- Agent 能**读懂语义** —— 看到 `@chart=line` 就知道要画折线图
- Agent 能**增量修改** —— "加个周环比"，agent 直接在 SQL 后追加一个 `@series` 透视
- 能 **Git 版本管理** —— diff 一目了然，回滚只需一行命令
- 能 **Code Review** —— agent 改完发 PR，你看 diff 批准即可

### 3. 完整的开发到交付闭环

Agent 开发的报表不是玩具，**直接可用于生产**：

| 功能 | 说明 |
|-----|------|
| **草稿/发布分离** | Agent 改草稿，你审核后一键发布。发布自动生成版本快照 |
| **RBAC 权限** | Agent 的每个操作继承令牌所属用户的权限，越权直接报错。不会有"AI 把生产库删了"的风险 |
| **多数据源** | MySQL / PostgreSQL / SQLite，MySQL 支持 SSH 隧道 |
| **定时任务** | Cron 定时跑报表，推送到飞书/企微。支持阈值告警（如"销售额低于 100 万时推送"） |
| **公开分享** | 生成不可枚举分享链接，无需登录即可查看，随时关闭 |
| **双 AI 工作流** | ① MCP 全自动（主线）  ② 页面内对话式改模板（流式 SEARCH/REPLACE 补丁） |

---

## 真实效率对比

| 任务 | 传统方式 | FocusBI + Agent |
|-----|---------|----------------|
| 新建日趋势报表 | 1-2 小时（探库 + 写 SQL + 调样式 + 测试） | **5 分钟**（一句话，agent 自己完成） |
| 增加一个指标 | 30 分钟（改 SQL + 重新调试） | **2 分钟**（"加个周环比"，agent 读已有 + 增量改） |
| 修复字段名错误 | 10 分钟（找错误 + 改 + 重跑） | **自动**（agent 看到结构化报错自己修，不需要你介入） |
| 部署上线 | 人工复制配置、手动测试 | **自动**（agent 调 `publish_report` 一键发布） |

**核心差异：从"AI 辅助"到"AI 全包"。** 你的角色从"搬砖工"变成"审核者"。

---

## 快速开始

### 本地开发

依赖：Go 1.25+、Node 20+ + pnpm、MySQL

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

### 接入 MCP（让 Agent 能操作）

在控制台「API 令牌」生成一个令牌 (`fbt_...`)，然后：

**Claude Code 一键添加：**

```bash
claude mcp add --scope local --transport http focusbi http://127.0.0.1:8099/mcp \
  --header "Authorization: Bearer fbt_xxxxx"
```

**Codex 一键添加：**

```bash
export FOCUSBI_TOKEN="fbt_xxxxx"
codex mcp add focusbi --url http://127.0.0.1:8099/mcp \
  --bearer-token-env-var FOCUSBI_TOKEN
```

之后你就可以对 Claude Code / Codex 说：

- "列出所有数据源"
- "看下 sales 库有哪些表"
- "做一个本月销售趋势报表，发布到运营目录"
- "给报表 #123 加个周环比"

**Agent 会自己完成全流程。你只需审核结果。**

---


## 技术亮点

### 为什么 Agent 能自主修复 Bug？

传统 AI 聊天：

```
你: "这个 SQL 报错了：Column 'channal' not found"
AI: "看起来是拼写错误，应该改成 'channel'"
你: [复制建议，手动改代码，重新跑]
```

FocusBI 的 `preview_template`：

```python
# Agent 内部流程
response = preview_template(content)
if response.blocks[0].error:
    # 结构化报错，agent 直接解析
    error = "列 'channal' 不存在。建议: 'channel'"
    # agent 自己生成修复补丁
    fixed_content = content.replace("channal", "channel")
    # 再次试跑
    response = preview_template(fixed_content)
    # 成功！
```

**关键：结构化报错 + 可重入的试跑接口 = Agent 自我修正闭环。**

### 为什么报表是文本而不是 JSON？

| JSON 配置 | FocusBI 文本模板 |
|----------|----------------|
| `{"chart": {"type": "line", "xAxis": "day"}}` | `-- @chart=line` (agent 一眼能读) |
| 改一个字段要找到嵌套的 JSON 路径 | 直接 `replace("旧列名", "新列名")` |
| Git diff 是 JSON 结构变化，不知道改了啥 | Git diff 直接看到 SQL 和注解变化 |
| Agent 要理解 JSON schema | Agent 用自然语言理解 `@title`、`@chart` |

**文本模板 = Agent 友好 + 人类可读 + Git 友好。**

---

## 架构说明

```
┌─────────────┐
│ Claude Code │  通过 MCP 调用 12 个工具
│   / Codex   │  继承用户令牌的 RBAC 权限
└──────┬──────┘
       │ HTTP /mcp (Streamable HTTP)
       ↓
┌─────────────────────────────────────────┐
│          FocusBI (Go + Vue3)            │
├─────────────────────────────────────────┤
│ MCP Server (internal/mcpserver/)        │
│  → 权限校验 (auth.Permission)            │
│  → 工具路由 (探库/预览/发布...)           │
├─────────────────────────────────────────┤
│ 报表引擎 (internal/engine/)             │
│  → 解析模板 (过滤器 + 宏 + SQL + 注解)    │
│  → 数据管道 (@filter → @sort → @series) │
│  → 返回结构化结果 (含每个 block 的报错)   │
├─────────────────────────────────────────┤
│ 数据源层 (internal/datasource/)         │
│  → 连接池 (MySQL/PG/SQLite)             │
│  → SSH 隧道 (MySQL)                     │
│  → 查询缓存 (@sql_cache)                │
└─────────────────────────────────────────┘
       │
       ↓
   MySQL (主库: 用户/报表/版本)
   Redis (缓存 + 分布式锁)
   报表数据源 (MySQL/PG/SQLite)
```

完整架构见 [`docs/REPORT.md`](docs/REPORT.md)，模板语法见 [`docs/SYNTAX.md`](docs/SYNTAX.md)。

---


## 部署

### 一键安装

```bash
curl -fsSL https://raw.githubusercontent.com/daodao97/focusbi/main/deploy/install.sh | bash
```

安装脚本会询问部署模式：
- **stack**: 生成 FocusBI + MySQL + Redis 完整配置（适合新服务器）
- **external**: 只生成 FocusBI 应用配置，连接已有数据库

脚本只生成配置文件（`.env`、`conf.prod.yaml`、`docker-compose.yml`），不会自动启动服务。确认配置后：

```bash
cd <install_dir>
docker compose pull
docker compose up -d
docker compose logs -f app
```

### 无交互部署

```bash
curl -fsSL https://raw.githubusercontent.com/daodao97/focusbi/main/deploy/install.sh | \
  FOCUSBI_ASSUME_YES=1 \
  FOCUSBI_MODE=stack \
  FOCUSBI_INSTALL_DIR=/opt/focusbi \
  SITE_URL=https://bi.example.com \
  bash
```

更多参数见 [`deploy/README.md`](deploy/README.md)。

### 配置检查清单

上线前必查：

- ✅ `site.jwt_secret` 必须用 `openssl rand -hex 32` 生成，不能是默认占位值
- ✅ `database[default]` / `redis[default]` 地址在容器内可访问（不是 `127.0.0.1`）
- ✅ `site.url` 设为对外访问地址（定时推送用）
- ✅ 如需 AI 改模板功能，配置 `ai.api_key`

完整配置说明：

| 配置项 | 说明 |
|-------|------|
| `site.jwt_secret` | **必填**，登录 token 签名密钥。为空或默认值时拒绝启动 |
| `site.url` | 站点对外地址，用于定时任务里拼报表链接 |
| `database` | 主库 (必须是 MySQL)，保存用户/报表/版本数据 |
| `redis` | 缓存和分布式锁（多实例部署必需） |
| `engine.query_timeout` | 单次 SQL 查询超时，默认 `3m`；可动态覆盖 |
| `engine.query_concurrency` | 单个报表的 SQL 区块并发数，默认 `8`；可动态覆盖 |
| `engine.script_timeout` | 单个脚本区块执行超时，默认 `3m`；可动态覆盖 |
| `engine.script_fetch` | 脚本 `fetch()` 的启动默认策略；运行中可在「系统设置」动态覆盖 |
| `schedule.enabled` | 定时任务运行时开关；`ENABLE_CRON` 决定是否在启动时加载调度器 |
| `security.public_share_enabled` | 是否允许创建和访问公开分享链接，默认开启 |
| `ai` | AI provider (`claude`/`openai`) + `api_key` + `model` |
| `turnstile` | Cloudflare Turnstile 登录人机验证 |

启动时会读取当前目录 `.env`，再加载配置文件，最后用环境变量覆盖。
管理员在「系统设置」保存的动态运行参数存于 `system_setting` 表，优先级高于配置文件，
保存后无需重启；支持 SQL 查询超时/并发数、脚本超时/网络访问、任务调度和公开分享开关，
多实例最多约 5 秒同步。

---

## 常用命令

```bash
make run        # 构建前端 + 启动后端 (dev, :8099)
make web        # 仅构建前端 → web/dist (Go embed)
make web_dev    # 前端 HMR 开发服务器 (代理 /api 到 :8099)
make build      # 生产构建单二进制 → build/server

go test ./...                       # 全部 Go 测试
go test ./internal/engine/          # 报表引擎测试

ENABLE_CRON=true go run ./cmd --app-env dev   # 启用定时任务调度
```

---

## 技术栈

**后端**: Go 1.25+ + Gin + [xgo](https://github.com/daodao97/xgo) (xdb/xredis/xcron/xapp) + goose (迁移) + goja (JS) + Eino (LLM)

**前端**: Vue3 + Vite (MPA) + Element Plus + ECharts + Monaco Editor

**构建**: 前端 `web/dist` 由 Go `embed` 内嵌为单二进制

**数据库**: MySQL (主库，必需) + PostgreSQL/SQLite (报表数据源可选)

**缓存/锁**: Redis (多实例部署必需，单实例可选)

---

## 文档

- **[模板语法规范](docs/SYNTAX.md)** —— 过滤器、宏、注解、图表、透视等完整语法（同时是 AI system prompt）
- **[架构与运维](docs/REPORT.md)** —— 报表引擎、MCP 工具、部署、RBAC 详解
- **[MCP 接入指南](docs/MCP.md)** —— Claude Code / Codex 配置步骤

---

## License

MIT

---

## FAQ

**Q: Agent 会不会误删生产数据？**

A: 不会。每个 MCP 工具调用都继承令牌所属用户的 RBAC 权限。如果用户没有删除权限，agent 也删不了。`query_raw` 工具只读，不允许 `DELETE`/`UPDATE`。

**Q: 报表模板存哪？能版本管理吗？**

A: 存在 MySQL 的 `report` 表，`content` 字段是纯文本。每次发布自动生成版本快照（`report_version` 表），可回滚。建议定期导出模板到 Git 仓库做离线备份。

**Q: 支持哪些图表？**

A: ECharts 5.x 的折线图、柱状图、饼图、散点图、热力图、雷达图等。设置 `@chart=__auto__` 会根据数据类型自动选图表。

**Q: 能对接我们自己的 SSO？**

A: 当前只支持用户名密码登录 + Turnstile 人机验证。需要对接 SSO 可以改 `api/auth.go` 的登录逻辑，签发标准 JWT 即可。

**Q: MCP 工具能跨网络调用吗（agent 在本地，FocusBI 在服务器）？**

A: 可以。只要 agent 能访问 FocusBI 的 `/mcp` 端点（HTTP/HTTPS），配置时填服务器 URL + Bearer token 即可。建议生产环境启用 HTTPS。

**Q: 定时任务支持哪些推送渠道？**

A: 当前支持飞书 webhook 和企业微信 webhook。可以在 `internal/schedule/push.go` 添加更多渠道（钉钉、邮件、Slack 等）。

---

**Star ⭐ 本项目，让更多人看到 AI Agent 全自动开发 BI 的未来。**

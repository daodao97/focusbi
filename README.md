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


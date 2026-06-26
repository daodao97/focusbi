# 在 AI 工具中开发报表 (MCP)

FocusBI 内置 **MCP (Model Context Protocol) 服务**, 让 Codex / Claude Code 等 AI 工具
直接连上系统, 读语法、探数据源、试跑模板、创建并发布报表 —— 像本地有个懂你数据的助手。

> 所有操作都用你的 **API 令牌**鉴权, 并严格继承你本人的权限 (RBAC): AI 能做的事
> 不会超过你自己能做的事。

## 1. 生成 API 令牌

在本页点「生成令牌」, 填个名称 (如 `我的 Claude Code`), 可选有效期 (留空永不过期)。
生成后会**一次性**显示明文令牌 `fbt_xxxxx` —— **请立即复制保存**, 关闭弹窗后无法再次查看。

令牌随时可在本页删除; 删除后使用它的客户端立即失效。

## 2. 配置 AI 客户端

MCP 服务地址是本站的 `/mcp` (Streamable HTTP)。假设本站为
`http://127.0.0.1:8099`, 则服务地址为 `http://127.0.0.1:8099/mcp`
(部署到线上时换成你的域名)。

### Claude Code

编辑项目根的 `.mcp.json` (或全局 `~/.claude.json`):

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

也可用命令添加:

```bash
claude mcp add --transport http focusbi http://127.0.0.1:8099/mcp \
  --header "Authorization: Bearer fbt_xxxxx"
```

### Codex

编辑 `~/.codex/config.toml` (Codex 的 HTTP MCP 令牌走环境变量, 不内联):

```toml
[mcp_servers.focusbi]
url = "http://127.0.0.1:8099/mcp"
bearer_token_env_var = "FOCUSBI_TOKEN"
```

启动 Codex 前设置令牌环境变量:

```bash
export FOCUSBI_TOKEN="fbt_xxxxx"
```

或用命令添加:

```bash
codex mcp add focusbi --url http://127.0.0.1:8099/mcp \
  --bearer-token-env-var FOCUSBI_TOKEN
```

### 其它客户端

任何支持 Streamable HTTP + Bearer 的 MCP 客户端都可接入: 服务地址 `…/mcp`,
请求头 `Authorization: Bearer fbt_xxxxx`。

## 3. 可用工具

连上后, AI 会看到下列工具 (每个都按你的权限校验):

| 工具 | 作用 | 所需权限 |
|------|------|---------|
| `get_syntax_doc` | 获取报表模板语法的完整说明 (写模板前应先读) | 登录 |
| `list_reports` | 列出你有读权限的报表与文件夹 | 报表读 |
| `get_report` | 读取单个报表的模板 (开发版/发布版) 与元信息 | 报表读 |
| `list_datasources` | 列出你可用的数据源 (脱敏, 不含连接串/密钥) | 数据源读 |
| `list_databases` / `list_tables` / `describe_table` | 探数据源的库 / 表 / 列定义 | 数据源读 |
| `query_raw` | 只读 SELECT 探数据 (拒非 SELECT / 多语句, 限 200 行) | 数据源读 |
| `preview_template` | 试跑一段模板返回结构化结果 (含每块错误), 不落库 | report.manage 写 |
| `create_report` | 创建报表或文件夹 | report.manage 写 |
| `update_report` | 更新报表的开发版草稿 / 名称 / 数据源 / 设置 | report.manage 写 |
| `publish_report` | 把开发版草稿发布为正式版 (并记录版本快照) | report.manage 写 |

还提供一个资源 `focusbi://syntax` (同 `get_syntax_doc`, 资源形式)。

## 4. 典型流程

跟 AI 这样说就行, 它会自己调用上面的工具:

1. **了解语法**: "先读 FocusBI 的报表语法" → 调 `get_syntax_doc`。
2. **探数据**: "看看 sales 数据源里有哪些表" → `list_tables` / `describe_table` /
   `query_raw` 试查。
3. **写并校验**: "写一个按渠道统计近 7 天销售额的报表, 带折线图" → AI 生成模板,
   用 `preview_template` 试跑, 按返回的区块错误自动修。
4. **落库发布**: "没问题, 创建并发布" → `create_report` + `publish_report`。

发布后报表即对有权限的查看者可见, 也能配订阅推送。

## 5. 安全与边界

- **权限**: 工具全部按你的 RBAC 判定。比如你只有 `sales` 数据源权限, 让 AI 用
  `finance` 会被拒; 你没有报表写权限, AI 无法创建/改报表。
- **令牌**: 只存哈希, 明文仅生成时可见; 泄漏可随时在本页删除。建议为不同工具/设备
  各建一个命名令牌, 便于按需吊销。
- **只读探查**: `query_raw` 仅允许只读 SELECT 且限行, AI 无法借它改库。
- **写操作不破坏发布版**: `update_report` 只动开发版草稿, 只有 `publish_report` 才更新
  线上版本 —— 改坏了不影响查看者, 且每次发布都有版本快照可回滚。

## 6. 排障

- **连上但调用工具报 401 / Unauthorized**: 先确认 `Authorization: Bearer fbt_...` 配对
  (Codex 是否 `export` 了对应环境变量)。部分 Claude Code 版本存在已知问题: 工具调用经
  SDK 通道时会漏掉 Authorization 头 (直接 curl 正常)。可在会话内用 `/mcp` 看连接状态,
  或升级 / 切换到不受影响的版本。
- **令牌过期**: 设了有效期的令牌到期后需在本页重新生成。
- **看不到某数据源 / 某报表**: 是权限问题 —— 该令牌所属用户没有对应的数据源或报表权限,
  找管理员在「角色管理」里授权。

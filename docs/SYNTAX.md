# 报表模板语法

报表模板是一段文本, 由**多个区块**组成, 引擎解析后执行 SQL 并渲染为表格/图表/markdown。
模板移植自 [dataddy](https://github.com/daodao97/dataddy) 的报表配置理念。

> 本文是报表模板语法的**唯一权威说明**, 同时作为 AI 助手生成/修改模板的依据。

## 1. 总览：一个完整示例

```sql
-- 顶部声明交互过滤器
${range|日期|-7 days,today|date_range}
${channel|渠道||enum(web:网页,app:客户端)}

-- 区块一：每日趋势 (折线图 + 表格)
-- @id=每日趋势
-- @title=每日销售趋势
-- @chart=__auto__
SELECT
    day                       AS 日期,
    SUM(amount)               AS 金额    -- @{"header":"销售额","count":true}
FROM sales
WHERE day >= '{from_range}' AND day <= '{to_range}'
  AND channel = '{channel}'             -- {?channel}
GROUP BY day
ORDER BY day;

-- 区块二：按渠道占比 (饼图)
-- @id=渠道占比
-- @chart=pie:channel,total
SELECT channel, SUM(amount) AS total
FROM sales
WHERE day >= '{from_range}' AND day <= '{to_range}'
GROUP BY channel;
```

要点：
- 过滤器写在前面 (`${...}`)，提供给前端渲染输入控件，并在 SQL 里以宏 `{name}` 引用。
- 每条以 `;` 结尾的 SQL 是一个**表格区块**；区块前的 `-- @key=value` 行是该区块的注解。
- SQL 区块只允许单条只读 `SELECT` / `WITH` 查询；`INSERT` / `UPDATE` / `DELETE` / DDL 及 `INTO OUTFILE` / `LOAD_FILE` 等文件读写会被拒绝。
- `date_range` 过滤器展开为 `{from_range}` / `{to_range}` 两个宏。
- `-- {?channel}` 行尾条件：`channel` 为空时删除该行 (动态过滤)。

## 2. 区块 (Block)

模板按 `;`(行尾分号) 切分为多个区块。区块类型由**内容首部自动判定**：

| 类型 | 判定 | 渲染 |
|------|------|------|
| **table** | 以 `SELECT` 或 `WITH` (不分大小写) 开头 | 表格 (可叠加图表) |
| **script** | 以 `#!SCRIPT` 开头, `#!END` 结束 | 脚本动态产出 (见 §11) |
| **markdown** | 以 `#!MARKDOWN` 开头, `#!END` 结束; **或任何非 SQL 文本** | 渲染为 markdown |
| **raw** | 以 `#!RAW` 开头, `#!END` 结束 | 原样文本 (不转 markdown) |

> 判定顺序：`#!SCRIPT` → `#!MARKDOWN` → `#!RAW` → `SELECT`/`WITH` → 空区块跳过 → 其余
> 一律按 markdown 渲染。所以普通中文说明文字直接写在区块里即可, 不必加 `#!MARKDOWN`。

> SQL 区块、脚本 `query()`、以及 `enum_sql(...)` 选项查询都只执行只读查询。即使以 `WITH`
> 开头, 只要包含 `DELETE` / `UPDATE` / `INSERT` / DDL, 或 `INTO OUTFILE` / `DUMPFILE` /
> `LOAD_FILE` 等文件读写向量, 都会被拒绝。

> **重点 — `#!MARKDOWN` / `#!RAW` 要用 `#!END` 收尾**：这两类区块没有 `;` 结尾, 引擎靠
> `#!END` 知道它在哪结束。**当它后面还有 SQL 区块时, 不写 `#!END` 会把后续 SQL 也吞进
> 说明文字里导致 SQL 不执行**。把说明放在报表开头是常见写法, 务必加 `#!END`：
>
> ```sql
> #!MARKDOWN
> # 销售日报
> 数据 T+1 更新, 金额单位元。
> #!END
>
> SELECT day, amount FROM sales;     -- 这条 SQL 现在能独立执行
> ```
>
> (无 `#!END` 时, 区块会延伸到下一个 `#!` 标记或模板末尾 —— 仅当说明放在**最后**、
> 后面没有 SQL 时才可省略。`#!SCRIPT` 同样以 `#!END` 收尾。)

markdown 区块示例：

```
#!MARKDOWN
## 数据说明
本报表数据 T+1 更新, 金额单位为元。
#!END
```

raw 区块 (原样输出, 适合展示纯文本/已转义内容)：

```
#!RAW
执行时间: {ts}    渠道: {channel}
#!END
```

> markdown / raw 区块同样支持过滤器宏 `{name}` (见 §8), 可把过滤值嵌进说明文字。

## 3. 区块注解 `-- @key=value`

写在区块**行首**(SQL 之前或之间), 控制该区块的标题、图表、汇总等。

| 注解 | 说明 | 示例 |
|------|------|------|
| `@id` | 区块标识 | `-- @id=每日趋势` |
| `@title` | 区块标题 | `-- @title=每日销售` |
| `@subtitle` | 副标题 | `-- @subtitle=单位: 元` |
| `@notice` | 表格上方提示信息 | `-- @notice=数据 T+1 更新` |
| `@dsn` | 覆盖该区块的数据源 | `-- @dsn=sales_db` |
| `@chart` | 图表配置 (见 §4) | `-- @chart=__auto__` |
| `@kpi` | KPI 卡片 (见 §4.1) | `-- @kpi={"items":[{"label":"GMV","value":"销售额"}]}` |
| `@sum` | 显示合计行 | `-- @sum=true` |
| `@avg` | 显示平均行 | `-- @avg=true` |
| `@invisible` | 隐藏表格主体, 仅留图表 | `-- @invisible=true` |
| `@hidden` | 执行并可被脚本引用, 但不渲染 | `-- @hidden=true` |
| `@limit` | 覆盖自动 LIMIT (默认 1000; `0` 不限制) | `-- @limit=500` |
| `@merge_cell` | 纵向合并相邻同值单元格 | `-- @merge_cell=月份,业务线` |
| `@series` | 行转列透视 (见 §5) | `-- @series={"x":"day","series":"channel","value":"amount"}` |
| `@sql_cache` | 查询结果缓存秒数 (0/缺省不缓存) | `-- @sql_cache=300` |
| `@row_tag` | 行级条件样式 (见 §6.1) | `-- @row_tag={"when":"amount<0","class":"row-danger"}` |
| `@filter` | 结果后置过滤 (见 §5.1) | `-- @filter=[["amount",">","100"]]` |
| `@sort` | 服务端排序 (见 §5.2) | `-- @sort=+month,-amount` |
| `@date_line` | 补全缺失日期行 (见 §5.3) | `-- @date_line={"field":"day","start":"-30 days"}` |
| `@flip` | 行列转置 (见 §5.4) | `-- @flip={"key":"product"}` |
| `@join` / `@union` | 与上一区块合并 (见 §5.5) | `-- @join=day` |
| `@data_fluctuations` | 最近两期波动检测 (见 §5.6) | `-- @data_fluctuations={"field":"consume"}` |

补充说明：

- **`@sum` / `@avg`**: 对配置了 `"count":true` 的数值列求和/平均；若没有任何列标
  `count`, 则对所有数值列汇总 (非数值列自动跳过)。合计/平均行第一列显示"合计"/"平均"。
  比率/百分比/已聚合的列用 `-- @{"nosum":true}` 排除, 避免被错误累加。
  > 注意：**不要在 SQL 里再手写合计行** (如 `UNION ALL SELECT '合计' ...) 然后又开
  > `@sum=true` —— 引擎会把你手写的合计行也算进去, 导致**合计翻倍**。二选一即可。
- **`@limit`**: 仅对没有 LIMIT 的 SELECT/WITH 追加, 防止误查全表。
- **`@hidden`**: 适合把某个 SQL block 当作复用数据源。该 block 会正常执行、进入脚本引用表,
  但不会渲染到页面。若执行报错, 错误仍会显示出来, 方便排查。
- **`@merge_cell`**: 层级合并 —— 后列仅当其前置合并列也相同时才合并, 适合月度/分组明细。
- **`@sql_cache`**: 以 `数据源 + SQL` 为键缓存结果, TTL 内重复执行直接命中, 降低库压力。
  前端点击"刷新"和自动刷新会旁路缓存取实时数据, 并把新结果回写缓存 (对其他访问者也生效);
  编辑预览、MCP 预览、定时任务执行始终不走缓存。
- **自动刷新**: 是**报表级**设置 (不是块注解), 在报表编辑器里配置"自动刷新间隔(秒)",
  存于 `report.settings`。加载后每隔 N 秒静默重查 (旁路缓存), 适合监控大屏。
- **顶部 HTML**: 同为**报表级**设置 (`report.settings.prepend_content`), 在报表编辑器
  "报表设置 → 顶部 HTML" 里填一段原始 HTML, 渲染在所有区块之上 (说明/提示/链接)。

### 处理顺序

SQL 执行后, 数据变换按固定管线进行 (与书写顺序无关)：

```
SQL → @filter → @date_line → @sort → @series(透视) → @flip(转置)
    → @join/@union(区块合并) → @data_fluctuations(波动检测)
    → 列配置(enum/percent/tag/date…) → @sum/@avg
```

`@flip` 会重构表结构, 转置后 `@sum`/`@avg` 自动跳过。
`@join`/`@union` 跨区块合并, 波动检测在合并后的最终数据上进行。

## 4. 图表 `@chart`

| 写法 | 含义 |
|------|------|
| `__auto__` | 第一列为 X 轴, 其余数值列为多条折线 |
| `line:列1,列2` | 折线图, 指定数值列 (X 轴取第一列) |
| `bar[x=列1/列2]:数值列…` | 显式指定 X 轴 (可多维, `/` 分隔); 多个维度列拼成类目 |
| `bar:列1,列2` | 柱状图 |
| `area:列1,列2` | 面积图 (填充折线) |
| `scatter:x列,y列` | 散点图 (两个数值轴, 看相关性) |
| `pie:分类列,数值列` | 饼图 |
| `funnel:分类列,数值列` | 漏斗图 (逐级收窄, 看转化) |
| `gauge:数值列` | 仪表盘 (取数据**末行**该列为当前值, 看达成率) |
| `radar:列1,列2,…` | 雷达图 (各列为一个维度, 每行一组指标) |

```sql
-- @chart=pie:channel,total
SELECT channel, SUM(amount) AS total FROM sales GROUP BY channel;
```

- `line`/`bar`/`area` 不写列名时默认：第一列作 X 轴、其余所有数值列作序列。
- `__auto__` 也可写作 `auto`。
- **多维 X 轴**：表格有多个维度列时 (如 `服务类型 + 处理方式`)，默认只取第一列作 X 轴会
  和表格行对不上。用 `bar[x=服务类型/处理方式]:总数,成功数` 显式指定 (多维 `/` 分隔，前端把
  几个维度拼成一个类目)。不写数值列时，序列默认取剩余非 X 列。对象式等价写法
  `@chart={"type":"bar","x":["服务类型","处理方式"],"series":["总数"]}` (x 为数组即多维)。
  若 X 轴出现重复值，报表会在表格上方提示 (重复会导致图表与表格对不上)。
- 也支持对象写法 `@chart={"type":"bar","x":"day","series":["pv","uv"]}`。兼容键：
  X 轴 `x`/`xAxis` (字符串=单维, 数组=多维); 序列 `series`/`y`/`yAxis`; 散点 y 轴 `y`;
  饼图/漏斗分类 `name`/`category`、数值 `value`; 仪表盘数值 `value`。
- **堆叠**：`bar`/`area` 对象式加 `"stack":true` 即堆叠 (如
  `@chart={"type":"bar","x":"day","series":["pv","uv"],"stack":true}`)。
- 配合 `@invisible=true` 可只显示图表、隐藏表格主体。
- 前端用 ECharts 渲染, 序列值按数值解析 (非数值按 0 处理)。
- **图表用原始数值**：图表、排序、汇总都使用 SQL 查出的**原始数值**；列配置 `format`
  (`money`/`integer`/`percent` 等) 只影响**表格单元格的展示**，不会改变图表/排序/汇总的计算值。

## 4.1 KPI 卡片 `@kpi`

把核心指标渲染成一排**计分卡**（大数字 + 同环比 + 迷你趋势线），适合报表顶部的概览区。
与 `@chart` 平行：**复用同一条 SQL 的结果**，不另外取数。值为对象 `{"items":[...]}`，
每个 item 是一张卡片：

```sql
-- @id=核心指标
-- @kpi={"items":[
--   {"label":"GMV","value":"销售额","compare":"上期","format":"money","trend":"销售额"},
--   {"label":"订单数","value":"订单数","compare":"上期订单","format":"integer"}
-- ]}
-- @invisible=true
SELECT day AS 日期, 销售额, 上期, 订单数, 上期订单 FROM ...
ORDER BY day;
```

item 字段（`value`/`compare`/`trend` 均为**列名**，引擎不预算、前端按列名从结果行自取）：

| 键 | 必填 | 作用 |
|----|------|------|
| `label` | | 卡片标题（缺省取 `value` 列名） |
| `value` | ✓ | 当前值列名，取**数据末行**该列 |
| `compare` | | 对比基准列名，算同环比 `(当前-基准)/基准`，▲绿/▼红 + 百分比 |
| `format` | | `money`/`number`/`integer`/`percent`（口径同列配置 `format`，见 §6） |
| `trend` | | sparkline 取值列名，按整列序列画迷你折线 |
| `unit` | | 数字后缀单位 |

- **当前值取末行**：建议 SQL 按日期**升序**，使末行为最新期；趋势线按行序从左到右。
- **同环比口径**：基准为 0 记 `+100%`，当前值缺失记"数据缺失"（对齐 §5.6 波动检测）。
- **KPI 与表格/图表可并存**；只想要卡片就加 `@invisible=true` 隐藏表格主体。
- 脚本里用 `result.table({kpi:{items:[...]}})` 同样产出（见 §11）。

## 5. 行转列透视 `@series`

把"长表"展开成"宽表", 一条 SQL 出多序列趋势图。

```sql
-- @id=分渠道趋势
-- @series={"x":"day","series":"channel","value":"amount"}
-- @chart=__auto__
SELECT day, channel, amount FROM sales ORDER BY day;
```

`(day, channel, amount)` 多行 → 每个 `day` 一行, 各 `channel` 成列 (缺失补 0)：

| day | web | app |
|-----|-----|-----|
| 2026-06-20 | 120 | 80 |
| 2026-06-21 | 150 | 95 |

键名：`x`(横轴) / `series`(展开为列名的字段) / `value`(填充值的字段)。
兼容 dataddy 键名 `xAxis` / `series`(数组) / `series_value`(数组)。

## 5.1 结果后置过滤 `@filter`

SQL 跑完后再按条件过滤行, 免改 SQL (常配合过滤器宏)。值为条件二维数组, **多条件 AND**：

```sql
-- @filter=[["status","=","active"],["amount",">","100"]]
SELECT status, amount FROM orders;
```

每条 `[字段, 操作符, 值]`, 操作符：

| 操作符 | 含义 | 值格式 |
|--------|------|--------|
| `=` / `!=` | 等于 / 不等于 (别名 `==`/`is`、`<>`) | 单值 (字符串比较) |
| `>` `>=` `<` `<=` | 数值比较 (非数值回退字符串) | 单值 |
| `in` / `not in` | 在/不在列表中 | 逗号分隔 `a,b,c` |
| `between` | 开区间 `(lo, hi)` | `lo,hi` |

> 未识别的操作符不过滤 (该条件视为恒真)。

> 缺失字段按空值参与比较。数值可比时按数值, 否则按字符串。

## 5.2 服务端排序 `@sort`

跨页正确的多字段排序 (前端 el-table 只能排当前页)。逗号分隔多个排序键, 前者优先：

```sql
-- @sort=+month,-amount          升序 month, 同值再降序 amount
-- @sort=-amount(dept>branch)    先按 dept 组总额、再按 branch 组总额聚拢, 组内降序
```

`+` 升序 / `-` 降序 (缺省降序)。括号内为**分组字段** (`>` 分层): 该键先按"组内对排序字段求和"
得到的组权重比较, 让同组的行聚在一起、组按总量排序; 组内再按字段值排。排序稳定。

## 5.3 日期补全 `@date_line`

给趋势数据补齐缺失日期行, 避免折线断裂：

```sql
-- @date_line={"field":"day","start":"-30 days","format":"Y-m-d"}
-- @chart=__auto__
SELECT day, SUM(amount) AS amount FROM sales GROUP BY day ORDER BY day;
```

- `field`: 日期列名 (默认 `day`, 兼容 `date`)。
- `start`: 起始日期。相对偏移 (`-30 days` 等) 基于今天; 也可写绝对日期; 缺省取数据最早日期。
- `format`: PHP 风格格式 (默认按数据首行推断)。含 `H` 按小时步进, 否则按天。

> 区间为 `[start, 数据最晚日期]`。补全行只含日期字段, 其余列为空 (图表按缺失/0 处理)。

## 5.4 行列转置 `@flip`

把"少列多行"转成"多列少行", 便于横向对比：

```sql
-- @flip={"key":"product"}
SELECT product, q1, q2, q3 FROM sales;
```

`key` 指定保留为行标签的列 (逗号分隔多列); 其余每列转成一行, 新表首列"名称"为原列名,
数据列名取 key 值 (形如 `product[A]`; 多 key 用逗号拼接 `product[A],channel[web]`)。
不指定 key (裸 `-- @flip`) 时列名取 `列1`/`列2`…; 列名重复时追加 `.N` 序号去重。
**行数上限 50**, 超出则放弃转置。转置后 `@sum`/`@avg` 跳过。

## 5.5 区块合并 `@join` / `@union`

把**多个 SQL 区块的结果合并成一张表**。第一个区块作为**基底**(不标注),
其后**紧邻**的区块标 `@join` 或 `@union` 即依次并入基底, 最终只产出一个合并区块。
基底区块的展示注解 (`@chart` / `@sum` / `@title` 等) 对合并结果生效。

**横向连接 `@join`** —— 按键把另一区块的列拼到基底 (类似 SQL LEFT JOIN)：

```sql
-- @id=每日概览
-- @chart=__auto__
SELECT day, pv FROM pv ORDER BY day;

-- @join=day
SELECT day, orders FROM orders ORDER BY day;
```

结果是 `day, pv, orders` 一张宽表。`@join` 的几种写法：

| 写法 | 含义 |
|------|------|
| `-- @join=day` | 按 `day` 列左连接 |
| `-- @join=day,channel` | 按多列左连接 |
| `-- @join` | 不指定键 —— 取两区块的列交集为连接键 |
| `-- @join={"on":"day","full":true}` | 全连接 (并入右区块未匹配的行) |

左连接时基底未匹配到的行, 新列补空; `full:true` 时右区块独有的行也并入 (基底独有列补空)。

**纵向合并 `@union`** —— 把另一区块的行追加到基底下方 (类似 SQL UNION ALL)：

```sql
-- @id=渠道汇总
SELECT '直播' AS 渠道, amount FROM live;

-- @union
SELECT '短视频' AS 渠道, amount FROM video;
```

列取并集, 某区块缺的列补空。适合把结构相同的多段结果叠成一张表。

> 注意: `@join` / `@union` 标在**被并入的区块**上, 不是基底。一旦遇到未标注的区块,
> 当前合并组即结束。

## 5.6 数据波动检测 `@data_fluctuations`

对**时序表**比较最近两期 (按**首列**降序取前两行) 的环比波动, 超过阈值即产出一条
波动消息。消息会进入**定时任务**正文 (见 REPORT.md 定时任务章节), 用于异动告警。

```sql
-- @id=每日消耗
-- @data_fluctuations={"field":["consume","orders"],"threshold_percent":50}
SELECT stat_date, consume, orders
FROM daily_finance
ORDER BY stat_date DESC;
```

| 配置 | 说明 |
|------|------|
| `field` | 要监控的字段, 字符串或字符串数组 |
| `threshold_percent` | 波动阈值百分比, 缺省 `50` |

波动按 `(最新 - 上期) / 上期` 计算, 绝对值超过阈值的字段汇总成一条形如
`2026-06-24 数据相较 2026-06-23 浮动: consume: 100 => 300 [+200%]` 的消息。
最新期该字段缺失或为 0 记为"数据缺失" (消息形如 `consume: 数据缺失`); 上期为 0 时按 `+100%` 计;
表不足两行则不检测。

> 检测只产出消息, **不改变表格展示**。消息仅在定时任务时随正文发出。

## 6. 列配置 `-- @{...}`

跟在某列 `AS 别名` 之后的 JSON, 控制该列的展示与计算。

```sql
SELECT
    status      AS 状态,    -- @{"enum":"1:成功,0:失败"}
    amount      AS 金额,    -- @{"header":"销售额","count":true,"round":2}
    rate        AS 转化率,  -- @{"ratio":1}
    uid         AS 用户     -- @{"href":"/#/user?id={uid}","tooltip":"点击查看用户"}
FROM orders;
```

| 键 | 作用 | 示例 |
|----|------|------|
| `header` | 重命名列标题 | `{"header":"销售额"}` |
| `tooltip` | 列标题悬浮说明 | `{"tooltip":"单位: 元"}` |
| `count` | 参与合计/平均 (配合 `@sum`/`@avg`) | `{"count":true}` |
| `nosum` | **排除**该列出合计/平均 (比率/已聚合列) | `{"nosum":true}` |
| `enum` | 值映射为标签 | `{"enum":"1:成功,0:失败"}` |
| `ratio` | 值 / ratio × 100 并加 `%` | `{"ratio":1}` (0.25 → 25.00%) |
| `round` | 保留 N 位小数 | `{"round":2}` |
| `format` | 快捷数值格式化 | `{"format":"money"}` |
| `date` | 日期/时间字符串重排格式 (PHP 风格) | `{"date":"Y-m-d"}` |
| `time2str` | Unix 时间戳 (秒) 转日期字符串 | `{"time2str":"Y-m-d H:i:s"}` |
| `percent` | 条件百分比 (值/base×100, 按阈值变色) | `{"percent":{"base":"total","succ":70,"warn":40}}` |
| `href` | 单元格渲染为链接, `{字段}` 取本行值 | `{"href":"/#/d?c={channel}"}` |
| `tag` | 单元格按值渲染为彩色标签 (el-tag) | `{"tag":"1:success:已完成,0:danger:失败"}` |

### 单元格标签 `tag`

按单元格的**原始值**匹配, 渲染为带颜色的标签。语法 `值:类型[:文本]`, 逗号分隔多条；
`default` 作为兜底键 (未命中其它值时使用)。类型取 element-plus 语义色：
`success` / `warning` / `danger` / `info` / `primary`。省略文本时显示原值。

```sql
SELECT
    state AS 状态   -- @{"tag":"1:success:已完成,0:danger:失败,default:info"}
FROM orders;
```

> `tag` 按原始值匹配, 与 `enum` 互斥使用 (都改写单元格显示); 若同列同时配置, 以 `tag` 标签为准。

### 条件百分比 `percent`

把数值列算成百分比并按阈值上色, 适合"完成率/达标率"这类指标。配置：

- `base`: 分母。字符串 → 取**同行某列**的值 (如 `"total"`); 数值 → 常量 (如 `100`)。
- `succ` / `warn`: 阈值。`>=succ` 绿(success), `>=warn` 橙(warning), 否则红(danger)。
  两者都省略时不上色 (仅显示百分比)。
- `dot`: 小数位数 (默认 0)。

```sql
SELECT
    finished,                                   -- 已完成数
    total,                                      -- 总数
    finished AS 完成率  -- @{"percent":{"base":"total","succ":80,"warn":50,"dot":1}}
FROM tasks;
```

> `base` 列若为 0 或非数值, 该单元格跳过 (保持原值不变)。百分比标签底层复用单元格标签机制。

### 日期格式化 `date` / `time2str`

- `date`: 解析日期/时间字符串后按 PHP 风格 token 重排, 无法解析则原样保留。
- `time2str`: 把 Unix 时间戳 (秒) 转为日期字符串。

格式 token: `Y`=4位年 `y`=2位年 `m`=月 `d`=日 `H`=时 `i`=分 `s`=秒, 其余字面输出。

```sql
SELECT
    created_at  AS 创建时间  -- @{"date":"Y-m-d H:i"}
    ,login_ts   AS 登录时间  -- @{"time2str":"Y-m-d H:i:s"}
FROM logs;
```

### 数值快捷格式 `format`

把数值列按常见样式格式化, 免去手写 `ratio`/`round`：

| 取值 | 效果 | 示例 |
|------|------|------|
| `money` / `currency` | 千分位 + 2 位小数 | `1234.5` → `1,234.50` |
| `number` | 千分位 (整数去尾零) | `1234.5` → `1,234.5` |
| `integer` / `int` | 千分位整数 (0 位小数) | `1234.6` → `1,235` |
| `percent` / `percentage` | 值 ×100 加 `%` | `0.25` → `25.00%` |

```sql
SELECT
    amount  AS 金额    -- @{"format":"money"}
    ,rate   AS 占比    -- @{"format":"percent"}
FROM orders;
```

> 非数值单元格原样保留。`format` 与 `enum`/`date`/`time2str`/`ratio`/`round` 互斥, 命中
> 优先级为 `enum → date → time2str → format → ratio → round`。脚本 `result.table` 的
> `formats` 简写 (见 §11) 即此机制的列名映射版。

## 6.1 行级标签 `@row_tag`

给**整行**按条件追加 css class (移植自 dataddy 行属性), 用于高亮异常/重点行。
块注解, 值为一条规则对象或规则数组：

```sql
-- @row_tag={"when":"amount>=10000","class":"row-success"}
SELECT day, amount FROM sales;

-- 多条规则 (命中则追加, class 以空格拼接)
-- @row_tag=[{"when":"profit<0","class":"row-danger"},{"when":"vip==1","class":"row-bold"}]
SELECT day, profit, vip FROM stat;
```

`when` 形如 `字段 运算符 值`, 运算符支持 `== != > >= < <=`；数值可比时按数值, 否则按字符串。
省略 `when` 表示匹配所有行。内置预设 class：`row-success` / `row-warning` / `row-danger` /
`row-info` (整行底色) 与 `row-muted` (灰字) / `row-bold` (加粗)。

## 7. 过滤器 `${...}`

声明交互式输入。语法：`${名称|标签|默认值|类型}`

```sql
${range|日期|-7 days,today|date_range}
${status|状态|1|enum(1:成功,0:失败)}
${kw|关键词||string}
```

### 7.1 过滤器类型

| 类型 | 控件 | 宏展开 |
|------|------|--------|
| `string` | 文本框 | `{name}` |
| `number` | 数字框 | `{name}` |
| `bool` | 开关 | `{name}` (1 或空) |
| `enum(v1:标签1,v2:标签2)` | 下拉 (静态选项) | `{name}` |
| `enum.multiple(...)` | **多选**下拉 (见 7.4) | `{name}` = SQL in-list |
| `enum_sql(SQL)` | 下拉 (**选项查库**, 见 7.3) | `{name}` |
| `enum_sql.multiple(SQL)` | 多选下拉 + 选项查库 | `{name}` = SQL in-list |
| `date` | 日期选择 | `{name}` |
| `time` | 日期时间选择 | `{name}` |
| `date_range` | 日期范围 | `{from_name}` + `{to_name}` |
| `time_range` | 日期时间范围 | `{from_name}` + `{to_name}` |

> **重点**: `date_range` / `time_range` 不产生 `{name}`, 而是展开为 `{from_name}` 与
> `{to_name}` 两个宏, 在 SQL 里这样用：
> `WHERE day >= '{from_range}' AND day <= '{to_range}'`

`enum` 选项写在括号里, `值:标签` 逗号分隔：`enum(1:成功,0:失败)`。未识别的类型一律
按 `string` 处理。

> **兼容 dataddy 后缀**: 类型后可带 `.macro` / `.raw` / `.month` 等修饰
> (如 `date.month.macro.raw`), 解析时取第一段为基础类型, 多余后缀忽略 —— 便于直接粘贴
> dataddy 模板而不报错。其中 **`.multiple` 会被识别为多选** (见 §7.4), 不再忽略。

### 7.3 动态下拉选项 `enum_sql`

下拉选项来自一条 SQL 查询 (而非写死), 适合"地区/用户/渠道"等维度从库里取:

```sql
${region|地区||enum_sql(SELECT id AS value, name AS label FROM regions ORDER BY name)}
${owner|负责人||enum_sql[crm](SELECT uid AS value, nick AS label FROM users)}
```

- 查询取 **value / label 两列** 作为选项的值与显示文本; 若结果列名含 `value`/`label`
  则按名取, 否则取**前两列** (单列时 value=label)。
- `enum_sql[dsn](...)`: 方括号里指定**独立数据源** (维度表在别的库时用); 省略则用报表数据源。
- 选项在报表加载时查一次填充; 查询出错则选项为空, 不影响报表其余部分。
- 用法与 `enum` 一致, SQL 里照常用 `{region}` 引用所选值。

> 比脚本更简单: 90% 的"动态下拉"就是查一张维度表, 用 `enum_sql` 一行搞定。需要更复杂
> 的选项逻辑 (依赖外部数据 / 任意计算) 时, 用脚本的 `result.filter()` (见 §11)。

### 7.4 多选 `.multiple`

给 `enum` / `enum_sql` 加 `.multiple` 后缀即多选, 选项来源 (静态 / 查库 / 脚本) 都支持:

```sql
${ch|渠道||enum.multiple(web:网页,app:客户端,mini:小程序)}
${region|地区||enum_sql.multiple(SELECT id AS value, name AS label FROM regions)}
```

多选时 `{name}` **展开为 SQL in-list** (`'web','app'`), 在 SQL 里配合 `IN` 使用。
**推荐写法** —— 用 `-- {?name}` 行条件让"未选 = 不过滤":

```sql
SELECT * FROM sales WHERE 1=1
  AND channel IN ({ch})   -- {?ch}
```

- **未选时整行被删除** (等价于不加这个过滤, 即全部命中); 有选时展开为 `channel IN ('web','app')`。
  `{?name}` 对多选已能正确判空 (看真实选择而非 SQL 编码值), 无需再写
  `'{ch_raw}' = '' OR channel IN ({ch})` 这类绕法。
- 若**不**用 `-- {?ch}` 行条件而直接 `IN ({ch})`: 未选时 `{ch}` 为 `''` (即 `IN ('')`, 不命中而非
  语法错) —— 这通常**不是**你想要的 (会过滤掉所有行), 故推荐上面的行条件写法。
- 另提供 `{name_raw}` = 原始逗号串 (`web,app`), 供需要原值的场景。
- 值里的单引号会被转义, 防止经宏拼接注入。
- 脚本里用 `result.filter({name, type:'enum', multiple:true, options:[...]})`。

### 7.2 默认值 (相对日期)

默认值支持相对表达式, 在解析时换算成具体日期 (按服务器当前时间)：

| 表达式 | 含义 |
|--------|------|
| `today` / `now` / 空 | 今天 |
| `yesterday` | 昨天 |
| `tomorrow` | 明天 |
| `this month` | 本月第一天 |
| `±N day(s)` | N 天前/后, 如 `-7 days` |
| `±N week(s)` | N 周前/后 |
| `±N month(s)` | N 月前/后 |
| `±N year(s)` | N 年前/后 |

非相对表达式 (已是 `2026-06-01` 这类绝对值) 原样保留。

`date_range` 默认值用逗号分隔起止：`-7 days,today` → `2026-06-18,2026-06-25`。
前端首次渲染即用解析后的默认值回填控件。

### 7.3 日期粒度/格式 `type[format]`

在日期类型后加 `[格式]` 控制粒度, 格式为 PHP 风格 token, 前端据此渲染对应控件：

```sql
${month|统计月份|-1 months,today|date_range[Y-m]}   -- 按月范围, 月份选择器
${day|日期|today|date[Y-m-d]}                         -- 单日
${ts|时间|now|time[Y-m-d H:i:s]}                      -- 日期时间
```

例如 `date_range[Y-m]` 默认 `-1 months,today` → `{from_month}`=`2026-05`、
`{to_month}`=`2026-06`。无 `[format]` 时 date/date_range 默认 `Y-m-d`,
time/time_range 默认 `Y-m-d H:i:s`。

## 8. 宏 `{name}` 与条件

- **取值**: `{name}` 替换为过滤器当前值。字符串值通常自己加引号：`'{kw}'`;
  数字/日期/`raw` 修饰的值可不加引号。宏在执行前已内联展开成字面量。
- **注入防护**: `{name}` 的值会自动做 SQL 单引号转义 (`'` → `''`), 用户传入的
  `x' OR '1'='1` 拼进 `'{kw}'` 后仍是字面量, 无法越出引号。`number` 型强制数值校验、
  `date`/`time`/`date_range`/`time_range` 型强制日期/时间字面量格式校验 (非法输入一律置空)。
  **字符串型 (`string`/单选 `enum`) 务必按上面的写法加引号 `'{name}'`**; 裸写 `{name}`
  (无引号) 只对 number/date/time 这类已校验类型安全。
- **`{name[raw]}` 原始值**: 显式取未转义的原始值 (如把过滤器值当动态表名/列名拼接)。
  这是绕过转义的逃生口, **只应用于作者可信的场景**, 不要对开放分享报表的自由文本过滤器用。
- **逗号组合 `{a,b}`**: 取第一个存在的过滤器值 (回退用), 如 `{uid,default_uid}`。
- **行级条件** (写在**行尾注释**, 满足时删掉整行)：
  - `-- {?name}` —— `name` **为空**时删除该行 (动态列 / 动态条件)
  - `-- {?!name}` —— `name` **非空**时删除该行
  - 条件也支持逗号：`-- {?a,b}` 当 a、b 都为空时删除

> **"空"的判定**: 值为 空串 / `0` / `false` 都视为"空"。所以 `bool` 过滤器 (取值 1 或空)
> 配 `-- {?flag}` 能很自然地做"勾选才生效"的开关。

```sql
${show_income|显示收入|0|bool}
${channel|渠道||string}

SELECT
    id,
    income          -- {?show_income}    未勾选"显示收入"则不查这列
FROM t
WHERE 1=1
  AND channel = '{channel}'  -- {?channel}    channel 为空则不加此条件
```

### 8.1 宏格式化与日期调整 `{name[modifier|format]}`

宏占位的中括号部分可对值 (主要是日期) 调整与格式化。

```sql
${month_start|统计月份|today|date}
WHERE created_at >= '{month_start[Y-m-01]}'   -- 值 2026-06-25 → 2026-06-01 (当月第一天)
```

| 写法 | 输入 `2026-06-25` 输出 | 说明 |
|------|------|------|
| `{m[Y-m-01]}` | `2026-06-01` | 仅格式化 (`01`/`-` 为字面量) |
| `{m[Y-m]}` | `2026-06` | |
| `{m[first_day_of_month\|Y-m-d]}` | `2026-06-01` | 先调整再格式化 |
| `{m[last_day_of_month\|Y-m-d]}` | `2026-06-30` | 2 月会得 28/29 |
| `{m[first_day_of_year\|Y-m-d]}` | `2026-01-01` | |
| `{m[+1 month\|Y-m-01]}` | `2026-07-01` | |
| `{m[-1 day\|Y-m-d]}` | `2026-06-24` | |
| `{m[raw]}` 或无修饰 | `2026-06-25` | 原值 |

- **格式 token** (PHP date 风格)：`Y`=4位年 `y`=2位年 `m`=2位月 `n`=月 `d`=2位日
  `j`=日 `H`=时 `i`=分 `s`=秒；其余字符 (含数字) 原样输出, 故 `Y-m-01` 里的 `01` 是字面量。
- **调整指令** (可逗号串联)：`first_day_of_month`/`month_start`、
  `last_day_of_month`/`month_end`、`first_day_of_year`、`last_day_of_year`、
  `±N day(s)`/`week(s)`/`month(s)`/`year(s)`。无 modifier 时直接格式化。
- 非日期值原样返回 (不报错)。

## 9. 多数据源与 SQL 方言

报表有默认数据源, 区块可用 `-- @dsn=name` 覆盖。SQL 直接发往目标库, **方言需按目标
库书写** (如 MySQL 用 `DATE_FORMAT`, Postgres 用 `to_char`)。过滤器宏在执行前已内联
展开成字面量, 不使用占位符, 故 `?`/`$1` 差异不影响。

## 10. 编写要点 (给 AI 与作者)

1. 过滤器先声明再在 SQL 里用宏引用；`date_range` 记得用 `{from_x}`/`{to_x}`。
2. 需要合计/平均时, 给数值列加 `"count":true` 并配 `@sum`/`@avg`。
3. 多序列趋势图优先用 `@series` 透视 + `@chart=__auto__`, 一条 SQL 搞定。
4. 动态列/动态条件用行尾 `-- {?name}` / `-- {?!name}`。
5. 取当月/上月/月末等用宏格式化 `{m[...]}`, 别在 SQL 里硬写日期。
6. 列要重命名用中文标题时, 既可 `AS 中文名`, 也可 `-- @{"header":"中文名"}`。

## 11. 脚本区块 `#!SCRIPT` (动态报表)

当声明式注解不够用时 (动态拼 SQL、按参数分支、自定义列计算、按数据生成多个区块、
外部 API 数据), 可写一段 **JavaScript** 动态产出报表。脚本区块用 `#!SCRIPT` 起、`#!END` 止,
与普通 SQL/markdown 区块**并存**:

> 脚本块内是纯 JS, **不受报表宏/过滤器解析影响** —— JS 模板字符串 `` `x ${value}` `` 里的
> `${...}` 不会被当成报表过滤器, 对象字面量 `{...}` 也不会被当成宏。正常写 JS 即可。


```
#!SCRIPT
const rows = query(
  'SELECT channel AS ch, SUM(amount) AS amt FROM sales WHERE day = ? GROUP BY channel',
  [params.day]
)
result.table({
  title: '渠道汇总 ' + params.day,
  columns: [{name:'ch', header:'渠道'}, {name:'amt', header:'金额'}],
  rows: rows.map(r => ({...r, 占比: (r.amt / 1000).toFixed(1) + '%'}))
})
#!END
```

脚本不 `return`; 通过 `result.*` 声明产出, **顺序即展示顺序**, 可产出任意多个区块。
脚本可通过 `dataset(id)` / `block(id)` 引用**前面已经执行完成**的 block; 后面的 block 暂不能被提前引用。

### 可用 API

| API | 说明 |
|-----|------|
| `params` | 查看者提交的过滤器**原始值** (字符串), 如 `params.day`; 多选为逗号串 `"web,app"` |
| `setParam(name, value)` | 写回派生参数, 供后续 SQL 的 `{name}` 宏 / 条件行 `{?name}` 使用; **仅在 `@setup` 前置脚本里有效** (见 §11.1) |
| `dataset(id)` | 按前面已执行 block 的 `@id` 读取 rows 副本 |
| `block(id)` | 按前面已执行 block 的 `@id` 读取 `{id,type,title,subtitle,notice,columns,rows,summary,average,chart,sql,error,invisible,hidden,merge_cell}` 副本 |
| `query(sql, args?, dsnOrOptions?, options?)` | 执行 SQL 返回行数组; `args` 为 `?` 占位参数 (**参数化, 防注入**); 可覆盖数据源和缓存 |
| `where(obj)` | 把条件对象拼成参数化 WHERE 片段, 返回 `{sql, args}` (空值自动跳过, 见下) |
| `result.table({id,title,subtitle,notice,columns,rows,chart,kpi,formats,sum,avg,row_tag,invisible,hidden,merge_cell})` | 产出表格区块; 列配置/图表/KPI/汇总/行样式同声明式 (见下) |
| `result.markdown(text)` | 产出 markdown 区块 |
| `result.chart(cfg, rows?)` | 产出图表区块; `cfg` 可带 `id/title/subtitle/notice/columns/formats/sum/avg/invisible/hidden/rows` (见下) |
| `result.filter({name,label,type,options,default,multiple})` | 动态产出**过滤器** (下拉选项可由脚本任意计算; `multiple:true` 为多选, 见下) |
| `cache.get(key)` / `cache.set(key, value, ttlSec)` | 自定义 KV 缓存脚本计算结果 (进程内); `get` 未命中返回 `undefined` (见下) |
| `now()` | 当前时间 (RFC3339 字符串) |
| `formatDate(value, fmt)` | 日期格式化 (PHP 风格, 见 §6) |
| `fetch(url, opts?)` | HTTP 外呼, 返回 `{status, body, json()}` |
| `log(...)` | 调试输出标量 (产出一个 raw 区块, 仅作者可见) |
| `dump(...)` | 调试输出**对象/数组/query 结果**, 结构化美化 JSON; 首参为字符串则当标签, 如 `dump('rows', rows)` |

### 动态拼 SQL 与 `where()`

`query()` 第一个参数就是 SQL 字符串, 可在 JS 里动态拼:

```
#!SCRIPT
// 动态选维度 (列名不能参数化 -> 走白名单)
const dims = { '渠道':'channel', '地区':'region' }
const dim = dims[params.groupBy] || 'channel'
const rows = query(`SELECT ${dim} AS 维度, SUM(amount) AS 金额 FROM sales GROUP BY ${dim}`)
result.table({ title: '按' + params.groupBy + '汇总', rows })
#!END
```

可选过滤条件多时, 用 `where()` 自动跳过空值、生成 `?` 占位、收集参数:

```
#!SCRIPT
const w = where({
  region: params.region,    // 空则跳过
  'day >=': params.day,      // 显式操作符
  status: [1, 2]             // 数组 -> IN (?,?)
})
const rows = query(`SELECT * FROM sales WHERE ${w.sql}`, w.args)
result.table({ rows })
#!END
```

- **键**: `"field"` (默认 `=`) 或 `"field op"` (op ∈ `= != <> > >= < <= like in "not in"`)。
- **值**: 空 (`undefined`/`null`/`""`/空数组) 的条件**自动跳过** —— 实现可选过滤器; 数组值转 `IN (?,...)`。
- 无任何有效条件时 `w.sql` 为 `1=1`, 可安全嵌入 `WHERE`。
- `where()` 只负责拼**值条件** (参数化); 列名/表名等结构仍需自己白名单拼接。

脚本里的 `query()` 默认不缓存。需要缓存时, 在第三或第四参数传选项:

```
const rows = query(
  'SELECT day, amount FROM sales WHERE day >= ?',
  [params.day],
  { sql_cache: 300 }                 // 缓存 300 秒
)

const rows2 = query(
  'SELECT day, amount FROM sales WHERE day >= ?',
  [params.day],
  'sales_db',
  { sql_cache: 300 }                 // 指定数据源 + 缓存
)
```

选项支持 `{dsn, sql_cache}`; `sql_cache` 也可写成 `cache` 或 `ttl`。公开分享和定时任务运行时
会按发布/开启分享/保存任务时批准的 `approved_dsns` 再校验实际 `dsn`; 若用 `params.dsn`
这类动态数据源, 运行时只能选择已批准列表中的数据源。

### 自定义缓存 `cache.get/set`

`query()` 缓存只覆盖 SQL 结果。若要缓存**脚本自己计算的中间结果** (聚合、fetch 外部接口的响应等),
用 `cache`:

```
#!SCRIPT
let stats = cache.get('sales:stats:' + params.day)
if (stats === undefined) {
  const rows = query('SELECT channel, SUM(amount) amt FROM sales WHERE day = ? GROUP BY channel', [params.day])
  stats = { total: rows.reduce((s, r) => s + r.amt, 0), rows }
  cache.set('sales:stats:' + params.day, stats, 300)   // 缓存 300 秒
}
result.table({ title: '合计 ' + stats.total, rows: stats.rows })
#!END
```

- 值须可 JSON 序列化 (对象/数组/标量); 函数等不可序列化的值静默不缓存。
- **键是全局命名空间** (跨报表共享), 建议自带前缀 (如 `'sales:'`) 避免撞键。
- 进程内缓存, 重启即失; 查看者强制刷新 (`nocache`) 时 `get` 强制未命中, `set` 照常写入新值。
- 上限: 500 个键 / 单值序列化后 1MB, 超限静默不缓存。

### 11.1 前置脚本 `@setup` — 派生参数

普通脚本块在所有 SQL 查询**之后**执行, 所以它 `setParam` 改的值后续 SQL 看不到。
若要按过滤器值**派生新参数**给后续 SQL 用 (如"选中月份是否为当前月"), 在 `#!SCRIPT` 的标记行
加 `@setup`: 这块会在**宏冻结前**抢先执行, `setParam` 写入的值即成为后续所有区块可用的 `{name}` 宏。

```
${month|统计月份|today|date[Y-m]}

#!SCRIPT @setup
// 派生 is_current: 选中月份等于当前月时为 '1', 否则空串
setParam('is_current', params.month === formatDate(now(), 'Y-m') ? '1' : '')
#!END

-- 当前月查实时表, 历史月查归档表 (条件行二选一)
SELECT * FROM api_log              -- {?is_current}
SELECT * FROM api_log_{month[Y_m]} -- {?!is_current}
WHERE ...;
```

- `@setup` 块**只为副作用** (`setParam`), 它产出的 `result.*` / `log` 会被忽略, 不渲染区块。
- 可写多个 `@setup` 块, 按出现顺序执行; 都在宏冻结前完成。
- 派生参数名可任取 (`is_current`/`tier`/...), 不必是已声明的过滤器。
- `@setup` 脚本里 `query()` 可用 (查库派生值), 但它跑在并发预取**之前**, 串行执行, 别放重活。

### 复用前置 Block 数据

任意 SQL/script 产出的 block 都有唯一 `id`。后续脚本可以按这个 `id` 读取它的数据, 从而实现
"一次取数, 多种展示":

```
-- @id=sales_by_business_line
-- @title=销售业务线基础数据
-- @hidden=true
SELECT
  月份,
  业务线,
  订单数,
  销售额,
  利润
FROM ...

#!SCRIPT
const rows = dataset('sales_by_business_line')
const src = block('sales_by_business_line')

result.chart({
  id: 'current_previous_month_compare',
  title: '本月 vs 上月销售对比',
  type: 'bar',
  x: '业务线',
  y: ['销售额', '利润'],
  rows
})

result.table({
  id: 'revenue_by_business_line',
  title: '业务线收入明细',
  columns: src.columns,
  rows
})
#!END
```

- `dataset(id)` 只返回 `rows`, 是最常用的轻量写法。
- `block(id)` 返回 block 元数据, 适合复用 `title`、`columns`、`summary` 等。
- 返回值都是副本, 脚本内修改不会反写原 block。
- 被引用 block 建议显式写 `@id`; 若只是取数不展示, 加 `@hidden=true`。
- 当前按模板顺序执行, 只能引用脚本之前已经完成的 block。需要跨顺序引用时, 把取数 block 放到前面。

### 产出图表与文字

```
#!SCRIPT
const rows = query('SELECT day, SUM(amount) AS amt FROM sales GROUP BY day ORDER BY day')

result.markdown('## 每日销售趋势\n数据 T+1 更新')   // 文字说明 (支持 markdown)
result.chart({ type:'line', x:'day', series:['amt'], title:'趋势' }, rows)  // 折线图
result.table({ title:'明细', rows })                // 同一份数据再出表格
#!END
```

- `result.markdown(text)`: 产出一段 markdown 文字区块。
- `result.chart(cfg, rows?)`: 产出图表区块, `cfg` 支持 `id/title/subtitle/notice/columns/formats/sum/avg/invisible/hidden`;
  `rows` 可作为第二参数传入, 也可直接写在 `cfg.rows`。
  - 折线/柱状: `{type:'line'|'bar', x:'列', series:['列1','列2']}` 或 `{type:'bar', x:'列', y:['列1','列2']}`
  - 饼图: `{type:'pie', name:'分类列', value:'数值列'}`
  - `rows` 为图表数据 (通常就是 `query()` 的结果)。
- `result.table({...})`: 若传 `chart`, 同一个 block 会先显示图表, 再显示表格。`chart` 写法同上或同
  `@chart` 字符串, 如 `chart: 'bar:销售额,利润'`。
- `result.table({kpi:{items:[...]}})`: 产出 KPI 卡片 (同 §4.1 `@kpi`), 卡片显示在图表/表格之上。
  `kpi.items[].value/compare/trend` 为列名, 数据取自该 block 的 `rows`。

### 工具函数

```
#!SCRIPT
now()                      // "2026-06-25T14:30:00+08:00"  当前时间 (RFC3339)
formatDate('2026-06-25', 'Y年m月d日')   // "2026年06月25日"  (PHP 风格 token, 见 §6)
formatDate('2026-06-25 14:30:00', 'H:i') // "14:30"
#!END
```

- 复杂的日期运算直接用 JS 的 `Date` 也可以; `formatDate` 只是方便按 PHP token 输出。

### 外部数据 `fetch()`

```
#!SCRIPT
// GET
const r = fetch('https://api.internal/rate?base=CNY')
const rate = r.json().usd                       // 解析 JSON 响应

// POST + 自定义 header/body
const r2 = fetch('https://api.internal/query', {
  method: 'POST',
  headers: { 'Authorization': 'Bearer xxx', 'Content-Type': 'application/json' },
  body: JSON.stringify({ q: params.kw })
})
if (r2.status !== 200) { result.markdown('接口异常: ' + r2.status); }
else { result.table({ rows: r2.json().data }) }
#!END
```

- 返回对象: `{ status, body, json() }` —— `status` 是 HTTP 状态码, `body` 是原始文本, `json()` 解析为对象。
- `opts`: `{ method, headers, body }`, 省略则为 GET。单请求超时 30 秒, 响应体上限 4MB。
- ⚠️ `fetch` 默认可访问任意地址 (SSRF 面), **仅限内网信任环境** (见文末安全前提)。管理员可通过配置 `engine.script_fetch` 收紧: `off` 禁用, 或逗号分隔的 URL 前缀白名单 (如 `https://api.example.com,https://open.feishu.cn`)。

### 调试技巧

脚本写不对时, 用 `dump()` 把中间结果打到报表上看:

```
#!SCRIPT
const rows = query('SELECT * FROM sales LIMIT 3')
dump('查询结果', rows)        // 结构化展示, 确认字段名/类型
dump('参数', params)          // 看查看者传了什么
// ... 确认无误后再 result.table(...)
#!END
```

`dump` 会产出一个带 🔍 标题的区块, 美化 JSON 缩进展示, 调好后删掉即可。

### 脚本产出过滤器 `result.filter()`

当下拉选项需要复杂逻辑 (多表关联、外部 API、按其他条件计算) 时, 用脚本产出过滤器:

```
#!SCRIPT
const regions = query('SELECT id, name FROM regions WHERE enabled = 1')
result.filter({
  name: 'region', label: '地区', type: 'enum',
  options: regions.map(r => ({ value: String(r.id), label: r.name })),
  default: ''
})
// 后续区块可直接用 params.region 拼 SQL
const rows = query('SELECT * FROM sales WHERE region = ?', [params.region])
result.table({ title: '地区销售', rows })
#!END
```

脚本产出的过滤器会与声明式 `${...}` 过滤器一起渲染。简单的"查库出选项"优先用 `enum_sql`
(见 §7.3), `result.filter()` 留给确实需要脚本逻辑的场景。

### 表格区块复用声明式能力

`result.table()` 产出的表格**自动复用列级处理链**, 与声明式 SQL 区块一致:

```
#!SCRIPT
result.table({
  columns: [
    { name:'st',  header:'状态', config:{ tag:'1:success:已完成,0:danger:失败' } },
    { name:'rate',header:'完成率', config:{ percent:{ base:'total', succ:80 } } },
    { name:'amt', header:'金额', config:{ count:true } }
  ],
  rows,
  sum: true,                                  // 合计行 (同 @sum)
  row_tag: { when:'amt<0', class:'row-danger' } // 行级样式 (同 @row_tag)
})
#!END
```

- **区块元数据**: `id` / `title` / `subtitle` / `notice` / `invisible` / `hidden` / `merge_cell` 与普通 SQL block
  的同名注解一致。显式设置 `id` 可避免脚本产出的 block 被自动命名为 `block_1`。
- **列顺序简写**: `columns` 可写完整对象数组, 也可写字符串数组固定顺序:
  `columns: ['月份','业务线','订单数','销售额']`。
- **格式简写**: `formats` 可按列名配置常见展示格式:
  `formats: {'销售额':'money','订单数':'number','利润占比':'percent'}`。
  `money` 显示千分位和 2 位小数, `number` 显示千分位, `percent` 按 `0.25 -> 25.00%` 展示并默认
  不参与合计。更复杂的格式仍用 `columns[].config`。
- **列配置 `config`**: `tag` / `percent` / `enum` / `ratio` / `round` / `date` / `time2str` 等 (见 §6) 都生效。
- **`sum` / `avg`**: 产出合计/平均行 (同 §3 的 `@sum`/`@avg`, 配合列 `count:true`)。
- **`row_tag`**: 行级条件样式 (同 §6.1), 传规则对象或数组。

> **表级变换 (排序/过滤/转置/透视) 在脚本里用 JS 直接做**, 不走声明式注解:
> 排序 `rows.sort(...)`、过滤 `rows.filter(...)`、求和 `rows.reduce(...)` —— 脚本已是完整
> 编程环境, 比声明式 `@sort`/`@filter`/`@flip` 更灵活。

### 综合示例

一个完整脚本报表: 顶部声明过滤器 → 脚本按参数动态查询 → 出概览、图表、按渠道分组的多张表。

```sql
${day|日期|today|date}
${chs|渠道||enum_sql.multiple(SELECT channel AS value, channel AS label FROM sales GROUP BY channel)}

#!SCRIPT
// 1) 用 where() 拼可选条件 (渠道多选 -> IN, 空则跳过)
//    脚本里 params.chs 是用户提交的原始逗号串 (如 "web,app"), 直接 split 即可
const chArr = (params.chs || '').split(',').filter(Boolean)
const w = where({ 'day': params.day, channel: chArr })
const rows = query(`SELECT channel, amount, profit FROM sales WHERE ${w.sql}`, w.args)

// 2) 概览数字 (JS 直接算)
const total = rows.reduce((s, r) => s + Number(r.amount), 0)
result.markdown(`## ${params.day} 销售概览\n总额 **${total}**, 共 ${rows.length} 笔`)

// 3) 按渠道汇总 + 趋势图
const byCh = {}
for (const r of rows) byCh[r.channel] = (byCh[r.channel] || 0) + Number(r.amount)
const sumRows = Object.entries(byCh).map(([channel, amt]) => ({ channel, amt }))
result.chart({ type:'pie', name:'channel', value:'amt' }, sumRows)

// 4) 明细表: 复用列配置 (利润为负标红行) + 合计行
result.table({
  title: '明细',
  columns: [
    { name:'channel', header:'渠道' },
    { name:'amount',  header:'金额', config:{ count:true } },
    { name:'profit',  header:'利润', config:{ count:true } }
  ],
  rows,
  sum: true,
  row_tag: { when:'profit<0', class:'row-danger' }
})
#!END
```

### 常见错误

- **把 `params` 拼进 SQL 字符串** → 注入风险。值用 `?`+args, 见 `query`/`where`。
- **`result.table` 的 `rows` 用了 query 返回的字段名, 但 SQL 没起别名** → 列名是原始字段。
  确认用 `dump('rows', rows)` 看真实键名。
- **多选过滤器在脚本里**: `params.chs` 是用户提交的**原始逗号串** (如 `"web,app"`), 直接
  `params.chs.split(',')` 即可。(注意: SQL in-list 展开 `'a','b'` 只在声明式 SQL 区块的 `{chs}` 宏里发生,
  脚本拿到的是原值。)
- **死循环 / 慢查询**: 脚本 3 分钟超时会被中断并产出错误区块; 别在脚本里跑无界循环。
- **列顺序乱**: 省略 `columns` 时按对象键序推断 (不稳定); 要固定顺序就显式声明 `columns`。
- **`query` 的 `args` 必须是数组**: `query(sql, [params.day])` 而非 `query(sql, params.day)`;
  传标量会被静默忽略, 占位符 `?` 拿不到值。

### 注意

- **拼 SQL 用 `query(sql, [args])` 的参数化形式**, 不要把 `params` 值字符串拼进 SQL (防注入)。
- 脚本**总执行超时 3 分钟** (防死循环), 查询次数不限。
- 脚本出错/抛异常**只影响该区块** (产出一个错误区块), 不会让整个报表崩溃。
- `columns` 建议显式声明 (含 `config` 可复用 enum/tag/percent 等列配置); 省略时列顺序不保证。
- 表级变换 (排序/过滤/转置/透视) 在脚本里**用 JS 直接做** (`rows.sort/filter/reduce`), 不走声明式注解。

### ⚠️ 安全前提

脚本 = **在服务器上执行任意 JS**, 仅对报表有写权限 (`report*:w`) 者可写, **务必只在内网信任环境部署**。
其中 `fetch()` 可访问任意地址 (SSRF 面), 同样仅限信任环境使用。

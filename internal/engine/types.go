// Package engine 实现 dataddy 风格的报表模板引擎:
// 把报表 content 解析为多个区块 (block), 解析 SQL 注解、列配置、过滤器与宏,
// 执行 SQL 并产出可供前端渲染的结果。
package engine

// FilterDef 描述一个交互式过滤器, 提供给前端渲染输入控件。
type FilterDef struct {
	Name     string    `json:"name"`
	Label    string    `json:"label"`
	Type     string    `json:"type"`             // date / date_range / string / number / enum / bool
	Format   string    `json:"format,omitempty"` // 日期格式 (PHP 风格, 如 Y-m / Y-m-d), 来自 type[...] 后缀
	Default  string    `json:"default"`          // 原始默认值定义 (如 "-7 days,today")
	Resolved string    `json:"resolved"`         // 解析后的默认值 (如 "2026-06-17,2026-06-24"), 供前端回填
	Options  []EnumOpt `json:"options,omitempty"`
	Multiple bool      `json:"multiple,omitempty"` // enum 多选 (值为逗号分隔, SQL 里配合 IN)
	// 动态选项: enum_sql 时, 选项由这条 SQL 查询得到 (value/label 两列); 不下发前端。
	optionSQL string `json:"-"`
	optionDSN string `json:"-"`
}

// EnumOpt 是枚举过滤器的一个候选项。
type EnumOpt struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Column 是一个表格列, 保留顺序与展示配置。
type Column struct {
	Name   string         `json:"name"`   // 行数据中的 key
	Header string         `json:"header"` // 展示标题
	Config map[string]any `json:"config,omitempty"`
}

// Block 是报表中的一个区块: 表格 / 图表 / markdown。
type Block struct {
	ID        string           `json:"id"`
	Type      string           `json:"type"` // table / markdown / raw
	Title     string           `json:"title,omitempty"`
	Subtitle  string           `json:"subtitle,omitempty"`
	Notice    string           `json:"notice,omitempty"` // 表格上方提示信息
	Columns   []Column         `json:"columns,omitempty"`
	MergeCell []string         `json:"merge_cell,omitempty"` // 需纵向合并相同值的列名
	Rows      []map[string]any `json:"rows,omitempty"`
	Summary   map[string]any   `json:"summary,omitempty"` // 合计行 (sum)
	Average   map[string]any   `json:"average,omitempty"` // 平均行 (avg)
	// CellAttrs 单元格标签 (移植自 dataddy attrs[i][field]): colName -> 行号(字符串) -> 标签。
	CellAttrs map[string]map[string]*CellAttr `json:"cell_attrs,omitempty"`
	// RowAttrs 行级样式 (移植自 dataddy attrs[i]['_']): 行号(字符串) -> 行属性。
	RowAttrs  map[string]*RowAttr `json:"row_attrs,omitempty"`
	Invisible bool                `json:"invisible,omitempty"` // 隐藏表格主体, 仅保留图表
	Hidden    bool                `json:"hidden,omitempty"`    // 执行并可被脚本引用, 但不渲染
	Chart     any                 `json:"chart,omitempty"`
	Markdown  string              `json:"markdown,omitempty"`
	SQL       string              `json:"sql,omitempty"`
	// Messages 波动检测等产出的告警消息 (移植自 dataddy report['message']); 供订阅推送读取。
	Messages []string `json:"messages,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// CellAttr 是一个单元格标签, 交给前端 el-tag 渲染。
type CellAttr struct {
	Type  string `json:"type,omitempty"`  // success / warning / danger / info / primary (element-plus tag type)
	Text  string `json:"text,omitempty"`  // 标签文本; 为空时回退到单元格原值
	Plain bool   `json:"plain,omitempty"` // 朴素风格 (描边)
}

// RowAttr 是行级样式, 交给前端 el-table row-class-name 渲染。
type RowAttr struct {
	Class string `json:"class,omitempty"` // 追加到 <tr> 的 css class
}

// Result 是一次报表解析+执行的完整结果。
type Result struct {
	Filters     []FilterDef `json:"filters"`
	Blocks      []Block     `json:"blocks"`
	AutoRefresh int         `json:"auto_refresh,omitempty"` // 报表级自动刷新间隔 (秒); 来自 report.settings, 0 不刷新
	// Messages 汇总各区块的波动/告警消息 (按区块顺序); 供订阅推送直接读取。
	Messages []string `json:"messages,omitempty"`
	// PrependContent 页面顶部注入的原始 HTML (来自 report.settings); 前端 v-html 渲染。
	PrependContent string `json:"prepend_content,omitempty"`
}

// ChartConfig 是规整后的图表配置, 交给前端 ECharts 渲染。
type ChartConfig struct {
	Type   string   `json:"type"`             // line / bar / pie
	X      string   `json:"x,omitempty"`      // x 轴字段 (折线/柱状)
	Series []string `json:"series,omitempty"` // 数值字段
	Name   string   `json:"name,omitempty"`   // 饼图: 分类字段
	Value  string   `json:"value,omitempty"`  // 饼图: 数值字段
	Auto   bool     `json:"auto,omitempty"`   // __auto__
}

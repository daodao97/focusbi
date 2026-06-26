package engine

import (
	"fmt"
	"strings"

	"github.com/spf13/cast"
)

// Runner 执行报表模板。defaultDSN 为报表配置的默认数据源。
type Runner struct {
	defaultDSN string
	noCache    bool // 旁路查询缓存 (前端传 _nocache 时置位)
	// authz 在执行触达某数据源前校验调用者权限 (按实际 dsn 名)。
	// nil 表示不校验 (公开分享 / 订阅推送等已预授权的入口)。
	authz func(dsn string) error
}

func NewRunner(defaultDSN string) *Runner {
	if defaultDSN == "" {
		defaultDSN = "default"
	}
	return &Runner{defaultDSN: defaultDSN}
}

// WithNoCache 返回一个旁路查询缓存的 Runner 副本 (用于强制刷新)。
func (r *Runner) WithNoCache(b bool) *Runner {
	cp := *r
	cp.noCache = b
	return &cp
}

// WithAuthz 返回一个带数据源授权校验的 Runner 副本。
// fn(dsn) 返回非 nil 时, 该数据源的查询被拒 (体现为对应区块的 error)。
func (r *Runner) WithAuthz(fn func(dsn string) error) *Runner {
	cp := *r
	cp.authz = fn
	return &cp
}

// Parse 仅解析过滤器, 不执行 SQL —— 用于前端首次渲染输入控件。
func Parse(content string) []FilterDef {
	filters, _ := parseFilters(content)
	resolveDefaults(filters)
	return filters
}

// Run 解析并执行报表, params 为用户提交的过滤器取值。
func (r *Runner) Run(content string, params map[string]string) (*Result, error) {
	filters, cleaned := parseFilters(content)
	resolveDefaults(filters)
	r.resolveFilterOptions(filters) // enum_sql: 查库填充动态选项
	macros := macroValues(filters, params)

	result := &Result{Filters: filters, Blocks: []Block{}}

	// pending 是尚未定稿的合并组基底 (延迟一拍推送):
	// SQL 块默认先挂起, 若紧随的 SQL 块带 @join/@union 则并入它, 否则先推送再换基底。
	var pending *mergeGroup
	blockSeq := 0
	blockRefs := map[string]Block{}
	appendBlock := func(b Block) {
		blockSeq++
		if b.ID == "" {
			b.ID = fmt.Sprintf("block_%d", blockSeq)
		}
		blockRefs[b.ID] = cloneBlockForScript(b)
		result.Messages = append(result.Messages, b.Messages...)
		if b.Hidden && b.Error == "" {
			return
		}
		result.Blocks = append(result.Blocks, b)
	}
	flush := func() {
		if pending == nil {
			return
		}
		appendBlock(pending.finalize())
		pending = nil
	}

	for _, rawText := range splitBlocks(cleaned) {
		if strings.TrimSpace(rawText) == "" {
			continue
		}

		rb := parseBlock(rawText)

		// 脚本区块: 不走宏替换 (JS 的 {} 会与宏占位冲突), 参数通过注入的 params 读取。
		// 脚本可产出多个区块, 与单 block 模型不同, 单独处理。
		if rb.kind == "script" {
			flush()
			ctx := scriptContext{defaultDSN: r.defaultDSN, params: params, authz: r.authz, blocks: blockRefs, noCache: r.noCache}
			scriptBlocks, scriptFilters, _ := runScript(stripMarker(rb.body), ctx)
			// 脚本产出的过滤器并入结果 (供前端渲染下拉)
			result.Filters = append(result.Filters, scriptFilters...)
			for _, sb := range scriptBlocks {
				appendBlock(sb)
			}
			continue
		}

		// 行级条件宏: 整块跳过判断 (块首 `-- {?cond}` 经 applyMacros 处理)
		body := applyMacros(rb.body, macros)
		body = strings.TrimSpace(body)
		if body == "" || rb.kind == "empty" {
			continue
		}

		switch rb.kind {
		case "markdown", "raw":
			flush()
			block := Block{ID: annotationString(rb, "id"), Title: annotationString(rb, "title"), Type: rb.kind}
			block.Hidden = annotationBool(rb, "hidden")
			block.Markdown = applyMacros(stripMarker(rb.body), macros)
			appendBlock(block)
		case "sql":
			cols, rows, flipped, sql, errStr := r.runSQLData(rb, body)
			spec, isMerge := parseJoinConfig(rb.annotations)
			// 带 @join/@union 且已有挂起基底 -> 并入基底, 不新开块。
			if isMerge && pending != nil {
				pending.merge(spec, cols, rows, errStr)
				continue
			}
			// 否则定稿上一个基底, 自己成为新的挂起基底。
			flush()
			pending = newMergeGroup(rb, cols, rows, flipped, sql, errStr)
		default:
			continue
		}
	}
	flush()

	return result, nil
}

// runSQLData 执行 SQL 区块的"数据阶段": 查询 + 数据管线 (filter/date_line/sort/series/flip),
// 返回定型后的列、行、是否转置、最终 SQL 与错误串 (空表示无错)。
// 展示阶段 (列配置/chart/sum/波动/notice) 留待合并完成后由 mergeGroup.finalize 处理。
func (r *Runner) runSQLData(rb *rawBlock, sql string) (cols []string, rows []map[string]any, flipped bool, finalSQL string, errStr string) {
	if err := validateReadOnlySQL(sql); err != nil {
		return nil, nil, false, sql, err.Error()
	}

	// 自动补 LIMIT (默认 1000), 防止误查全表。可用 -- @limit=N 覆盖, 0 表示不限制。
	sql = injectLimit(sql, annotationInt(rb, "limit", 1000))
	finalSQL = sql

	dsn := r.defaultDSN
	if d := annotationString(rb, "dsn"); d != "" {
		dsn = d
	}

	// 数据源授权: 按实际触达的 dsn 校验 (@dsn= 覆盖也走这里)。无权则该块直接报错, 不查库。
	if r.authz != nil {
		if err := r.authz(dsn); err != nil {
			return nil, nil, false, sql, err.Error()
		}
	}

	// 查询缓存: -- @sql_cache=秒数 (0 或缺省不缓存); 前端强制刷新时旁路。
	qr, err := cachedQuery(dsn, sql, annotationInt(rb, "sql_cache", 0), r.noCache)
	if err != nil {
		return nil, nil, false, sql, err.Error()
	}

	cols, rows = qr.Columns, qr.Rows

	// 结果后置过滤: @filter=[["amount",">","100"]] (多条件 AND)
	if conds := parseFilterConfig(rb.annotations["filter"]); conds != nil {
		rows = applyResultFilter(conds, rows)
	}

	// 日期补全: @date_line={"field":"day","start":"-30 days"} 补齐缺失日期行。
	if cfg, ok := parseDateLineConfig(rb.annotations["date_line"]); ok {
		rows = applyDateLine(cfg, cols, rows)
	}

	// 服务端排序: @sort=+revenue,-count 多字段/分组排序 (跨页正确)。
	if items := parseSortConfig(rb.annotations["sort"]); items != nil {
		applySort(items, rows)
	}

	// 行转列 (透视): @series={"x":"day","series":"channel","value":"amount"}
	// 把长表展开成宽表, 便于多序列图表。
	if x, s, v, ok := parseSeriesConfig(rb.annotations["series"]); ok {
		cols, rows = pivotSeries(cols, rows, x, s, v)
	}

	// 行列转置: @flip={"key":"product"} (重构表结构, 须在透视之后)。
	if cfg, ok := parseFlipConfig(rb.annotations["flip"]); ok {
		if nc, nr, done := applyFlip(cfg, cols, rows); done {
			cols, rows, flipped = nc, nr, true
		}
	}

	return cols, rows, flipped, sql, ""
}

// mergeGroup 是一个 @join/@union 合并组的累积状态。
// 基底块 (rb) 提供展示注解; 后续带 @join/@union 的块只贡献数据。
type mergeGroup struct {
	rb      *rawBlock
	table   *arrayTable
	flipped bool
	sqls    []string
	errStr  string
}

func newMergeGroup(rb *rawBlock, cols []string, rows []map[string]any, flipped bool, sql, errStr string) *mergeGroup {
	return &mergeGroup{
		rb:      rb,
		table:   newArrayTable(cols, rows),
		flipped: flipped,
		sqls:    []string{sql},
		errStr:  errStr,
	}
}

// merge 把一个带 @join/@union 的块并入基底。
func (g *mergeGroup) merge(spec joinSpec, cols []string, rows []map[string]any, errStr string) {
	g.sqls = append(g.sqls, "")
	if errStr != "" {
		if g.errStr == "" {
			g.errStr = errStr
		}
		return
	}
	if g.errStr != "" {
		return // 基底已出错, 不再合并
	}
	if spec.isUnion {
		g.table.union(cols, rows)
	} else {
		g.table.join(cols, rows, spec.onKeys, spec.full)
	}
}

// finalize 完成展示阶段, 产出最终 Block。
func (g *mergeGroup) finalize() Block {
	rb := g.rb
	block := Block{
		ID:    annotationString(rb, "id"),
		Title: annotationString(rb, "title"),
		Type:  "table",
		SQL:   strings.TrimSpace(strings.Join(g.sqls, ";\n")),
	}
	if g.errStr != "" {
		block.Error = g.errStr
		return block
	}

	cols, rows := g.table.cols, g.table.rows

	// 波动检测: @data_fluctuations 算最近两期环比, 须在列级转换改值之前。
	if fields, threshold, ok := parseFluctuationsConfig(rb.annotations["data_fluctuations"]); ok {
		block.Messages = detectFluctuations(cols, rows, fields, threshold)
	}

	block.Columns = buildColumns(cols, rb.colConfigs)
	block.Rows = rows

	// 列级处理链: tag / row_tag / percent / 单元格转换 / 合计平均 (转置后跳过汇总)。
	applyColumnPipeline(&block, rb.annotations["row_tag"], !g.flipped && annotationBool(rb, "sum"), !g.flipped && annotationBool(rb, "avg"))

	if chart, ok := rb.annotations["chart"]; ok {
		block.Chart = normalizeChart(chart, cols)
	}

	if kpi, ok := rb.annotations["kpi"]; ok {
		block.Kpi = parseKpiConfig(kpi)
	}

	if title := annotationString(rb, "title"); title != "" {
		block.Title = title
	}
	block.Subtitle = annotationString(rb, "subtitle")
	block.Notice = annotationString(rb, "notice")
	block.Invisible = annotationBool(rb, "invisible")
	block.Hidden = annotationBool(rb, "hidden")
	block.MergeCell = parseMergeCell(annotationString(rb, "merge_cell"))
	return block
}

// parseMergeCell 解析 "report_month,business_line" 为列名切片。
func parseMergeCell(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// annotationInt 取注解的整数值, 缺失时返回 def。
func annotationInt(rb *rawBlock, key string, def int) int {
	v, ok := rb.annotations[key]
	if !ok {
		return def
	}
	return cast.ToInt(v)
}

// annotationBool 取注解的布尔值 (true / 1 / 非空且非 "false" 视为真)。
func annotationBool(rb *rawBlock, key string) bool {
	v, ok := rb.annotations[key]
	if !ok {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		s := strings.ToLower(strings.TrimSpace(val))
		return s != "" && s != "false" && s != "0"
	default:
		return cast.ToBool(v)
	}
}

// buildColumns 依据查询返回的列顺序与列配置构造 Column 列表。
func buildColumns(cols []string, configs map[string]map[string]any) []Column {
	out := make([]Column, 0, len(cols))
	for _, c := range cols {
		col := Column{Name: c, Header: c}
		if cfg, ok := configs[c]; ok {
			col.Config = cfg
			if h, ok := cfg["header"].(string); ok && h != "" {
				col.Header = h
			}
		}
		out = append(out, col)
	}
	return out
}

// annotationString 取注解的字符串值。
func annotationString(rb *rawBlock, key string) string {
	if v, ok := rb.annotations[key]; ok {
		return cast.ToString(v)
	}
	return ""
}

package engine

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"xproxy/conf"

	"github.com/spf13/cast"
)

// RunTraceEvent 是 Runner 执行过程中的可选观测事件。
type RunTraceEvent struct {
	Phase          string
	BlockIndex     int
	BlockKind      string
	BlockID        string
	BlockTitle     string
	DSN            string
	Duration       time.Duration
	ParsedBlocks   int
	OutputBlocks   int
	Rows           int
	Columns        int
	SQLLen         int
	ProducedBlocks int
	Error          string
}

// RunTraceFunc 接收 Runner 执行过程中的计时事件。调用方应保证它可并发调用。
type RunTraceFunc func(RunTraceEvent)

// Runner 执行报表模板。defaultDSN 为报表配置的默认数据源。
type Runner struct {
	defaultDSN string
	noCache    bool // 旁路查询缓存 (前端传 _nocache 时置位)
	// authz 在执行触达某数据源前校验调用者权限 (按实际 dsn 名)。
	// nil 表示不校验 (公开分享 / 订阅推送等已预授权的入口)。
	authz func(dsn string) error
	// concurrency 独立 SQL 区块的并发预取数。<=0 时 Run 取 conf 默认值。
	concurrency int
	trace       RunTraceFunc
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

// WithConcurrency 返回一个指定 SQL 区块并发预取数的 Runner 副本 (主要供测试覆写)。
// n<=0 时 Run 回退到 conf 配置 (默认 8)。
func (r *Runner) WithConcurrency(n int) *Runner {
	cp := *r
	cp.concurrency = n
	return &cp
}

// WithTrace 返回一个带执行计时回调的 Runner 副本。
func (r *Runner) WithTrace(fn RunTraceFunc) *Runner {
	cp := *r
	cp.trace = fn
	return &cp
}

// Parse 仅解析过滤器, 不执行 SQL —— 用于前端首次渲染输入控件。
func Parse(content string) []FilterDef {
	filters, _ := parseFilters(content)
	resolveDefaults(filters)
	return filters
}

// Run 解析并执行报表, params 为用户提交的过滤器取值。
func (r *Runner) Run(content string, params map[string]string) (ret *Result, err error) {
	runStarted := time.Now()
	parsedCount := 0
	parseDurations := map[int]time.Duration{}
	defer func() {
		if r.trace != nil {
			ev := RunTraceEvent{Phase: "report", Duration: time.Since(runStarted), ParsedBlocks: parsedCount}
			if ret != nil {
				ev.OutputBlocks = len(ret.Blocks)
			}
			if err != nil {
				ev.Error = err.Error()
			}
			r.trace(ev)
		}
		if ret != nil {
			ret.Timing = &ReportTiming{
				TotalMS:      durationMS(time.Since(runStarted)),
				ParsedBlocks: parsedCount,
				OutputBlocks: len(ret.Blocks),
			}
		}
	}()

	filters, cleaned := parseFilters(content)
	resolveDefaults(filters)
	r.resolveFilterOptions(filters) // enum_sql: 查库填充动态选项

	result := &Result{Filters: filters, Blocks: []Block{}}

	// 解析所有区块一次 (避免装配阶段重复 parse)。
	parsed := make([]*rawBlock, 0)
	for _, rawText := range splitBlocks(cleaned) {
		if strings.TrimSpace(rawText) == "" {
			continue
		}
		idx := len(parsed) + 1
		started := time.Now()
		rb := parseBlock(rawText)
		parseDuration := time.Since(started)
		parseDurations[idx-1] = parseDuration
		r.traceEvent(RunTraceEvent{
			Phase:      "block_parse",
			BlockIndex: idx,
			BlockKind:  rb.kind,
			BlockID:    annotationString(rb, "id"),
			BlockTitle: annotationString(rb, "title"),
			Duration:   parseDuration,
			SQLLen:     len(rb.body),
		})
		parsed = append(parsed, rb)
	}
	parsedCount = len(parsed)

	// 阶段 0: 前置脚本 (`#!SCRIPT @setup`)。在宏冻结前串行执行, 可 setParam 派生新值
	// (如按选中月份算 is_current), 供后续 SQL 的 {macro} / 条件行使用。
	// 这些块在主装配循环里跳过, 不再重复执行, 也不产出区块。
	for _, rb := range parsed {
		if rb.kind == "script" && annotationBool(rb, "setup") {
			if params == nil {
				params = map[string]string{} // setParam 需可写
			}
			ctx := scriptContext{defaultDSN: r.defaultDSN, params: params, authz: r.authz, noCache: r.noCache}
			runScript(stripMarker(rb.body), ctx) // 只为副作用 (setParam); 忽略产出
		}
	}

	// 宏在此冻结: 基于增广后的 params (含 @setup 写入的派生值)。此后并发预取与装配都用它。
	macros := macroValues(filters, params)

	// 阶段 1: 并发预取所有独立 SQL 区块的数据 (查询 + 数据管线)。
	// 纯 SQL 块的 runSQLData 是自包含的, 无跨块依赖, 故可并行; 脚本/markdown 不预取。
	prefetched := r.prefetchSQL(parsed, macros)

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

	// 阶段 2: 按模板顺序串行装配 (块序/消息序与串行执行完全一致)。
	for i, rb := range parsed {
		// 脚本区块: 不走宏替换 (JS 的 {} 会与宏占位冲突), 参数通过注入的 params 读取。
		// 脚本可产出多个区块, 与单 block 模型不同, 单独处理。
		if rb.kind == "script" {
			if annotationBool(rb, "setup") {
				continue // 前置脚本已在阶段 0 执行, 不产出区块
			}
			flush()
			started := time.Now()
			ctx := scriptContext{defaultDSN: r.defaultDSN, params: params, authz: r.authz, blocks: blockRefs, noCache: r.noCache}
			scriptBlocks, scriptFilters, scriptErr := runScript(stripMarker(rb.body), ctx)
			errStr := ""
			if scriptErr != nil {
				errStr = scriptErr.Error()
			}
			timing := blockTiming(parseDurations[i], time.Since(started))
			timing.DSN = r.defaultDSN
			timing.ProducedBlocks = len(scriptBlocks)
			timing.Error = errStr
			r.traceEvent(RunTraceEvent{
				Phase:          "block_exec",
				BlockIndex:     i + 1,
				BlockKind:      rb.kind,
				BlockID:        annotationString(rb, "id"),
				BlockTitle:     annotationString(rb, "title"),
				DSN:            r.defaultDSN,
				Duration:       time.Since(started),
				ProducedBlocks: len(scriptBlocks),
				Error:          errStr,
			})
			// 脚本产出的过滤器并入结果 (供前端渲染下拉)
			result.Filters = append(result.Filters, scriptFilters...)
			for _, sb := range scriptBlocks {
				sb.Timing = cloneTiming(timing)
				appendBlock(sb)
			}
			continue
		}

		switch rb.kind {
		case "markdown", "raw":
			started := time.Now()
			// 行级条件宏: 整块跳过判断 (块首 `-- {?cond}` 经 applyMacros 处理)
			if strings.TrimSpace(applyMacros(rb.body, macros)) == "" {
				continue
			}
			flush()
			block := Block{ID: annotationString(rb, "id"), Title: annotationString(rb, "title"), Type: rb.kind}
			block.Hidden = annotationBool(rb, "hidden")
			block.Markdown = applyMacros(stripMarker(rb.body), macros)
			block.Timing = blockTiming(parseDurations[i], time.Since(started))
			appendBlock(block)
			r.traceEvent(RunTraceEvent{
				Phase:      "block_exec",
				BlockIndex: i + 1,
				BlockKind:  rb.kind,
				BlockID:    annotationString(rb, "id"),
				BlockTitle: annotationString(rb, "title"),
				Duration:   time.Since(started),
			})
		case "sql":
			sd := prefetched[i]
			if sd == nil {
				continue // 空 body (行级条件宏删空) -> 跳过, 与串行一致
			}
			spec, isMerge := parseJoinConfig(rb.annotations)
			timing := cloneTiming(sd.timing)
			if timing == nil {
				timing = &BlockTiming{}
			}
			timing.ParseMS += durationMS(parseDurations[i])
			timing.TotalMS = timing.ParseMS + timing.ExecMS
			// 带 @join/@union 且已有挂起基底 -> 并入基底, 不新开块。
			if isMerge && pending != nil {
				pending.merge(spec, sd.cols, sd.rows, sd.errStr, timing)
				continue
			}
			// 否则定稿上一个基底, 自己成为新的挂起基底。
			flush()
			pending = newMergeGroup(rb, sd.cols, sd.rows, sd.flipped, sd.sql, sd.errStr, timing)
		default:
			continue
		}
	}
	flush()

	return result, nil
}

func (r *Runner) traceEvent(ev RunTraceEvent) {
	if r.trace != nil {
		r.trace(ev)
	}
}

func (r *Runner) blockDSN(rb *rawBlock) string {
	dsn := r.defaultDSN
	if d := annotationString(rb, "dsn"); d != "" {
		dsn = d
	}
	return dsn
}

func durationMS(d time.Duration) int64 {
	return d.Milliseconds()
}

func blockTiming(parseDuration, execDuration time.Duration) *BlockTiming {
	parseMS := durationMS(parseDuration)
	execMS := durationMS(execDuration)
	return &BlockTiming{ParseMS: parseMS, ExecMS: execMS, TotalMS: parseMS + execMS}
}

func cloneTiming(t *BlockTiming) *BlockTiming {
	if t == nil {
		return nil
	}
	cp := *t
	return &cp
}

func mergeTiming(base, add *BlockTiming) *BlockTiming {
	if base == nil {
		return cloneTiming(add)
	}
	if add == nil {
		return base
	}
	base.ParseMS += add.ParseMS
	base.ExecMS += add.ExecMS
	base.TotalMS = base.ParseMS + base.ExecMS
	base.Rows += add.Rows
	if add.Columns > base.Columns {
		base.Columns = add.Columns
	}
	base.SQLLen += add.SQLLen
	if base.DSN == "" {
		base.DSN = add.DSN
	} else if add.DSN != "" && base.DSN != add.DSN {
		base.DSN += "," + add.DSN
	}
	if base.Error == "" {
		base.Error = add.Error
	}
	return base
}

// sqlData 是一个 SQL 区块预取阶段的产出 (runSQLData 的返回值打包)。
type sqlData struct {
	cols    []string
	rows    []map[string]any
	flipped bool
	sql     string
	errStr  string
	timing  *BlockTiming
}

// prefetchSQL 并发执行所有独立 SQL 区块的数据阶段, 返回与 parsed 等长的结果切片
// (非 SQL 块、或宏替换后 body 为空的块对应 nil)。每个 goroutine 只写自己的下标, 无写竞争。
func (r *Runner) prefetchSQL(parsed []*rawBlock, macros map[string]string) []*sqlData {
	out := make([]*sqlData, len(parsed))

	// 收集待执行的 SQL 块下标及其宏替换后的 body。
	type job struct {
		idx  int
		rb   *rawBlock
		body string
	}
	var jobs []job
	for i, rb := range parsed {
		if rb.kind != "sql" {
			continue
		}
		body := strings.TrimSpace(applyMacros(rb.body, macros))
		if body == "" {
			continue // 空 body (行级条件宏删空) -> 留 nil
		}
		jobs = append(jobs, job{idx: i, rb: rb, body: body})
	}
	if len(jobs) == 0 {
		return out
	}

	n := r.concurrency
	if n <= 0 {
		n = conf.Get().QueryConcurrency()
	}
	if n > len(jobs) {
		n = len(jobs)
	}

	run := func(j job) {
		// 兜底: runSQLData 只返回 in-band errStr, 但并发下意外 panic 不应拖垮整个报表。
		started := time.Now()
		dsn := r.blockDSN(j.rb)
		defer func() {
			if rec := recover(); rec != nil {
				errStr := fmt.Sprintf("区块执行异常: %v", rec)
				timing := blockTiming(0, time.Since(started))
				timing.DSN = dsn
				timing.SQLLen = len(j.body)
				timing.Error = errStr
				out[j.idx] = &sqlData{sql: j.body, errStr: errStr, timing: timing}
				r.traceEvent(RunTraceEvent{
					Phase:      "block_exec",
					BlockIndex: j.idx + 1,
					BlockKind:  j.rb.kind,
					BlockID:    annotationString(j.rb, "id"),
					BlockTitle: annotationString(j.rb, "title"),
					DSN:        dsn,
					Duration:   time.Since(started),
					SQLLen:     len(j.body),
					Error:      errStr,
				})
			}
		}()
		cols, rows, flipped, sql, errStr := r.runSQLData(j.rb, j.body)
		timing := blockTiming(0, time.Since(started))
		timing.DSN = dsn
		timing.Rows = len(rows)
		timing.Columns = len(cols)
		timing.SQLLen = len(sql)
		timing.Error = errStr
		out[j.idx] = &sqlData{cols: cols, rows: rows, flipped: flipped, sql: sql, errStr: errStr, timing: timing}
		r.traceEvent(RunTraceEvent{
			Phase:      "block_exec",
			BlockIndex: j.idx + 1,
			BlockKind:  j.rb.kind,
			BlockID:    annotationString(j.rb, "id"),
			BlockTitle: annotationString(j.rb, "title"),
			DSN:        dsn,
			Duration:   time.Since(started),
			Rows:       len(rows),
			Columns:    len(cols),
			SQLLen:     len(sql),
			Error:      errStr,
		})
	}

	if n == 1 {
		// 串行回退: 不起 goroutine, 行为与原实现等价。
		for _, j := range jobs {
			run(j)
		}
		return out
	}

	sem := make(chan struct{}, n)
	var wg sync.WaitGroup
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			run(j)
		}(j)
	}
	wg.Wait()
	return out
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
	timing  *BlockTiming
}

func newMergeGroup(rb *rawBlock, cols []string, rows []map[string]any, flipped bool, sql, errStr string, timing *BlockTiming) *mergeGroup {
	return &mergeGroup{
		rb:      rb,
		table:   newArrayTable(cols, rows),
		flipped: flipped,
		sqls:    []string{sql},
		errStr:  errStr,
		timing:  cloneTiming(timing),
	}
}

// merge 把一个带 @join/@union 的块并入基底。
func (g *mergeGroup) merge(spec joinSpec, cols []string, rows []map[string]any, errStr string, timing *BlockTiming) {
	g.sqls = append(g.sqls, "")
	g.timing = mergeTiming(g.timing, timing)
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
	block.Timing = cloneTiming(g.timing)
	if g.errStr != "" {
		block.Error = g.errStr
		if block.Timing != nil && block.Timing.Error == "" {
			block.Timing.Error = g.errStr
		}
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

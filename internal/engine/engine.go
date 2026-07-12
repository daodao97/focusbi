package engine

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"xproxy/internal/datasource"
	"xproxy/internal/runtimecfg"

	"github.com/spf13/cast"
	"golang.org/x/sync/singleflight"
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
	ctx          context.Context
	queryGroup   *singleflight.Group
	queryMemo    *sync.Map
	memoEligible *sync.Map
	defaultDSN   string
	noCache      bool   // 旁路查询缓存 (前端传 _nocache 时置位)
	cacheScope   string // 脚本 KV 缓存作用域; 持久化报表使用 report:<id>
	// authz 在执行触达某数据源前校验调用者权限 (按实际 dsn 名)。
	// nil 表示不校验 (公开分享 / 定时任务等已预授权的入口)。
	authz func(dsn string) error
	trace RunTraceFunc
}

// WithCacheScope 返回一个带脚本缓存作用域的 Runner 副本。
// 空作用域会禁用脚本 KV 缓存, 避免临时预览跨模板共享数据。
func (r *Runner) WithCacheScope(scope string) *Runner {
	cp := *r
	cp.cacheScope = scope
	return &cp
}

func NewRunner(defaultDSN string) *Runner {
	if defaultDSN == "" {
		defaultDSN = "default"
	}
	return &Runner{defaultDSN: defaultDSN, ctx: context.Background()}
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

// WithTrace 返回一个带执行计时回调的 Runner 副本。
func (r *Runner) WithTrace(fn RunTraceFunc) *Runner {
	cp := *r
	cp.trace = fn
	return &cp
}

// ValidationIssue 是一次静态校验发现的问题。
type ValidationIssue struct {
	BlockIndex int    `json:"block_index"` // 1-based 区块序号
	BlockID    string `json:"block_id,omitempty"`
	Severity   string `json:"severity"` // error / warning
	Message    string `json:"message"`
}

// Validate 只做解析 + 只读 SQL 静态校验, 不连库执行。
// 返回 (过滤器, 问题列表)。用于 AI 快速迭代模板结构而不消耗数据库连接。
// 检查项: SQL 区块经默认过滤值宏替换后是否为合法只读单语句 (堆叠/写关键字会被拒)。
func Validate(content string) ([]FilterDef, []ValidationIssue) {
	filters, cleaned := parseFilters(content)
	resolveDefaults(filters)
	var issues []ValidationIssue
	for _, f := range filters {
		if f.constraintError != "" {
			issues = append(issues, ValidationIssue{Severity: "error", Message: "过滤器 " + f.Name + ": " + f.constraintError})
		}
	}
	macros := macroValues(filters, nil) // 用默认值展开宏

	knownIDs := map[string]bool{}
	declared := map[string]bool{}
	for _, f := range filters {
		declared[f.Name] = true
		declared["from_"+f.Name] = true
		declared["to_"+f.Name] = true
	}
	hasSQLBase := false
	idx := 0
	for _, raw := range splitBlocks(cleaned) {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		idx++
		rb := parseBlock(raw)
		id := annotationString(rb, "id")
		add := func(severity, message string) {
			issues = append(issues, ValidationIssue{BlockIndex: idx, BlockID: id, Severity: severity, Message: message})
		}
		if id != "" {
			if knownIDs[id] {
				add("error", "区块 id 重复: "+id)
			}
			knownIDs[id] = true
		}
		if annotationInt(rb, "limit", -1) == 0 {
			add("warning", "@limit=0 会取消默认限制, 但仍受系统硬上限约束")
		}
		if strings.Contains(rb.body, "[raw]") {
			add("warning", "使用了未转义的 [raw] 宏, 请确认输入来源可信")
		}
		if rb.kind == "script" {
			if strings.Contains(rb.body, "fetch(") {
				add("warning", "脚本使用了 fetch(), 发布前请确认外部地址白名单")
			}
			for _, m := range datasetRefRe.FindAllStringSubmatch(rb.body, -1) {
				if !knownIDs[m[1]] {
					add("warning", "脚本引用了尚未声明 id 的区块: "+m[1])
				}
			}
		}
		if rb.kind != "sql" {
			hasSQLBase = false
			continue
		}
		if _, isMerge := parseJoinConfig(rb.annotations); isMerge && !hasSQLBase {
			add("error", "@join/@union 前缺少可合并的 SQL 区块")
		}
		hasSQLBase = true
		for _, m := range macroRe.FindAllStringSubmatch(rb.body, -1) {
			name := strings.Split(m[2], ",")[0]
			if name != "" && !declared[name] {
				add("warning", "使用了未声明的宏: "+name)
			}
		}
		sql := strings.TrimSpace(applyMacros(rb.body, macros))
		if sql == "" {
			continue // 行级条件宏可能把整块置空, 非错误
		}
		if err := validateReadOnlySQL(sql); err != nil {
			add("error", err.Error())
		}
	}
	return filters, issues
}

// Run 解析并执行报表, params 为用户提交的过滤器取值。
func (r *Runner) Run(content string, params map[string]string) (*Result, error) {
	return r.RunContext(context.Background(), content, params)
}

// RunContext 在报表总超时内执行。调用方取消请求时, 数据库查询也会随之取消。
func (r *Runner) RunContext(parent context.Context, content string, params map[string]string) (*Result, error) {
	ctx, cancel := context.WithTimeout(parent, runtimecfg.ReportTimeout())
	defer cancel()
	cp := *r
	cp.ctx = ctx
	cp.queryGroup = &singleflight.Group{}
	cp.queryMemo = &sync.Map{}
	cp.memoEligible = &sync.Map{}
	return cp.run(content, params)
}

func (r *Runner) run(content string, params map[string]string) (ret *Result, err error) {
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
	if err := validateFilterDefinitions(filters); err != nil {
		return nil, err
	}
	r.resolveFilterOptions(filters, params) // enum_sql: 查库填充动态/级联选项
	result := &Result{Filters: filters, Blocks: []Block{}}
	if err := validateFilterParams(filters, params); err != nil {
		// 首次打开报表时仍需把过滤器定义返回前端，否则必填项无法填写。
		// 参数不合法时不执行任何主 SQL，只用错误区块提示用户。
		result.Blocks = append(result.Blocks, Block{Type: "raw", Error: err.Error()})
		return result, nil
	}

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
			ctx := scriptContext{ctx: r.ctx, defaultDSN: r.defaultDSN, params: params, authz: r.authz, noCache: r.noCache, cacheScope: r.cacheScope, query: r.scriptQuery}
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
			ctx := scriptContext{ctx: r.ctx, defaultDSN: r.defaultDSN, params: params, authz: r.authz, blocks: blockRefs, noCache: r.noCache, cacheScope: r.cacheScope, query: r.scriptQuery}
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
				pending.merge(spec, sd.cols, sd.rows, sd.truncated, sd.rowLimit, sd.errStr, timing)
				continue
			}
			// 否则定稿上一个基底, 自己成为新的挂起基底。
			flush()
			pending = newMergeGroup(rb, sd.cols, sd.rows, sd.flipped, sd.truncated, sd.rowLimit, sd.sql, sd.errStr, timing)
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
	cols      []string
	rows      []map[string]any
	flipped   bool
	sql       string
	errStr    string
	timing    *BlockTiming
	truncated bool
	rowLimit  int
}

// 请求内顺序复用只保留小结果; 大结果仍由 singleflight 合并并发查询, 避免 memo
// 与区块数据同时长期持有两份大对象。
const maxRunMemoRows = 5000

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
	// 在执行前统计声明式 SQL 的最终查询键。确定重复的键即使结果很大也进入 memo,
	// 从而在 query_concurrency=1 时仍只访问数据库一次；唯一大查询不额外占内存。
	counts := map[string]int{}
	for _, j := range jobs {
		finalSQL, _, _ := prepareBlockSQL(j.rb, j.body)
		key := runQueryKey(r.blockDSN(j.rb), finalSQL, annotationInt(j.rb, "sql_cache", 0))
		counts[key]++
	}
	if r.memoEligible != nil {
		for key, count := range counts {
			if count > 1 {
				r.memoEligible.Store(key, true)
			}
		}
	}

	n := runtimecfg.QueryConcurrency()
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
		cols, rows, flipped, truncated, rowLimit, sql, errStr := r.runSQLData(j.rb, j.body)
		timing := blockTiming(0, time.Since(started))
		timing.DSN = dsn
		timing.Rows = len(rows)
		timing.Columns = len(cols)
		timing.SQLLen = len(sql)
		timing.Error = errStr
		out[j.idx] = &sqlData{cols: cols, rows: rows, flipped: flipped, truncated: truncated, rowLimit: rowLimit, sql: sql, errStr: errStr, timing: timing}
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
func (r *Runner) runSQLData(rb *rawBlock, sql string) (cols []string, rows []map[string]any, flipped, truncated bool, rowLimit int, finalSQL string, errStr string) {
	if err := validateReadOnlySQL(sql); err != nil {
		return nil, nil, false, false, 0, sql, err.Error()
	}

	sql, templateLimit, detectTemplateTruncation := prepareBlockSQL(rb, sql)
	finalSQL = sql

	dsn := r.defaultDSN
	if d := annotationString(rb, "dsn"); d != "" {
		dsn = d
	}

	// 数据源授权: 按实际触达的 dsn 校验 (@dsn= 覆盖也走这里)。无权则该块直接报错, 不查库。
	if r.authz != nil {
		if err := r.authz(dsn); err != nil {
			return nil, nil, false, false, 0, sql, err.Error()
		}
	}

	// 查询缓存: -- @sql_cache=秒数 (0 或缺省不缓存); 前端强制刷新时旁路。
	qr, err := r.query(dsn, sql, annotationInt(rb, "sql_cache", 0))
	if err != nil {
		return nil, nil, false, false, 0, sql, err.Error()
	}

	cols, rows = qr.Columns, qr.Rows
	if detectTemplateTruncation && len(rows) > templateLimit {
		rows = rows[:templateLimit]
		truncated = true
		rowLimit = templateLimit
	} else if len(rows) > reportQueryHardLimit {
		rows = rows[:reportQueryHardLimit]
		truncated = true
		rowLimit = reportQueryHardLimit
	}

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

	return cols, rows, flipped, truncated, rowLimit, sql, ""
}

func prepareBlockSQL(rb *rawBlock, sql string) (finalSQL string, templateLimit int, detectTemplateTruncation bool) {
	// 引擎自动限制查询 N+1 行, 用额外一行判断默认/注解 LIMIT 是否截断。
	// SQL 自带 LIMIT 时尊重作者语义; 系统硬上限始终由外层查询兜底。
	templateLimit = annotationInt(rb, "limit", 1000)
	detectTemplateTruncation = templateLimit > 0 && !limitRe.MatchString(strings.TrimSpace(sql))
	if detectTemplateTruncation {
		sql = injectLimit(sql, templateLimit+1)
	}
	return enforceHardLimit(sql, reportQueryHardLimit+1), templateLimit, detectTemplateTruncation
}

// query 合并单次报表运行内完全相同的 DSN+SQL+参数查询。每个调用方拿到独立副本,
// 后续排序、透视等变换不会互相污染。
func (r *Runner) query(dsn, sql string, ttlSec int, args ...any) (*datasource.QueryResult, error) {
	return r.queryWithMemo(dsn, sql, ttlSec, false, args...)
}

func (r *Runner) scriptQuery(dsn, sql string, ttlSec int, args ...any) (*datasource.QueryResult, error) {
	return r.queryWithMemo(dsn, sql, ttlSec, true, args...)
}

func (r *Runner) queryWithMemo(dsn, sql string, ttlSec int, forceMemo bool, args ...any) (*datasource.QueryResult, error) {
	if r.queryGroup == nil {
		return cachedQueryContext(r.ctx, dsn, sql, ttlSec, r.noCache, args...)
	}
	key := runQueryKey(dsn, sql, ttlSec, args...)
	if r.queryMemo != nil {
		if memo, ok := r.queryMemo.Load(key); ok {
			return cloneQueryResult(memo.(*datasource.QueryResult)), nil
		}
	}
	v, err, _ := r.queryGroup.Do(key, func() (any, error) {
		res, err := cachedQueryContext(r.ctx, dsn, sql, ttlSec, r.noCache, args...)
		if err == nil && r.queryMemo != nil {
			knownDuplicate := false
			if r.memoEligible != nil {
				_, knownDuplicate = r.memoEligible.Load(key)
			}
			if forceMemo || knownDuplicate || len(res.Rows) <= maxRunMemoRows {
				r.queryMemo.Store(key, cloneQueryResult(res))
			}
		}
		return res, err
	})
	if err != nil {
		return nil, err
	}
	return cloneQueryResult(v.(*datasource.QueryResult)), nil
}

func runQueryKey(dsn, sql string, ttlSec int, args ...any) string {
	return fmt.Sprintf("%s:ttl=%d", cacheKey(normalizedDSNName(dsn), sql, args...), ttlSec)
}

// mergeGroup 是一个 @join/@union 合并组的累积状态。
// 基底块 (rb) 提供展示注解; 后续带 @join/@union 的块只贡献数据。
type mergeGroup struct {
	rb        *rawBlock
	table     *arrayTable
	flipped   bool
	sqls      []string
	errStr    string
	timing    *BlockTiming
	truncated bool
	rowLimit  int
}

func newMergeGroup(rb *rawBlock, cols []string, rows []map[string]any, flipped, truncated bool, rowLimit int, sql, errStr string, timing *BlockTiming) *mergeGroup {
	return &mergeGroup{
		rb:        rb,
		table:     newArrayTable(cols, rows),
		flipped:   flipped,
		sqls:      []string{sql},
		errStr:    errStr,
		timing:    cloneTiming(timing),
		truncated: truncated,
		rowLimit:  rowLimit,
	}
}

// merge 把一个带 @join/@union 的块并入基底。
func (g *mergeGroup) merge(spec joinSpec, cols []string, rows []map[string]any, truncated bool, rowLimit int, errStr string, timing *BlockTiming) {
	g.sqls = append(g.sqls, "")
	g.timing = mergeTiming(g.timing, timing)
	g.truncated = g.truncated || truncated
	if truncated && (g.rowLimit == 0 || rowLimit < g.rowLimit) {
		g.rowLimit = rowLimit
	}
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
	block.Truncated = g.truncated
	if g.truncated {
		block.RowLimit = g.rowLimit
	}
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
		cc := normalizeChart(chart, cols)
		block.Chart = cc
		// X 轴重复值提示: 同一类目出现多次会让图表与多维表格行对不上 (反馈 2)。
		if note := chartXDupNotice(cc, block.Rows); note != "" && block.Notice == "" {
			block.Notice = note
		}
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

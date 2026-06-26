package engine

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/spf13/cast"
)

// 动态脚本报表 (移植自 dataddy 的脚本插件思路):
// 报表里的 #!SCRIPT...#!END 区块是一段 JavaScript, 用 goja (纯 Go JS VM) 执行。
// 脚本通过注入的全局 API 查询数据、读取过滤器参数、产出区块:
//
//	const rows = query('SELECT ... WHERE day=?', [params.day])
//	result.table({ title:'渠道', columns:[{name:'ch',header:'渠道'}], rows })
//
// 安全前提: 脚本 = 在服务器执行任意 JS, 仅限 report.manage:rw 可写、内网信任环境部署。
// goja 本身无文件/网络/exec 能力; 我们注入的 query (参数化) 与 fetch (任意外呼) 是仅有的外部面。

// scriptTimeout 是单个脚本区块的总执行超时 (防死循环)。var 便于测试覆写。
var scriptTimeout = 3 * time.Minute

// fetchTimeout 是脚本内单个 fetch 请求的超时 (独立于脚本总超时)。
const fetchTimeout = 30 * time.Second

// scriptContext 是脚本执行所需的上下文。
type scriptContext struct {
	defaultDSN string
	params     map[string]string
	authz      func(dsn string) error // 数据源授权 (nil = 不校验)
	blocks     map[string]Block       // 已执行完成的 block, 按 id 引用
	noCache    bool                   // 旁路查询缓存
}

// runScript 执行一段脚本, 返回它产出的区块与过滤器。脚本出错/超时不崩报表, 以 Error 区块返回。
func runScript(code string, ctx scriptContext) (blocks []Block, filters []FilterDef, err error) {
	vm := goja.New()
	// 字段名按 JSON tag (小写) 暴露给 JS, 与声明式区块结构一致。
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))

	acc := &scriptResult{}

	// 超时中断: 到点强制中断 VM (中断死循环/超长执行)。
	timer := time.AfterFunc(scriptTimeout, func() {
		vm.Interrupt("脚本执行超时")
	})
	defer timer.Stop()

	// panic 兜底: goja 中断或脚本异常以 panic 抛出, 这里转成 Error 区块。
	defer func() {
		if r := recover(); r != nil {
			blocks = append(acc.blocks, Block{Type: "raw", Error: fmt.Sprintf("脚本执行异常: %v", r)})
			filters = acc.filters
			err = nil
		}
	}()

	injectScriptAPI(vm, ctx, acc)

	if _, runErr := vm.RunString(code); runErr != nil {
		// 脚本语法/运行错误 (含中断): 已产出的区块保留, 末尾追加错误区块。
		acc.blocks = append(acc.blocks, Block{Type: "raw", Error: "脚本错误: " + runErr.Error()})
	}
	return acc.blocks, acc.filters, nil
}

// scriptResult 累积脚本通过 result.* 声明的区块与过滤器。
type scriptResult struct {
	blocks  []Block
	filters []FilterDef
}

// injectScriptAPI 把全局 API 注入 goja Runtime。
func injectScriptAPI(vm *goja.Runtime, ctx scriptContext, acc *scriptResult) {
	// params: 过滤器值 (只读 map)
	_ = vm.Set("params", ctx.params)

	// dataset(id) / block(id): 读取前面已执行完成的 block 数据。
	// dataset 只返回 rows, block 返回带 columns/summary 等元数据的对象。
	_ = vm.Set("dataset", func(call goja.FunctionCall) goja.Value {
		id := strings.TrimSpace(call.Argument(0).String())
		blk, ok := ctx.blocks[id]
		if !ok {
			panic(vm.ToValue("dataset 未找到: " + id))
		}
		if blk.Error != "" {
			panic(vm.ToValue("dataset 执行失败: " + id + ": " + blk.Error))
		}
		return vm.ToValue(cloneRows(blk.Rows))
	})
	_ = vm.Set("block", func(call goja.FunctionCall) goja.Value {
		id := strings.TrimSpace(call.Argument(0).String())
		blk, ok := ctx.blocks[id]
		if !ok {
			panic(vm.ToValue("block 未找到: " + id))
		}
		return vm.ToValue(blockForJS(blk))
	})

	// query(sql, args?, dsnOrOptions?, options?) -> []row
	_ = vm.Set("query", func(call goja.FunctionCall) goja.Value {
		sql := call.Argument(0).String()
		var args []any
		if a := call.Argument(1); !goja.IsUndefined(a) && !goja.IsNull(a) {
			if arr, ok := a.Export().([]any); ok {
				args = arr
			}
		}
		dsn := ctx.defaultDSN
		ttlSec := 0
		dsn, ttlSec = scriptQueryOptions(call.Argument(2), dsn, ttlSec)
		dsn, ttlSec = scriptQueryOptions(call.Argument(3), dsn, ttlSec)
		// 数据源授权: 脚本可指定任意 dsn, 必须校验。
		if ctx.authz != nil {
			if err := ctx.authz(dsn); err != nil {
				panic(vm.ToValue(err.Error()))
			}
		}
		qr, err := cachedQuery(dsn, sql, ttlSec, ctx.noCache, args...)
		if err != nil {
			panic(vm.ToValue("query 失败: " + err.Error()))
		}
		return vm.ToValue(qr.Rows)
	})

	// result.table / result.markdown / result.chart
	result := vm.NewObject()
	_ = result.Set("table", func(call goja.FunctionCall) goja.Value {
		acc.blocks = append(acc.blocks, tableFromJS(vm, call.Argument(0)))
		return goja.Undefined()
	})
	_ = result.Set("markdown", func(call goja.FunctionCall) goja.Value {
		acc.blocks = append(acc.blocks, Block{Type: "markdown", Markdown: call.Argument(0).String()})
		return goja.Undefined()
	})
	_ = result.Set("chart", func(call goja.FunctionCall) goja.Value {
		acc.blocks = append(acc.blocks, chartFromJS(call.Argument(0), call.Argument(1)))
		return goja.Undefined()
	})
	// result.filter({name,label,type,options,default}) -> 动态产出一个过滤器
	_ = result.Set("filter", func(call goja.FunctionCall) goja.Value {
		if f, ok := filterFromJS(call.Argument(0)); ok {
			acc.filters = append(acc.filters, f)
		}
		return goja.Undefined()
	})
	_ = vm.Set("result", result)

	// now() -> RFC3339; formatDate(value, fmt) -> PHP 风格
	_ = vm.Set("now", func() string { return nowFunc().Format(time.RFC3339) })
	_ = vm.Set("formatDate", func(call goja.FunctionCall) goja.Value {
		v := call.Argument(0).String()
		f := "Y-m-d"
		if a := call.Argument(1); !goja.IsUndefined(a) {
			f = a.String()
		}
		return vm.ToValue(formatDateCell(v, f))
	})

	// fetch(url, opts?) -> {status, body, json()}  (任意外呼, 仅内网信任环境)
	_ = vm.Set("fetch", func(call goja.FunctionCall) goja.Value {
		return scriptFetch(vm, call)
	})

	// where({...}) -> {sql, args}: 把条件对象拼成参数化 WHERE 片段。
	_ = vm.Set("where", func(call goja.FunctionCall) goja.Value {
		return buildWhere(vm, call.Argument(0))
	})

	// log(...) -> 追加一个 raw 调试区块 (仅作者可见); 标量直接拼字符串
	_ = vm.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			parts[i] = a.String()
		}
		acc.blocks = append(acc.blocks, Block{Type: "raw", Markdown: strings.Join(parts, " ")})
		return goja.Undefined()
	})

	// dump(...) -> 把任意值 (对象/数组/query 结果) 结构化美化输出, 用于调试。
	// 多参数时, 第一个为字符串则当作标签 (如 dump('rows', rows))。
	_ = vm.Set("dump", func(call goja.FunctionCall) goja.Value {
		args := call.Arguments
		label := ""
		if len(args) >= 2 {
			if s, ok := args[0].Export().(string); ok {
				label, args = s, args[1:]
			}
		}
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = jsPretty(vm, a)
		}
		text := strings.Join(parts, "\n")
		if label != "" {
			text = label + ":\n" + text
		}
		acc.blocks = append(acc.blocks, Block{Type: "raw", Title: "🔍 dump", Markdown: text})
		return goja.Undefined()
	})
}

// jsPretty 用 VM 的 JSON.stringify 把值美化为 2 空格缩进的字符串; 失败回退到 String()。
func jsPretty(vm *goja.Runtime, v goja.Value) string {
	if v == nil || goja.IsUndefined(v) {
		return "undefined"
	}
	if goja.IsNull(v) {
		return "null"
	}
	stringify, ok := goja.AssertFunction(vm.GlobalObject().Get("JSON").ToObject(vm).Get("stringify"))
	if ok {
		if res, err := stringify(goja.Undefined(), v, goja.Undefined(), vm.ToValue(2)); err == nil {
			if s := res.String(); s != "" && s != "undefined" {
				return s
			}
		}
	}
	return v.String() // 回退: 函数等无法 JSON 序列化的值
}

// tableFromJS 把脚本的 result.table({...}) 参数转成 Block。
func tableFromJS(vm *goja.Runtime, arg goja.Value) Block {
	spec, _ := arg.Export().(map[string]any)
	blk := Block{Type: "table"}
	if spec == nil {
		return blk
	}
	applyScriptBlockSpec(&blk, spec, spec["rows"])
	if chart, ok := spec["chart"]; ok {
		blk.Chart = normalizeChart(chart, columnNames(blk.Columns))
	}

	// 复用列级处理链: columns[].config 的 tag/enum/percent/date 等生效, 支持 row_tag / sum / avg。
	applyColumnPipeline(&blk, spec["row_tag"], cast.ToBool(spec["sum"]), cast.ToBool(spec["avg"]))
	return blk
}

// 合法的比较操作符 (键里 "field op" 的 op 部分白名单)。
var whereOps = map[string]bool{
	"=": true, "!=": true, "<>": true, ">": true, ">=": true, "<": true, "<=": true,
	"like": true, "LIKE": true, "in": true, "IN": true, "not in": true, "NOT IN": true,
}

// buildWhere 把条件对象拼成参数化 WHERE 片段, 返回 JS 对象 {sql, args}。
//
//	where({ region: params.region, 'day >=': params.day, status: [1,2] })
//	-> { sql: "region = ? AND day >= ? AND status IN (?,?)", args: [...] }
//
// 规则: 空值 (undefined/null/""/空数组) 的条件**自动跳过** (实现可选过滤器);
// 数组值用 IN (?,...); 键可写 "field" (默认 =) 或 "field op" (显式操作符, 走白名单)。
// 无任何有效条件时 sql 为 "1=1" (可安全嵌入 WHERE)。
func buildWhere(vm *goja.Runtime, arg goja.Value) goja.Value {
	out := vm.NewObject()
	obj := arg.ToObject(vm)
	if obj == nil {
		_ = out.Set("sql", "1=1")
		_ = out.Set("args", []any{})
		return out
	}

	var conds []string
	var args []any
	for _, key := range obj.Keys() { // Keys() 保持插入顺序, SQL 稳定
		field, op := splitFieldOp(key)
		if field == "" {
			continue
		}
		v := obj.Get(key).Export()
		// 数组 -> IN
		if arr, ok := v.([]any); ok {
			vals := nonEmpty(arr)
			if len(vals) == 0 {
				continue // 空数组跳过
			}
			ph := make([]string, len(vals))
			for i := range vals {
				ph[i] = "?"
			}
			inOp := "IN"
			if op == "not in" || op == "NOT IN" {
				inOp = "NOT IN"
			}
			conds = append(conds, fmt.Sprintf("%s %s (%s)", field, inOp, strings.Join(ph, ",")))
			args = append(args, vals...)
			continue
		}
		// 标量 -> field op ?
		if isEmptyVal(v) {
			continue // 空值跳过 (可选过滤器)
		}
		conds = append(conds, field+" "+op+" ?")
		args = append(args, v)
	}

	sql := "1=1"
	if len(conds) > 0 {
		sql = strings.Join(conds, " AND ")
	}
	_ = out.Set("sql", sql)
	_ = out.Set("args", args)
	return out
}

// splitFieldOp 把 "field" / "field >=" 拆成字段与操作符 (默认 =)。
// 非法操作符回退为整体当字段名 (再默认 =), 避免静默吞掉条件。
func splitFieldOp(key string) (field, op string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ""
	}
	if i := strings.LastIndex(key, " "); i > 0 {
		cand := strings.TrimSpace(key[i+1:])
		if whereOps[cand] || whereOps[strings.ToLower(cand)] {
			return strings.TrimSpace(key[:i]), cand
		}
		// "not in" 是两段, 特判
		lower := strings.ToLower(key)
		if strings.HasSuffix(lower, " not in") {
			return strings.TrimSpace(key[:len(key)-len(" not in")]), "NOT IN"
		}
	}
	return key, "="
}

// isEmptyVal 判断标量是否为"空" (应跳过的条件值)。
func isEmptyVal(v any) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s) == ""
	}
	return false
}

// nonEmpty 过滤数组里的空元素。
func nonEmpty(arr []any) []any {
	out := make([]any, 0, len(arr))
	for _, v := range arr {
		if !isEmptyVal(v) {
			out = append(out, v)
		}
	}
	return out
}

// filterFromJS 把 result.filter({...}) 参数转成 FilterDef。name 为空则忽略。
func filterFromJS(arg goja.Value) (FilterDef, bool) {
	spec, _ := arg.Export().(map[string]any)
	if spec == nil {
		return FilterDef{}, false
	}
	f := FilterDef{
		Name:     cast.ToString(spec["name"]),
		Label:    cast.ToString(spec["label"]),
		Type:     cast.ToString(spec["type"]),
		Default:  cast.ToString(spec["default"]),
		Multiple: cast.ToBool(spec["multiple"]),
	}
	if f.Name == "" {
		return FilterDef{}, false
	}
	if f.Type == "" {
		f.Type = "string"
	}
	if f.Label == "" {
		f.Label = f.Name
	}
	f.Resolved = f.Default
	if opts, ok := spec["options"].([]any); ok {
		for _, o := range opts {
			m, ok := o.(map[string]any)
			if !ok {
				continue
			}
			val := cast.ToString(m["value"])
			lab := cast.ToString(m["label"])
			if lab == "" {
				lab = val
			}
			f.Options = append(f.Options, EnumOpt{Value: val, Label: lab})
		}
	}
	return f, true
}

// chartFromJS 把 result.chart(cfg, rows) 转成 Block (复用 normalizeChart)。
// 兼容两种写法:
//
//	result.chart({type:'bar', x:'业务线', y:['销售额'], rows, id, title})
//	result.chart({type:'bar', x:'业务线', y:['销售额'], id, title}, rows)
func chartFromJS(cfgArg, rowsArg goja.Value) Block {
	blk := Block{Type: "table"}
	spec, _ := cfgArg.Export().(map[string]any)
	var rows any
	if !goja.IsUndefined(rowsArg) && !goja.IsNull(rowsArg) {
		rows = rowsArg.Export()
	} else if spec != nil {
		rows = spec["rows"]
	}
	if spec != nil {
		applyScriptBlockSpec(&blk, spec, rows)
		applyColumnPipeline(&blk, spec["row_tag"], cast.ToBool(spec["sum"]), cast.ToBool(spec["avg"]))
	} else {
		blk.Rows = rowsFromJS(rows)
		if len(blk.Rows) > 0 {
			blk.Columns = inferColumns(blk.Rows[0])
		}
	}
	blk.Chart = normalizeChart(cfgArg.Export(), columnNames(blk.Columns))
	return blk
}

// inferColumns 从一行数据推断列 (键序不稳定, 仅兜底; 建议脚本显式给 columns)。
func inferColumns(row map[string]any) []Column {
	cols := make([]Column, 0, len(row))
	for k := range row {
		cols = append(cols, Column{Name: k, Header: k})
	}
	return cols
}

func applyScriptBlockSpec(blk *Block, spec map[string]any, rows any) {
	blk.ID = cast.ToString(spec["id"])
	blk.Title = cast.ToString(spec["title"])
	blk.Subtitle = cast.ToString(spec["subtitle"])
	blk.Notice = cast.ToString(spec["notice"])
	blk.Invisible = cast.ToBool(spec["invisible"])
	blk.Hidden = cast.ToBool(spec["hidden"])
	blk.MergeCell = parseMergeCell(cast.ToString(spec["merge_cell"]))
	blk.Rows = rowsFromJS(rows)
	blk.Columns = columnsFromJS(spec["columns"], blk.Rows)
	applyFormatSpec(blk.Columns, spec["formats"])
}

func rowsFromJS(raw any) []map[string]any {
	if rows, ok := raw.([]map[string]any); ok {
		return cloneRows(rows)
	}
	rows, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func columnsFromJS(raw any, rows []map[string]any) []Column {
	if cols, ok := raw.([]Column); ok {
		return cloneColumns(cols)
	}
	if cols, ok := raw.([]map[string]any); ok {
		out := make([]Column, 0, len(cols))
		for _, c := range cols {
			if col, ok := columnFromMap(c); ok {
				out = append(out, col)
			}
		}
		return out
	}
	if cols, ok := raw.([]any); ok {
		out := make([]Column, 0, len(cols))
		for _, c := range cols {
			switch v := c.(type) {
			case string:
				if v != "" {
					out = append(out, Column{Name: v, Header: v})
				}
			case map[string]any:
				if col, ok := columnFromMap(v); ok {
					out = append(out, col)
				}
			}
		}
		return out
	}
	if len(rows) > 0 {
		return inferColumns(rows[0])
	}
	return nil
}

func columnFromMap(v map[string]any) (Column, bool) {
	name := cast.ToString(v["name"])
	if name == "" {
		name = cast.ToString(v["field"])
	}
	if name == "" {
		return Column{}, false
	}
	col := Column{Name: name, Header: name}
	if h := cast.ToString(v["header"]); h != "" {
		col.Header = h
	} else if h := cast.ToString(v["label"]); h != "" {
		col.Header = h
	}
	if cfg, ok := v["config"].(map[string]any); ok {
		col.Config = cfg
	}
	return col, true
}

func scriptQueryOptions(v goja.Value, dsn string, ttlSec int) (string, int) {
	if goja.IsUndefined(v) || goja.IsNull(v) {
		return dsn, ttlSec
	}
	exported := v.Export()
	switch opt := exported.(type) {
	case string:
		if s := strings.TrimSpace(opt); s != "" {
			dsn = s
		}
	case int, int64, int32, float64, float32:
		ttlSec = cast.ToInt(opt)
	case map[string]any:
		if s := strings.TrimSpace(cast.ToString(opt["dsn"])); s != "" {
			dsn = s
		}
		for _, key := range []string{"sql_cache", "cache", "ttl"} {
			if raw, ok := opt[key]; ok {
				ttlSec = cast.ToInt(raw)
				break
			}
		}
	}
	return dsn, ttlSec
}

func applyFormatSpec(cols []Column, raw any) {
	formats, ok := raw.(map[string]any)
	if !ok {
		return
	}
	for i := range cols {
		format := strings.ToLower(strings.TrimSpace(cast.ToString(formats[cols[i].Name])))
		if format == "" {
			format = strings.ToLower(strings.TrimSpace(cast.ToString(formats[cols[i].Header])))
		}
		if format == "" {
			continue
		}
		if cols[i].Config == nil {
			cols[i].Config = map[string]any{}
		}
		if _, exists := cols[i].Config["format"]; !exists {
			cols[i].Config["format"] = format
		}
		if (format == "percent" || format == "percentage") && cols[i].Config["nosum"] == nil {
			cols[i].Config["nosum"] = true
		}
	}
}

func columnNames(cols []Column) []string {
	names := make([]string, 0, len(cols))
	for _, c := range cols {
		names = append(names, c.Name)
	}
	return names
}

func cloneBlockForScript(b Block) Block {
	b.Rows = cloneRows(b.Rows)
	b.Columns = cloneColumns(b.Columns)
	b.Summary = cloneMap(b.Summary)
	b.Average = cloneMap(b.Average)
	return b
}

func blockForJS(b Block) map[string]any {
	return map[string]any{
		"id":         b.ID,
		"type":       b.Type,
		"title":      b.Title,
		"subtitle":   b.Subtitle,
		"notice":     b.Notice,
		"columns":    columnsForJS(b.Columns),
		"rows":       cloneRows(b.Rows),
		"summary":    cloneMap(b.Summary),
		"average":    cloneMap(b.Average),
		"chart":      b.Chart,
		"sql":        b.SQL,
		"error":      b.Error,
		"invisible":  b.Invisible,
		"hidden":     b.Hidden,
		"merge_cell": append([]string(nil), b.MergeCell...),
	}
}

func cloneRows(rows []map[string]any) []map[string]any {
	if rows == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		out = append(out, cloneMap(row))
	}
	return out
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneColumns(cols []Column) []Column {
	if cols == nil {
		return nil
	}
	out := make([]Column, len(cols))
	for i, col := range cols {
		out[i] = col
		out[i].Config = cloneMap(col.Config)
	}
	return out
}

func columnsForJS(cols []Column) []map[string]any {
	if cols == nil {
		return nil
	}
	out := make([]map[string]any, 0, len(cols))
	for _, col := range cols {
		out = append(out, map[string]any{
			"name":   col.Name,
			"header": col.Header,
			"config": cloneMap(col.Config),
		})
	}
	return out
}

// scriptFetch 实现 fetch(url, opts?) -> {status, body, json()}。
func scriptFetch(vm *goja.Runtime, call goja.FunctionCall) goja.Value {
	url := call.Argument(0).String()
	method := "GET"
	var bodyReader io.Reader
	headers := map[string]string{}
	if o := call.Argument(1); !goja.IsUndefined(o) && !goja.IsNull(o) {
		if opts, ok := o.Export().(map[string]any); ok {
			if m := cast.ToString(opts["method"]); m != "" {
				method = strings.ToUpper(m)
			}
			if b := cast.ToString(opts["body"]); b != "" {
				bodyReader = strings.NewReader(b)
			}
			if h, ok := opts["headers"].(map[string]any); ok {
				for k, v := range h {
					headers[k] = cast.ToString(v)
				}
			}
		}
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		panic(vm.ToValue("fetch 请求构造失败: " + err.Error()))
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: fetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		panic(vm.ToValue("fetch 失败: " + err.Error()))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20)) // 上限 4MB
	bodyStr := string(raw)

	out := vm.NewObject()
	_ = out.Set("status", resp.StatusCode)
	_ = out.Set("body", bodyStr)
	_ = out.Set("json", func() goja.Value {
		// 复用 VM 的 JSON.parse, 返回 JS 原生对象
		parse, _ := goja.AssertFunction(vm.GlobalObject().Get("JSON").ToObject(vm).Get("parse"))
		res, perr := parse(goja.Undefined(), vm.ToValue(bodyStr))
		if perr != nil {
			panic(vm.ToValue("fetch.json 解析失败: " + perr.Error()))
		}
		return res
	})
	return out
}

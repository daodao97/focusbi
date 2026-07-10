package engine

import (
	"net"
	"strings"
	"testing"
	"time"
)

func TestScriptFetchIPBlocked(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "::1", "fc00::1"} {
		if !scriptFetchIPBlocked(net.ParseIP(raw)) {
			t.Errorf("%s 应被判定为不可访问地址", raw)
		}
	}
	if scriptFetchIPBlocked(net.ParseIP("8.8.8.8")) {
		t.Fatal("公网地址不应被拒绝")
	}
}

// ---- splitBlocks: 脚本含分号不被切碎 ----

func TestSplitBlocksKeepsScriptIntact(t *testing.T) {
	content := "SELECT 1;\n#!SCRIPT\nconst a = 1;\nconst b = 2;\nresult.table({rows:[]});\n#!END\nSELECT 2;"
	blocks := splitBlocks(content)
	// 期望: SELECT 1;  |  #!SCRIPT...#!END (整段)  |  SELECT 2;
	if len(blocks) != 3 {
		t.Fatalf("blocks = %d, want 3: %#v", len(blocks), blocks)
	}
	if !strings.Contains(blocks[1], "const a = 1;") || !strings.Contains(blocks[1], "#!END") {
		t.Errorf("脚本块被切碎: %q", blocks[1])
	}
	if !strings.HasPrefix(strings.TrimSpace(blocks[1]), "#!SCRIPT") {
		t.Errorf("脚本块未整体保留: %q", blocks[1])
	}
}

func TestParseBlockScriptKind(t *testing.T) {
	b := parseBlock("#!SCRIPT\nconst x = {a:1};\n#!END")
	if b.kind != "script" {
		t.Fatalf("kind = %q, want script", b.kind)
	}
	// 脚本体不应被注解/列配置扫描破坏
	if !strings.Contains(b.body, "{a:1}") {
		t.Errorf("脚本体被破坏: %q", b.body)
	}
}

// @setup 前置脚本在宏冻结前 setParam, 派生值 (非过滤器名) 也能作为 {macro} 用。
func TestSetupScriptDerivesParam(t *testing.T) {
	// is_current 由 month 派生, 在 markdown 里用 {is_current} 宏回显 (无需数据库)。
	content := "${month|月份|2026-06|string}\n" +
		"#!SCRIPT @setup\n" +
		"setParam('is_current', params.month === '2026-06' ? 'YES' : 'NO')\n" +
		"#!END\n\n" +
		"#!MARKDOWN\n当前月: {is_current}\n#!END\n"

	res, err := NewRunner("default").Run(content, map[string]string{"month": "2026-06"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 1 || !strings.Contains(res.Blocks[0].Markdown, "当前月: YES") {
		t.Fatalf("派生 is_current=YES 应进宏, got %+v", res.Blocks)
	}

	res, err = NewRunner("default").Run(content, map[string]string{"month": "2026-05"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 1 || !strings.Contains(res.Blocks[0].Markdown, "当前月: NO") {
		t.Fatalf("派生 is_current=NO 应进宏, got %+v", res.Blocks)
	}
}

func TestStripMarkerScript(t *testing.T) {
	got := stripMarker("#!SCRIPT\nresult.table({rows:[]});\n#!END")
	if strings.Contains(got, "#!SCRIPT") || strings.Contains(got, "#!END") {
		t.Errorf("标记未去净: %q", got)
	}
	if !strings.Contains(got, "result.table") {
		t.Errorf("脚本体丢失: %q", got)
	}
}

// ---- runScript: result.table + columns 推断 + params ----

func TestRunScriptTable(t *testing.T) {
	code := `
		result.table({
			title: '渠道',
			columns: [{name:'ch', header:'渠道'}, {name:'amt', header:'金额'}],
			rows: [{ch:'web', amt:100}, {ch:'app', amt:200}]
		})
	`
	blocks, _, err := runScript(code, scriptContext{})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Type != "table" {
		t.Fatalf("blocks = %#v", blocks)
	}
	b := blocks[0]
	if b.Title != "渠道" || len(b.Columns) != 2 || len(b.Rows) != 2 {
		t.Errorf("table 内容错误: %+v", b)
	}
	if b.Columns[0].Name != "ch" || b.Columns[0].Header != "渠道" {
		t.Errorf("列错误: %+v", b.Columns)
	}
}

func TestRunScriptParams(t *testing.T) {
	code := `result.markdown('day=' + params.day)`
	blocks, _, err := runScript(code, scriptContext{params: map[string]string{"day": "2026-06-25"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || blocks[0].Markdown != "day=2026-06-25" {
		t.Fatalf("params 注入失败: %#v", blocks)
	}
}

func TestRunScriptMultiBlock(t *testing.T) {
	code := `
		result.markdown('# 标题')
		result.table({rows:[{x:1}]})
	`
	blocks, _, _ := runScript(code, scriptContext{})
	if len(blocks) != 2 {
		t.Fatalf("应产出 2 个区块, got %d", len(blocks))
	}
	if blocks[0].Type != "markdown" || blocks[1].Type != "table" {
		t.Errorf("区块顺序/类型错误: %#v", blocks)
	}
	// columns 从首行推断
	if len(blocks[1].Columns) != 1 || blocks[1].Columns[0].Name != "x" {
		t.Errorf("列推断失败: %+v", blocks[1].Columns)
	}
}

// ---- 错误隔离 + 超时 ----

func TestRunScriptErrorIsolation(t *testing.T) {
	// 脚本抛错: 不 panic 崩溃, 返回带 Error 的区块
	blocks, _, err := runScript(`throw new Error('boom')`, scriptContext{})
	if err != nil {
		t.Fatalf("不应返回 err (错误应进区块): %v", err)
	}
	if len(blocks) == 0 || blocks[len(blocks)-1].Error == "" {
		t.Fatalf("错误未隔离为区块: %#v", blocks)
	}
	if !strings.Contains(blocks[len(blocks)-1].Error, "boom") {
		t.Errorf("错误信息丢失: %q", blocks[len(blocks)-1].Error)
	}
}

func TestRunScriptPartialThenError(t *testing.T) {
	// 先产出一个区块, 再抛错: 已产出的保留, 末尾追加错误
	code := `result.markdown('ok'); throw new Error('later')`
	blocks, _, _ := runScript(code, scriptContext{})
	if len(blocks) != 2 {
		t.Fatalf("应有 1 正常 + 1 错误, got %d: %#v", len(blocks), blocks)
	}
	if blocks[0].Markdown != "ok" || blocks[1].Error == "" {
		t.Errorf("部分产出未保留: %#v", blocks)
	}
}

func TestRunScriptTimeout(t *testing.T) {
	old := scriptTimeout
	scriptTimeout = 100 * time.Millisecond
	defer func() { scriptTimeout = old }()

	done := make(chan struct{})
	var blocks []Block
	go func() {
		blocks, _, _ = runScript(`while(true){}`, scriptContext{})
		close(done)
	}()
	select {
	case <-done:
		// 应被中断, 返回带错误的区块, 不挂死
		if len(blocks) == 0 || blocks[len(blocks)-1].Error == "" {
			t.Errorf("超时未产生错误区块: %#v", blocks)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("脚本未被超时中断, 挂死了")
	}
}

// ---- dump 调试 ----

func TestRunScriptDump(t *testing.T) {
	// dump 对象应输出结构化 JSON
	blocks, _, _ := runScript(`dump({a:1, b:[2,3]})`, scriptContext{})
	if len(blocks) != 1 {
		t.Fatalf("应产出 1 个调试区块, got %d", len(blocks))
	}
	md := blocks[0].Markdown
	if !strings.Contains(md, `"a": 1`) || !strings.Contains(md, "2,") && !strings.Contains(md, "2\n") {
		t.Errorf("dump 未美化输出对象: %q", md)
	}
	if blocks[0].Title == "" {
		t.Errorf("dump 区块应带标题")
	}
}

func TestRunScriptDumpWithLabel(t *testing.T) {
	// 首参为字符串 -> 当作标签
	blocks, _, _ := runScript(`dump('rows', [{x:1}])`, scriptContext{})
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	md := blocks[0].Markdown
	if !strings.HasPrefix(md, "rows:") {
		t.Errorf("标签未生效: %q", md)
	}
	if !strings.Contains(md, `"x": 1`) {
		t.Errorf("值未美化: %q", md)
	}
}

func TestRunScriptDumpNullUndefined(t *testing.T) {
	blocks, _, _ := runScript(`dump(null); dump(undefined)`, scriptContext{})
	if len(blocks) != 2 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	if !strings.Contains(blocks[0].Markdown, "null") {
		t.Errorf("null 输出错误: %q", blocks[0].Markdown)
	}
	if !strings.Contains(blocks[1].Markdown, "undefined") {
		t.Errorf("undefined 输出错误: %q", blocks[1].Markdown)
	}
}

// ---- result.filter() 脚本产出过滤器 ----

func TestRunScriptResultFilter(t *testing.T) {
	code := `
		result.filter({
			name: 'region', label: '地区', type: 'enum',
			options: [{value:'1',label:'华东'},{value:'2',label:'华南'}]
		})
		result.table({rows:[{x:1}]})
	`
	blocks, filters, _ := runScript(code, scriptContext{})
	if len(filters) != 1 {
		t.Fatalf("应产出 1 个过滤器, got %d", len(filters))
	}
	f := filters[0]
	if f.Name != "region" || f.Type != "enum" || len(f.Options) != 2 {
		t.Errorf("过滤器内容错误: %+v", f)
	}
	if f.Options[0].Value != "1" || f.Options[0].Label != "华东" {
		t.Errorf("选项错误: %+v", f.Options)
	}
	if len(blocks) != 1 { // filter 不计入 blocks
		t.Errorf("blocks 应为 1, got %d", len(blocks))
	}
}

func TestRunScriptFilterNoName(t *testing.T) {
	// name 缺失的过滤器被忽略
	_, filters, _ := runScript(`result.filter({label:'x', options:[]})`, scriptContext{})
	if len(filters) != 0 {
		t.Errorf("无 name 应忽略, got %d", len(filters))
	}
}

// ---- where() 助手 ----

func TestRunScriptWhere(t *testing.T) {
	// 空值跳过 + 数组 IN + 显式操作符; 用 markdown 把结果带出来断言
	code := `
		const w = where({ region: params.region, 'day >=': params.day, status: [1,2], kw: '' })
		result.markdown(w.sql + ' || ' + JSON.stringify(w.args))
	`
	blocks, _, _ := runScript(code, scriptContext{params: map[string]string{"region": "华东", "day": "2026-06-01"}})
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	md := blocks[0].Markdown
	// region 与 day 有值保留, kw 空跳过, status 数组 -> IN
	if !strings.Contains(md, "region = ?") || !strings.Contains(md, "day >= ?") {
		t.Errorf("条件拼接错误: %q", md)
	}
	if !strings.Contains(md, "status IN (?,?)") {
		t.Errorf("数组未转 IN: %q", md)
	}
	if strings.Contains(md, "kw") {
		t.Errorf("空值未跳过: %q", md)
	}
	if !strings.Contains(md, `["华东","2026-06-01",1,2]`) {
		t.Errorf("args 错误: %q", md)
	}
}

func TestRunScriptWhereEmpty(t *testing.T) {
	// 全空 -> 1=1, args 空
	code := `const w = where({a:'', b:null, c:[]}); result.markdown(w.sql + '|' + w.args.length)`
	blocks, _, _ := runScript(code, scriptContext{})
	if blocks[0].Markdown != "1=1|0" {
		t.Errorf("空条件应为 1=1: %q", blocks[0].Markdown)
	}
}

// ---- 脚本 table 复用列级处理 (tag/percent/enum/sum) ----

func TestRunScriptTableColumnPipeline(t *testing.T) {
	code := `
		result.table({
			columns: [
				{name:'st', header:'状态', config:{tag:'1:success:已完成,0:danger:失败'}},
				{name:'amt', header:'金额', config:{count:true}}
			],
			rows: [{st:'1', amt:100}, {st:'0', amt:200}],
			sum: true
		})
	`
	blocks, _, _ := runScript(code, scriptContext{})
	b := blocks[0]
	// tag: st 列应生成 cell_attrs
	col := b.CellAttrs["st"]
	if col == nil || col["0"] == nil || col["0"].Type != "success" || col["0"].Text != "已完成" {
		t.Fatalf("tag 未生效: %+v", b.CellAttrs)
	}
	// sum: 应有合计行
	if b.Summary == nil || b.Summary["amt"] != "300" {
		t.Errorf("sum 未生效: %+v", b.Summary)
	}
}

func TestRunScriptTableEnumTransform(t *testing.T) {
	// 列 enum 转换在脚本 table 里也应生效
	code := `result.table({columns:[{name:'s',config:{enum:'1:成功,0:失败'}}], rows:[{s:'1'},{s:'0'}]})`
	blocks, _, _ := runScript(code, scriptContext{})
	rows := blocks[0].Rows
	if rows[0]["s"] != "成功" || rows[1]["s"] != "失败" {
		t.Errorf("enum 转换未生效: %+v", rows)
	}
}

func TestRunScriptChartConfigMetadataColumnsAndFormats(t *testing.T) {
	code := `
		const rows = [
			{业务线:'GPT 代付', 本月销售额:1234.5, 上月销售额:1000, 利润占比:0.25},
			{业务线:'gemini 代付', 本月销售额:2345.5, 上月销售额:2000, 利润占比:0.3}
		]
		result.chart({
			id: 'current_previous_month_compare',
			title: '本月 vs 上月销售对比',
			subtitle: '本月为筛选结束月份',
			type: 'bar',
			x: '业务线',
			y: ['本月销售额', '上月销售额'],
			columns: ['业务线', '本月销售额', '上月销售额', '利润占比'],
			formats: {'本月销售额':'money', '上月销售额':'money', '利润占比':'percent'},
			rows,
			sum: true
		})
	`
	blocks, _, _ := runScript(code, scriptContext{})
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	b := blocks[0]
	if b.ID != "current_previous_month_compare" || b.Title != "本月 vs 上月销售对比" || b.Subtitle == "" {
		t.Fatalf("metadata 未生效: %+v", b)
	}
	chart, ok := b.Chart.(*ChartConfig)
	if !ok {
		t.Fatalf("chart 类型错误: %#v", b.Chart)
	}
	if chart.Type != "bar" || chart.X != "业务线" || len(chart.Series) != 2 || chart.Series[0] != "本月销售额" {
		t.Fatalf("chart 配置错误: %+v", chart)
	}
	if len(b.Columns) != 4 || b.Columns[1].Name != "本月销售额" || b.Columns[3].Name != "利润占比" {
		t.Fatalf("列顺序错误: %+v", b.Columns)
	}
	// 新契约: format 是展示格式, rows 保持原始数值 (前端渲染时格式化), 仅写入列 config。
	if b.Rows[0]["本月销售额"] != 1234.5 || b.Rows[0]["利润占比"] != 0.25 {
		t.Fatalf("rows 应保持原始数值: %+v", b.Rows[0])
	}
	if cfgFormat(b.Columns, "本月销售额") != "money" || cfgFormat(b.Columns, "利润占比") != "percent" {
		t.Fatalf("format 应写入列 config: %+v", b.Columns)
	}
	if b.Summary["本月销售额"] != "3580" {
		t.Fatalf("summary 应为原始数值: %+v", b.Summary)
	}
	if _, ok := b.Summary["利润占比"]; ok {
		t.Fatalf("percent 列默认不应汇总: %+v", b.Summary)
	}
}

func TestRunScriptTableCanCarryChartAndStringColumns(t *testing.T) {
	code := `
		result.table({
			id: 'revenue_by_business_line',
			title: '业务线收入明细',
			chart: {type:'bar', x:'业务线', y:['销售额']},
			columns: ['月份', '业务线', '订单数', '销售额'],
			formats: {'订单数':'number', '销售额':'money'},
			rows: [{月份:'2026-06', 业务线:'GPT 代付', 订单数:1200, 销售额:45678.9}]
		})
	`
	blocks, _, _ := runScript(code, scriptContext{})
	b := blocks[0]
	if b.ID != "revenue_by_business_line" || b.Title != "业务线收入明细" {
		t.Fatalf("metadata 未生效: %+v", b)
	}
	if len(b.Columns) != 4 || b.Columns[0].Header != "月份" || b.Columns[3].Header != "销售额" {
		t.Fatalf("columns 字符串写法错误: %+v", b.Columns)
	}
	// 新契约: rows 保持原始数值, format 仅写入列 config。
	if v, _ := toFloat(b.Rows[0]["订单数"]); v != 1200 {
		t.Fatalf("rows 应保持原始数值: %+v", b.Rows[0])
	}
	if v, _ := toFloat(b.Rows[0]["销售额"]); v != 45678.9 {
		t.Fatalf("rows 应保持原始数值: %+v", b.Rows[0])
	}
	if cfgFormat(b.Columns, "订单数") != "number" || cfgFormat(b.Columns, "销售额") != "money" {
		t.Fatalf("format 应写入列 config: %+v", b.Columns)
	}
	chart, ok := b.Chart.(*ChartConfig)
	if !ok || chart.Type != "bar" || chart.X != "业务线" || len(chart.Series) != 1 || chart.Series[0] != "销售额" {
		t.Fatalf("table chart 配置错误: %#v", b.Chart)
	}
}

func TestRunScriptDatasetAndBlockRefs(t *testing.T) {
	ctx := scriptContext{blocks: map[string]Block{
		"sales": {
			ID:      "sales",
			Type:    "table",
			Title:   "销售数据",
			Columns: []Column{{Name: "业务线", Header: "业务线"}, {Name: "销售额", Header: "销售额"}},
			Rows:    []map[string]any{{"业务线": "GPT 代付", "销售额": 100}},
		},
	}}
	code := `
		const rows = dataset('sales')
		rows[0].销售额 = 200
		const b = block('sales')
		result.table({
			id: 'view',
			title: b.title,
			columns: b.columns,
			rows: b.rows
		})
	`
	blocks, _, _ := runScript(code, ctx)
	if len(blocks) != 1 {
		t.Fatalf("got %d blocks", len(blocks))
	}
	amt, _ := toFloat(blocks[0].Rows[0]["销售额"])
	if blocks[0].Title != "销售数据" || amt != 100 {
		t.Fatalf("block 引用或副本隔离错误: %+v", blocks[0])
	}
}

func TestRunScriptDatasetMissingReturnsErrorBlock(t *testing.T) {
	blocks, _, _ := runScript(`dataset('missing')`, scriptContext{blocks: map[string]Block{}})
	if len(blocks) != 1 || !strings.Contains(blocks[0].Error, "dataset 未找到") {
		t.Fatalf("missing dataset 应产生错误区块: %+v", blocks)
	}
}

func TestRunHiddenBlockCanBeReferencedByScript(t *testing.T) {
	setupSQLiteDefault(t)
	content := `
-- @id=sales_by_day
-- @title=每日销售数据
-- @hidden=true
SELECT day, pv FROM pv ORDER BY day;

#!SCRIPT
const rows = dataset('sales_by_day')
const src = block('sales_by_day')
result.table({
  id: 'sales_view',
  title: src.title,
  columns: [
    {name:'day', header:'日期'},
    {name:'pv', header:'销售额'},
    {name:'double_pv', header:'销售额 x2'}
  ],
  rows: rows.map(r => ({day:r.day, pv:r.pv, double_pv:Number(r.pv)*2}))
})
#!END
`
	res, err := NewRunner("default").Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 1 {
		t.Fatalf("hidden 取数块不应渲染, got %d blocks: %+v", len(res.Blocks), res.Blocks)
	}
	b := res.Blocks[0]
	if b.ID != "sales_view" || b.Title != "每日销售数据" {
		t.Fatalf("script view 错误: %+v", b)
	}
	doublePV, _ := toFloat(b.Rows[0]["double_pv"])
	if len(b.Rows) != 2 || doublePV != 200 {
		t.Fatalf("dataset 引用数据错误: %+v", b.Rows)
	}
}

// cfgFormat 取某列的 format 配置, 供测试断言 format 写入了列 config 而非改了 rows。
func cfgFormat(cols []Column, name string) string {
	for _, c := range cols {
		if c.Name == name && c.Config != nil {
			if f, ok := c.Config["format"].(string); ok {
				return f
			}
		}
	}
	return ""
}

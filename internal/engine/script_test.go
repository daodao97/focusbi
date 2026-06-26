package engine

import (
	"strings"
	"testing"
	"time"
)

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

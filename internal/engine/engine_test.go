package engine

import (
	"regexp"
	"strings"
	"testing"
)

func TestParseFilters(t *testing.T) {
	content := "${date|日期|-7 days,today|date_range}\n" +
		"${status|状态|1|enum(1:成功,0:失败)}\n" +
		"SELECT 1;"
	filters, cleaned := parseFilters(content)
	if len(filters) != 2 {
		t.Fatalf("want 2 filters, got %d", len(filters))
	}
	if filters[0].Name != "date" || filters[0].Type != "date_range" {
		t.Errorf("filter0 = %+v", filters[0])
	}
	if filters[1].Type != "enum" || len(filters[1].Options) != 2 {
		t.Errorf("filter1 = %+v", filters[1])
	}
	if strings.Contains(cleaned, "${") {
		t.Errorf("cleaned still has filter defs: %q", cleaned)
	}
}

// #!SCRIPT 内的 JS template literal ${x} 不应被当成报表过滤器解析或删除。
func TestParseFiltersSkipsScriptBody(t *testing.T) {
	content := "${month|统计月份|today|date}\n" +
		"#!SCRIPT\n" +
		"const table = 'user_api_call_record';\n" +
		"const sql = `SELECT * FROM ${table} LIMIT 1`;\n" +
		"#!END\n"
	filters, cleaned := parseFilters(content)
	if len(filters) != 1 || filters[0].Name != "month" {
		t.Fatalf("want only the month filter, got %+v", filters)
	}
	for _, f := range filters {
		if f.Name == "table" {
			t.Fatalf("JS template literal ${table} leaked into filters")
		}
	}
	// 脚本体内的 ${table} 必须原样保留, 不能被删除。
	if !strings.Contains(cleaned, "${table}") {
		t.Errorf("script body ${table} was stripped: %q", cleaned)
	}
}

func TestMacroValuesDateRange(t *testing.T) {
	filters := []FilterDef{{Name: "date", Type: "date_range", Default: "-7 days,today"}}
	m := macroValues(filters, map[string]string{})
	if m["from_date"] == "" || m["to_date"] == "" {
		t.Fatalf("date_range should expand to from/to: %+v", m)
	}
}

func TestApplyMacrosConditionalColumn(t *testing.T) {
	sql := "SELECT id,\n  income -- {?show_income}\nFROM t"
	// show_income empty -> income line dropped
	got := applyMacros(sql, map[string]string{"show_income": ""})
	if strings.Contains(got, "income") {
		t.Errorf("income column should be dropped: %q", got)
	}
	// show_income set -> income kept, comment stripped
	got = applyMacros(sql, map[string]string{"show_income": "1"})
	if !strings.Contains(got, "income") || strings.Contains(got, "{?show_income}") {
		t.Errorf("income should be kept without marker: %q", got)
	}
}

// 多选 enum 为空时, {name} 是 SQL in-list "”", 但 {?name} 行条件应判它为空 (看 _raw),
// 从而删掉 `AND x IN ({name}) -- {?name}` 整行 = 不过滤; 有选时保留并展开。
func TestApplyMacrosMultiSelectEmptyDropsLine(t *testing.T) {
	sql := "SELECT * FROM t WHERE 1=1\n  AND service IN ({service}) -- {?service}"

	// 空选: {service}="''" (SQL 编码), {service_raw}="" -> 整行删除
	empty := map[string]string{"service": "''", "service_raw": ""}
	got := applyMacros(sql, empty)
	if strings.Contains(got, "service IN") {
		t.Errorf("空选时 IN 行应被删除 (不过滤): %q", got)
	}

	// 有选: 行保留, {service} 展开为 in-list, 行尾条件注释去掉
	picked := map[string]string{"service": "'a','b'", "service_raw": "a,b"}
	got = applyMacros(sql, picked)
	if !strings.Contains(got, "service IN ('a','b')") || strings.Contains(got, "{?service}") {
		t.Errorf("有选时应保留并展开: %q", got)
	}
}

func TestApplyMacrosSubstitute(t *testing.T) {
	sql := "WHERE d >= '{from_date}' AND d <= '{to_date}'"
	got := applyMacros(sql, map[string]string{"from_date": "2026-01-01", "to_date": "2026-01-07"})
	if !strings.Contains(got, "2026-01-01") || !strings.Contains(got, "2026-01-07") {
		t.Errorf("macros not substituted: %q", got)
	}
}

func TestParseBlockAnnotations(t *testing.T) {
	raw := "-- @id=sales\n-- @chart=line:day,amount\nSELECT day, amount FROM s;"
	b := parseBlock(raw)
	if b.annotations["id"] != "sales" {
		t.Errorf("id annotation = %v", b.annotations["id"])
	}
	if b.annotations["chart"] != "line:day,amount" {
		t.Errorf("chart annotation = %v", b.annotations["chart"])
	}
	if b.kind != "sql" {
		t.Errorf("kind = %s", b.kind)
	}
}

func TestParseBlockColumnConfig(t *testing.T) {
	raw := "SELECT\n  amount AS \"金额\" -- @{\"header\":\"消耗金额\"}\nFROM t;"
	b := parseBlock(raw)
	cfg, ok := b.colConfigs["金额"]
	if !ok {
		t.Fatalf("col config not parsed: %+v", b.colConfigs)
	}
	if cfg["header"] != "消耗金额" {
		t.Errorf("header = %v", cfg["header"])
	}
	if strings.Contains(b.body, "-- @") {
		t.Errorf("annotation not stripped from body: %q", b.body)
	}
}

func TestSplitBlocks(t *testing.T) {
	content := "SELECT 1;\nSELECT 2;\n#!MARKDOWN\nhello"
	blocks := splitBlocks(content)
	if len(blocks) != 3 {
		t.Fatalf("want 3 blocks, got %d: %v", len(blocks), blocks)
	}
}

func TestNormalizeChart(t *testing.T) {
	cols := []string{"day", "pv", "uv"}
	c := normalizeChart("__auto__", cols)
	if c.X != "day" || len(c.Series) != 2 {
		t.Errorf("auto chart = %+v", c)
	}
	c = normalizeChart("pie:cat,val", cols)
	if c.Type != "pie" || c.Name != "cat" || c.Value != "val" {
		t.Errorf("pie chart = %+v", c)
	}
	c = normalizeChart("bar:pv", cols)
	if c.Type != "bar" || c.X != "day" || len(c.Series) != 1 {
		t.Errorf("bar chart = %+v", c)
	}
}

func TestDetectKind(t *testing.T) {
	cases := map[string]string{
		"SELECT 1":       "sql",
		"WITH x AS ()":   "sql",
		"#!MARKDOWN\nhi": "markdown",
		"#!RAW\nhi":      "raw",
		"":               "empty",
	}
	for body, want := range cases {
		if got := detectKind(body); got != want {
			t.Errorf("detectKind(%q) = %s, want %s", body, got, want)
		}
	}
}

func TestApplyModifier(t *testing.T) {
	cases := []struct{ val, mod, want string }{
		{"2026-06-24", "Y-m-01", "2026-06-01"},
		{"2026-06-24", "first_day_of_month|Y-m-d", "2026-06-01"},
		{"2026-06-24", "last_day_of_month|Y-m-d", "2026-06-30"},
		{"2026-02-15", "last_day_of_month|Y-m-d", "2026-02-28"},
		{"2026-06-24", "first_day_of_month", "2026-06-01"},
		{"2026-06-24", "Y-m", "2026-06"},
		{"2026-06-24", "+1 month|Y-m-01", "2026-07-01"},
		{"2026-06-24", "-1 day|Y-m-d", "2026-06-23"},
		{"2026-06-24", "first_day_of_year|Y-m-d", "2026-01-01"},
		{"2026-06-24 13:05:09", "Y-m-d H:i:s", "2026-06-24 13:05:09"},
		{"2026-06-24", "raw", "2026-06-24"},
		{"not-a-date", "Y-m-01", "not-a-date"},
	}
	for _, c := range cases {
		if got := applyModifier(c.val, c.mod); got != c.want {
			t.Errorf("applyModifier(%q, %q) = %q, want %q", c.val, c.mod, got, c.want)
		}
	}
}

func TestSubstituteWithFormat(t *testing.T) {
	out := substitute("WHERE x >= '{month_start[Y-m-01]}'", map[string]string{"month_start": "2026-06-24"})
	if out != "WHERE x >= '2026-06-01'" {
		t.Fatalf("got %q", out)
	}
}

func TestParseFilterTypeFormat(t *testing.T) {
	typ, format, _, _ := parseFilterType("date_range[Y-m]")
	if typ != "date_range" || format != "Y-m" {
		t.Fatalf("got type=%q format=%q", typ, format)
	}
	typ, format, opts, _ := parseFilterType("enum(1:是,0:否)")
	if typ != "enum" || format != "" || len(opts) != 2 {
		t.Fatalf("enum: type=%q format=%q opts=%d", typ, format, len(opts))
	}
}

func TestParseFilterTypeMultiple(t *testing.T) {
	typ, _, opts, multiple := parseFilterType("enum.multiple(1:是,0:否)")
	if typ != "enum" || !multiple || len(opts) != 2 {
		t.Fatalf("multiple enum: type=%q multiple=%v opts=%d", typ, multiple, len(opts))
	}
	// 普通 enum 不是多选
	if _, _, _, m := parseFilterType("enum(1:是)"); m {
		t.Error("普通 enum 不应是多选")
	}
}

func TestPhpFormatToGoLayout(t *testing.T) {
	cases := map[string]string{
		"Y-m":         "2006-01",
		"Y-m-d":       "2006-01-02",
		"Y-m-d H:i:s": "2006-01-02 15:04:05",
	}
	for in, want := range cases {
		if got := phpFormatToGoLayout(in); got != want {
			t.Errorf("phpFormatToGoLayout(%q)=%q want %q", in, got, want)
		}
	}
}

func TestDefaultValueLiteralFormat(t *testing.T) {
	// date_range[Y-m-01]: "01" 必须作为字面量保留, 而非被当成 Go 布局的月份 token
	f := FilterDef{Name: "month", Type: "date_range", Format: "Y-m-01", Default: "today"}
	got := defaultValue(f)
	// today 的当月第一天, from==to
	parts := strings.SplitN(got, ",", 2)
	if len(parts) != 2 || parts[0] != parts[1] {
		t.Fatalf("range default = %q", got)
	}
	// 必须以 -01 结尾 (当月第一天)
	if !strings.HasSuffix(parts[0], "-01") {
		t.Errorf("expected day '01', got %q", parts[0])
	}
	// 形如 YYYY-MM-01
	if !regexp.MustCompile(`^\d{4}-\d{2}-01$`).MatchString(parts[0]) {
		t.Errorf("format wrong: %q", parts[0])
	}
}

func TestApplyCellTransformsEnum(t *testing.T) {
	cols := []Column{{Name: "status", Header: "状态", Config: map[string]any{"enum": "1:成功,0:失败"}}}
	rows := []map[string]any{{"status": "1"}, {"status": "0"}, {"status": "9"}}
	applyCellTransforms(cols, rows)
	if rows[0]["status"] != "成功" || rows[1]["status"] != "失败" {
		t.Fatalf("enum map failed: %+v", rows)
	}
	if rows[2]["status"] != "9" { // 未命中保持原值
		t.Errorf("unmapped should stay: %v", rows[2]["status"])
	}
}

func TestApplyCellTransformsRatio(t *testing.T) {
	cols := []Column{{Name: "rate", Config: map[string]any{"ratio": 1}}}
	rows := []map[string]any{{"rate": 0.25}}
	applyCellTransforms(cols, rows)
	if rows[0]["rate"] != "25.00%" {
		t.Fatalf("ratio failed: %v", rows[0]["rate"])
	}
}

func TestComputeSummary(t *testing.T) {
	cols := []Column{
		{Name: "day"},
		{Name: "amount", Config: map[string]any{"count": true}},
	}
	rows := []map[string]any{
		{"day": "d1", "amount": 10},
		{"day": "d2", "amount": 20},
	}
	sum, avg := computeSummary(cols, rows, true, true)
	if sum["amount"] != "30" {
		t.Errorf("sum amount = %v, want 30", sum["amount"])
	}
	if avg["amount"] != "15.00" {
		t.Errorf("avg amount = %v, want 15.00", avg["amount"])
	}
	if sum["day"] != "合计" || avg["day"] != "平均" {
		t.Errorf("label cols: sum.day=%v avg.day=%v", sum["day"], avg["day"])
	}
}

func TestComputeSummaryAllNumeric(t *testing.T) {
	// 无显式 count, 对全部数值列汇总, 非数值列跳过
	cols := []Column{{Name: "name"}, {Name: "a"}, {Name: "b"}}
	rows := []map[string]any{{"name": "x", "a": 1, "b": 2}, {"name": "y", "a": 3, "b": 4}}
	sum, _ := computeSummary(cols, rows, true, false)
	if sum["a"] != "4" || sum["b"] != "6" {
		t.Fatalf("sum = %+v", sum)
	}
}

func TestInjectLimit(t *testing.T) {
	cases := []struct {
		in      string
		n       int
		wantHas string
	}{
		{"SELECT * FROM t", 1000, "LIMIT 1000"},
		{"SELECT * FROM t;", 500, "LIMIT 500"},
		{"select * from t limit 10", 1000, ""},    // 已有 limit, 不加
		{"SELECT * FROM t LIMIT 5, 10", 1000, ""}, // 带 offset 的 limit
		{"WITH x AS (SELECT 1) SELECT * FROM x", 100, "LIMIT 100"},
		{"SHOW TABLES", 1000, ""}, // 非 select/with, 不加
	}
	for _, c := range cases {
		got := injectLimit(c.in, c.n)
		if c.wantHas == "" {
			if strings.Count(strings.ToUpper(got), "LIMIT") != strings.Count(strings.ToUpper(c.in), "LIMIT") {
				t.Errorf("injectLimit(%q) should not add limit, got %q", c.in, got)
			}
		} else if !strings.Contains(got, c.wantHas) {
			t.Errorf("injectLimit(%q) = %q, want contains %q", c.in, got, c.wantHas)
		}
	}
	// n<=0 不限制
	if got := injectLimit("SELECT 1", 0); got != "SELECT 1" {
		t.Errorf("n=0 should not change: %q", got)
	}
}

func TestValidateReadOnlySQL(t *testing.T) {
	valid := []string{
		"SELECT 1",
		"select 'delete; drop' AS txt;",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"/* comment */ SELECT * FROM t",
	}
	for _, sql := range valid {
		if err := validateReadOnlySQL(sql); err != nil {
			t.Fatalf("valid SQL rejected: %q: %v", sql, err)
		}
	}

	invalid := []string{
		"UPDATE users SET name = 'x'",
		"DELETE FROM users",
		"DROP TABLE users",
		"SELECT 1; SELECT 2",
		"WITH moved AS (DELETE FROM users RETURNING id) SELECT * FROM moved",
	}
	for _, sql := range invalid {
		if err := validateReadOnlySQL(sql); err == nil {
			t.Fatalf("invalid SQL accepted: %q", sql)
		}
	}
}

func TestRunRejectsWriteSQLBlock(t *testing.T) {
	setupSQLiteDefault(t)
	res, err := NewRunner("default").Run("WITH moved AS (DELETE FROM pv RETURNING day) SELECT * FROM moved;", nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 1 || !strings.Contains(res.Blocks[0].Error, "不允许使用 DELETE") {
		t.Fatalf("write SQL should be rejected, blocks=%+v", res.Blocks)
	}
}

func TestRunScriptQueryRejectsWriteSQL(t *testing.T) {
	setupSQLiteDefault(t)
	res, err := NewRunner("default").Run(`#!SCRIPT
query('UPDATE pv SET pv = 1')
#!END`, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 1 || !strings.Contains(res.Blocks[0].Error, "仅支持只读") {
		t.Fatalf("script write query should be rejected, blocks=%+v", res.Blocks)
	}
}

func TestParseMergeCell(t *testing.T) {
	got := parseMergeCell("report_month, business_line ,")
	if len(got) != 2 || got[0] != "report_month" || got[1] != "business_line" {
		t.Fatalf("parseMergeCell = %+v", got)
	}
	if parseMergeCell("") != nil {
		t.Error("empty should be nil")
	}
}

func TestParseSeriesConfig(t *testing.T) {
	// 本项目键名
	x, s, v, ok := parseSeriesConfig(map[string]any{"x": "day", "series": "channel", "value": "amount"})
	if !ok || x != "day" || s != "channel" || v != "amount" {
		t.Fatalf("got x=%q s=%q v=%q ok=%v", x, s, v, ok)
	}
	// dataddy 键名: xAxis + 数组 series + series_value
	x, s, v, ok = parseSeriesConfig(map[string]any{
		"xAxis": "time_date", "series": []any{"event_type"}, "series_value": []any{"event_count"},
	})
	if !ok || x != "time_date" || s != "event_type" || v != "event_count" {
		t.Fatalf("dataddy keys: x=%q s=%q v=%q ok=%v", x, s, v, ok)
	}
	// 缺字段 -> 无效
	if _, _, _, ok := parseSeriesConfig(map[string]any{"x": "day"}); ok {
		t.Error("missing fields should be invalid")
	}
}

func TestPivotSeries(t *testing.T) {
	cols := []string{"day", "channel", "amount"}
	rows := []map[string]any{
		{"day": "2026-06-20", "channel": "web", "amount": 120},
		{"day": "2026-06-20", "channel": "app", "amount": 80},
		{"day": "2026-06-21", "channel": "web", "amount": 150},
	}
	nc, nr := pivotSeries(cols, rows, "day", "channel", "amount")
	// 列: day, web, app (按首次出现顺序)
	if len(nc) != 3 || nc[0] != "day" || nc[1] != "web" || nc[2] != "app" {
		t.Fatalf("cols = %+v", nc)
	}
	// 行: 2 个 day
	if len(nr) != 2 {
		t.Fatalf("rows = %+v", nr)
	}
	if nr[0]["web"] != 120 || nr[0]["app"] != 80 {
		t.Errorf("row0 = %+v", nr[0])
	}
	// 2026-06-21 没有 app, 应补 0
	if nr[1]["web"] != 150 || nr[1]["app"] != 0 {
		t.Errorf("row1 = %+v", nr[1])
	}
}

func TestComputeSummaryNoSum(t *testing.T) {
	cols := []Column{
		{Name: "name"},
		{Name: "amount"},
		{Name: "ratio", Config: map[string]any{"nosum": true}}, // 比率列排除
	}
	rows := []map[string]any{
		{"name": "a", "amount": 10, "ratio": 0.5},
		{"name": "b", "amount": 20, "ratio": 0.5},
	}
	sum, _ := computeSummary(cols, rows, true, false)
	if sum["amount"] != "30" {
		t.Errorf("amount sum = %v, want 30", sum["amount"])
	}
	if _, ok := sum["ratio"]; ok {
		t.Errorf("ratio 列应被 nosum 排除, 却出现在合计: %v", sum["ratio"])
	}
}

func TestParseEnumSQL(t *testing.T) {
	// 默认源
	sql, dsn, multiple, ok := parseEnumSQL("enum_sql(SELECT id AS value, name AS label FROM regions)")
	if !ok || dsn != "" || multiple || sql != "SELECT id AS value, name AS label FROM regions" {
		t.Fatalf("enum_sql 解析错误: sql=%q dsn=%q ok=%v", sql, dsn, ok)
	}
	// 指定源 + SQL 含括号
	sql, dsn, _, ok = parseEnumSQL("enum_sql[dim](SELECT id, name FROM t WHERE f IN (1,2))")
	if !ok || dsn != "dim" || !strings.Contains(sql, "IN (1,2)") {
		t.Fatalf("带 dsn/括号解析错误: sql=%q dsn=%q ok=%v", sql, dsn, ok)
	}
	// 多选 + 指定源
	_, dsn, multiple, ok = parseEnumSQL("enum_sql.multiple[crm](SELECT uid, nick FROM users)")
	if !ok || dsn != "crm" || !multiple {
		t.Fatalf("多选 enum_sql 解析错误: dsn=%q multiple=%v ok=%v", dsn, multiple, ok)
	}
	// 非 enum_sql
	if _, _, _, ok := parseEnumSQL("enum(1:a,0:b)"); ok {
		t.Error("普通 enum 不应匹配 enum_sql")
	}
}

func TestMacroValuesMultiple(t *testing.T) {
	filters := []FilterDef{{Name: "ch", Type: "enum", Multiple: true}}
	m := macroValues(filters, map[string]string{"ch": "web,app"})
	if m["ch"] != "'web','app'" {
		t.Errorf("多选 in-list 错误: %q", m["ch"])
	}
	if m["ch_raw"] != "web,app" {
		t.Errorf("raw 值错误: %q", m["ch_raw"])
	}
	// 注入防护: 单引号转义
	m = macroValues(filters, map[string]string{"ch": "a',drop"})
	if strings.Contains(m["ch"], "',drop") && !strings.Contains(m["ch"], "''") {
		t.Errorf("单引号未转义: %q", m["ch"])
	}
}

func TestParseFilterBodyEnumSQL(t *testing.T) {
	f := parseFilterBody("region|地区||enum_sql(SELECT id AS value, name AS label FROM regions)")
	if f.Type != "enum" {
		t.Errorf("type = %q, want enum", f.Type)
	}
	if f.optionSQL == "" {
		t.Errorf("optionSQL 未填充: %+v", f)
	}
}

func TestRowsToOptions(t *testing.T) {
	// 显式 value/label 列
	opts := rowsToOptions([]string{"value", "label"}, []map[string]any{{"value": "1", "label": "华东"}})
	if len(opts) != 1 || opts[0].Value != "1" || opts[0].Label != "华东" {
		t.Fatalf("value/label 列映射错误: %+v", opts)
	}
	// 单列: value=label
	opts = rowsToOptions([]string{"name"}, []map[string]any{{"name": "华东"}})
	if opts[0].Value != "华东" || opts[0].Label != "华东" {
		t.Errorf("单列映射错误: %+v", opts)
	}
}

package engine

import (
	"testing"
	"time"
)

// ---- @filter ----

func TestApplyResultFilterAND(t *testing.T) {
	conds := parseFilterConfig([]any{
		[]any{"status", "=", "active"},
		[]any{"age", ">=", "18"},
	})
	if conds == nil {
		t.Fatal("filter config not parsed")
	}
	rows := []map[string]any{
		{"status": "active", "age": 25},
		{"status": "inactive", "age": 30},
		{"status": "active", "age": 17},
	}
	out := applyResultFilter(conds, rows)
	if len(out) != 1 || out[0]["age"] != 25 {
		t.Fatalf("filter result = %+v, want 1 row age=25", out)
	}
}

func TestFilterOps(t *testing.T) {
	cases := []struct {
		op, val string
		cell    any
		want    bool
	}{
		{"!=", "x", "y", true},
		{">", "100", 150, true},
		{"<=", "100", 100, true},
		{"in", "a,b,c", "b", true},
		{"not in", "a,b", "c", true},
		{"between", "10,20", 15, true},
		{"between", "10,20", 20, false}, // 开区间
	}
	for _, c := range cases {
		got := filterCond{field: "f", op: c.op, value: c.val}.match(c.cell)
		if got != c.want {
			t.Errorf("%s %q cell=%v = %v, want %v", c.op, c.val, c.cell, got, c.want)
		}
	}
}

// ---- @sort ----

func TestApplySortMultiField(t *testing.T) {
	items := parseSortConfig("+score,-id")
	rows := []map[string]any{
		{"score": 85, "id": 1},
		{"score": 92, "id": 2},
		{"score": 85, "id": 3},
	}
	applySort(items, rows)
	// score 升序: 85,85,92; 同分 id 降序: id3 在 id1 前
	if rows[0]["id"] != 3 || rows[1]["id"] != 1 || rows[2]["id"] != 2 {
		t.Fatalf("sort order = %+v", rows)
	}
}

func TestApplySortGroup(t *testing.T) {
	// 按 dept 分组, 组总额降序; 组内 revenue 降序。
	items := parseSortConfig("-revenue(dept)")
	rows := []map[string]any{
		{"dept": "A", "revenue": 100},
		{"dept": "B", "revenue": 150},
		{"dept": "A", "revenue": 200}, // A 组总额 300 > B 组 150
	}
	applySort(items, rows)
	if rows[0]["dept"] != "A" || rows[1]["dept"] != "A" || rows[2]["dept"] != "B" {
		t.Fatalf("group sort = %+v, want A,A,B", rows)
	}
	if rows[0]["revenue"] != 200 { // 组内降序
		t.Errorf("intra-group order wrong: %+v", rows)
	}
}

// ---- @date_line ----

func TestApplyDateLineFillsGaps(t *testing.T) {
	cfg, ok := parseDateLineConfig(map[string]any{"field": "day", "format": "Y-m-d"})
	if !ok {
		t.Fatal("date_line config not parsed")
	}
	rows := []map[string]any{
		{"day": "2026-01-01", "v": 1},
		{"day": "2026-01-03", "v": 3},
	}
	out := applyDateLine(cfg, []string{"day", "v"}, rows)
	if len(out) != 3 {
		t.Fatalf("date_line len = %d, want 3 (%+v)", len(out), out)
	}
	if out[1]["day"] != "2026-01-02" {
		t.Errorf("gap day = %v, want 2026-01-02", out[1]["day"])
	}
	if _, has := out[1]["v"]; has {
		t.Errorf("filled row should only carry date field: %+v", out[1])
	}
}

func TestDateLineRelativeStart(t *testing.T) {
	old := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, 1, 5, 0, 0, 0, 0, time.Local) }
	defer func() { nowFunc = old }()

	cfg, _ := parseDateLineConfig(map[string]any{"field": "day", "start": "-2 days", "format": "Y-m-d"})
	rows := []map[string]any{{"day": "2026-01-05", "v": 9}}
	out := applyDateLine(cfg, []string{"day", "v"}, rows)
	// start = 今天-2天 = 01-03; 到 01-05 共 3 行
	if len(out) != 3 || out[0]["day"] != "2026-01-03" {
		t.Fatalf("relative start = %+v", out)
	}
}

// ---- @flip ----

func TestApplyFlipWithKey(t *testing.T) {
	cfg, ok := parseFlipConfig(map[string]any{"key": "product"})
	if !ok {
		t.Fatal("flip config not parsed")
	}
	cols := []string{"product", "q1", "q2"}
	rows := []map[string]any{
		{"product": "A", "q1": 10, "q2": 20},
		{"product": "B", "q1": 30, "q2": 40},
	}
	nc, nr, done := applyFlip(cfg, cols, rows)
	if !done {
		t.Fatal("flip should succeed")
	}
	// 新列: 名称 + product[A] + product[B]
	if nc[0] != "名称" || nc[1] != "product[A]" || nc[2] != "product[B]" {
		t.Fatalf("flip cols = %+v", nc)
	}
	// 两行: q1 / q2
	if len(nr) != 2 || nr[0]["名称"] != "q1" || nr[0]["product[A]"] != 10 || nr[1]["product[B]"] != 40 {
		t.Fatalf("flip rows = %+v", nr)
	}
}

func TestApplyFlipRowLimit(t *testing.T) {
	rows := make([]map[string]any, 51)
	for i := range rows {
		rows[i] = map[string]any{"k": i}
	}
	_, _, done := applyFlip(flipConfig{}, []string{"k"}, rows)
	if done {
		t.Error("flip should refuse >50 rows")
	}
}

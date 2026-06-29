package engine

import "testing"

func TestApplyCellTransformsDate(t *testing.T) {
	cols := []Column{{Name: "d", Config: map[string]any{"date": "Y/m/d"}}}
	rows := []map[string]any{{"d": "2026-06-25 13:40:00"}, {"d": "not-a-date"}}
	applyCellTransforms(cols, rows)
	if rows[0]["d"] != "2026/06/25" {
		t.Errorf("date format = %v, want 2026/06/25", rows[0]["d"])
	}
	if rows[1]["d"] != "not-a-date" { // 不可解析保持原值
		t.Errorf("unparseable should stay: %v", rows[1]["d"])
	}
}

func TestApplyCellTransformsTime2str(t *testing.T) {
	cols := []Column{{Name: "ts", Config: map[string]any{"time2str": "Y-m-d"}}}
	// 1750000000 = 2025-06-15 (UTC); 用 Local 解释, 断言年份足够稳健
	rows := []map[string]any{{"ts": 1750000000}}
	applyCellTransforms(cols, rows)
	got, _ := rows[0]["ts"].(string)
	if len(got) != 10 || got[:2] != "20" {
		t.Errorf("time2str = %v, want YYYY-MM-DD", rows[0]["ts"])
	}
}

func TestApplyPercentColumns(t *testing.T) {
	cols := []Column{
		{Name: "done", Config: map[string]any{"percent": map[string]any{"base": "total", "succ": float64(70), "warn": float64(40)}}},
		{Name: "total"},
	}
	rows := []map[string]any{
		{"done": 90, "total": 100}, // 90% -> success
		{"done": 50, "total": 100}, // 50% -> warning
		{"done": 10, "total": 100}, // 10% -> danger
	}
	attrs := applyPercentColumns(cols, rows, nil)
	if rows[0]["done"] != "90%" {
		t.Errorf("row0 value = %v, want 90%%", rows[0]["done"])
	}
	col := attrs["done"]
	if col == nil {
		t.Fatal("done col attrs missing")
	}
	if col["0"].Type != "success" || col["1"].Type != "warning" || col["2"].Type != "danger" {
		t.Errorf("types = %v/%v/%v", col["0"].Type, col["1"].Type, col["2"].Type)
	}
}

// format (money/integer/percent 等) 是纯展示格式, 不应在后端把 rows 改成字符串 —— 否则
// 图表/排序/汇总读到 "14,112" 这种带逗号字符串会算错。这里断言数值保持原样, 由前端渲染时格式化。
func TestApplyCellTransformsKeepsRawForFormat(t *testing.T) {
	cols := []Column{
		{Name: "total", Config: map[string]any{"format": "integer"}},
		{Name: "gmv", Config: map[string]any{"format": "money"}},
	}
	rows := []map[string]any{{"total": 14112, "gmv": 9999.5}}
	applyCellTransforms(cols, rows)
	if rows[0]["total"] != 14112 {
		t.Errorf("format 列应保持原始数值, got %v (%T)", rows[0]["total"], rows[0]["total"])
	}
	if rows[0]["gmv"] != 9999.5 {
		t.Errorf("money 列应保持原始数值, got %v (%T)", rows[0]["gmv"], rows[0]["gmv"])
	}
}

func TestApplyPercentConstBaseAndDot(t *testing.T) {
	cols := []Column{{Name: "p", Config: map[string]any{"percent": map[string]any{"base": float64(200), "dot": float64(1)}}}}
	rows := []map[string]any{{"p": 50}} // 50/200*100 = 25.0%
	applyPercentColumns(cols, rows, nil)
	if rows[0]["p"] != "25.0%" {
		t.Errorf("value = %v, want 25.0%%", rows[0]["p"])
	}
}

func TestApplyPercentZeroBaseSkipped(t *testing.T) {
	cols := []Column{{Name: "p", Config: map[string]any{"percent": map[string]any{"base": "b"}}}}
	rows := []map[string]any{{"p": 5, "b": 0}}
	attrs := applyPercentColumns(cols, rows, nil)
	if attrs != nil {
		t.Errorf("base=0 应跳过, got %+v", attrs)
	}
	if rows[0]["p"] != 5 {
		t.Errorf("base=0 值不应改写: %v", rows[0]["p"])
	}
}

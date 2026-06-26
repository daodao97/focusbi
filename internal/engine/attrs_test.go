package engine

import "testing"

func TestBuildCellAttrs(t *testing.T) {
	cols := []Column{{Name: "status", Config: map[string]any{"tag": "1:success:已完成,0:danger,default:info"}}}
	rows := []map[string]any{{"status": "1"}, {"status": "0"}, {"status": "9"}}
	attrs := buildCellAttrs(cols, rows)
	col := attrs["status"]
	if col == nil {
		t.Fatalf("status col attrs missing: %+v", attrs)
	}
	if col["0"].Type != "success" || col["0"].Text != "已完成" {
		t.Errorf("row0 = %+v, want success/已完成", col["0"])
	}
	if col["1"].Type != "danger" || col["1"].Text != "" {
		t.Errorf("row1 = %+v, want danger/空文本", col["1"])
	}
	if col["2"].Type != "info" { // 命中 default
		t.Errorf("row2 = %+v, want default info", col["2"])
	}
}

func TestBuildCellAttrsNoDefault(t *testing.T) {
	cols := []Column{{Name: "s", Config: map[string]any{"tag": "1:success"}}}
	rows := []map[string]any{{"s": "1"}, {"s": "2"}}
	attrs := buildCellAttrs(cols, rows)
	if attrs["s"]["0"] == nil {
		t.Fatalf("row0 should have tag")
	}
	if _, ok := attrs["s"]["1"]; ok { // 未命中且无 default
		t.Errorf("row1 should have no tag")
	}
}

func TestParseTagConfigInvalid(t *testing.T) {
	if parseTagConfig("") != nil {
		t.Error("empty -> nil")
	}
	if parseTagConfig(123) != nil {
		t.Error("non-string -> nil")
	}
	if parseTagConfig("1:notatype") != nil { // 非法 type 被丢弃, 整体为空
		t.Error("invalid type -> nil")
	}
}

func TestBuildRowAttrsCondition(t *testing.T) {
	ann := map[string]any{"when": "amount>=100", "class": "row-success"}
	rows := []map[string]any{{"amount": 50}, {"amount": 100}, {"amount": 200}}
	attrs := buildRowAttrs(ann, rows)
	if _, ok := attrs["0"]; ok {
		t.Errorf("row0 (50) should not match")
	}
	if attrs["1"].Class != "row-success" || attrs["2"].Class != "row-success" {
		t.Errorf("row1/2 should match: %+v", attrs)
	}
}

func TestBuildRowAttrsMultiRule(t *testing.T) {
	ann := []any{
		map[string]any{"when": "level==vip", "class": "row-bold"},
		map[string]any{"when": "amount<0", "class": "row-danger"},
	}
	rows := []map[string]any{
		{"level": "vip", "amount": -5}, // 两条都命中, class 追加
		{"level": "normal", "amount": 10},
	}
	attrs := buildRowAttrs(ann, rows)
	if attrs["0"].Class != "row-bold row-danger" {
		t.Errorf("row0 class = %q, want both", attrs["0"].Class)
	}
	if _, ok := attrs["1"]; ok {
		t.Errorf("row1 should not match")
	}
}

func TestBuildRowAttrsStringEq(t *testing.T) {
	ann := map[string]any{"when": `name!="total"`, "class": "row-info"}
	rows := []map[string]any{{"name": "total"}, {"name": "alice"}}
	attrs := buildRowAttrs(ann, rows)
	if _, ok := attrs["0"]; ok {
		t.Errorf("row0 (total) should not match !=total")
	}
	if attrs["1"].Class != "row-info" {
		t.Errorf("row1 should match: %+v", attrs)
	}
}

func TestBuildRowAttrsNone(t *testing.T) {
	if buildRowAttrs(nil, []map[string]any{{"a": 1}}) != nil {
		t.Error("nil annotation -> nil")
	}
	// 缺 class 的规则被忽略
	if buildRowAttrs(map[string]any{"when": "a==1"}, []map[string]any{{"a": "1"}}) != nil {
		t.Error("no class -> nil")
	}
}

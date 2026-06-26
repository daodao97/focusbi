package engine

import (
	"strings"
	"testing"
)

func TestParseFluctuationsConfig(t *testing.T) {
	// 字符串字段 + 默认阈值
	fields, th, ok := parseFluctuationsConfig(map[string]any{"field": "consume"})
	if !ok || len(fields) != 1 || fields[0] != "consume" || th != defaultFluctuationThreshold {
		t.Fatalf("string field = %+v th=%v ok=%v", fields, th, ok)
	}
	// 数组字段 + 自定义阈值
	fields, th, ok = parseFluctuationsConfig(map[string]any{
		"field":             []any{"consume", "orders"},
		"threshold_percent": float64(30),
	})
	if !ok || len(fields) != 2 || th != 30 {
		t.Fatalf("array field = %+v th=%v ok=%v", fields, th, ok)
	}
	// 非 map / 空字段 -> 不启用
	if _, _, ok := parseFluctuationsConfig("nope"); ok {
		t.Fatal("string config 应 ok=false")
	}
	if _, _, ok := parseFluctuationsConfig(map[string]any{"field": ""}); ok {
		t.Fatal("空字段应 ok=false")
	}
}

func TestDetectFluctuationsOverThreshold(t *testing.T) {
	cols := []string{"day", "consume"}
	rows := []map[string]any{
		{"day": "2026-06-24", "consume": 200}, // 最新
		{"day": "2026-06-23", "consume": 100}, // 上期 -> +100%
	}
	msgs := detectFluctuations(cols, rows, []string{"consume"}, 50)
	if len(msgs) != 1 {
		t.Fatalf("want 1 msg, got %+v", msgs)
	}
	m := msgs[0]
	for _, want := range []string{"2026-06-24", "2026-06-23", "consume", "100 => 200", "+100%"} {
		if !strings.Contains(m, want) {
			t.Errorf("msg %q 缺少 %q", m, want)
		}
	}
}

func TestDetectFluctuationsUnderThreshold(t *testing.T) {
	cols := []string{"day", "consume"}
	rows := []map[string]any{
		{"day": "2026-06-24", "consume": 110},
		{"day": "2026-06-23", "consume": 100}, // +10%, 低于阈值 50
	}
	if msgs := detectFluctuations(cols, rows, []string{"consume"}, 50); msgs != nil {
		t.Fatalf("低于阈值不应产出消息, got %+v", msgs)
	}
}

func TestDetectFluctuationsUnsortedInput(t *testing.T) {
	// 输入乱序: 应按首列降序自行取最近两期。
	cols := []string{"day", "v"}
	rows := []map[string]any{
		{"day": "2026-06-22", "v": 50},
		{"day": "2026-06-24", "v": 300}, // 最新
		{"day": "2026-06-23", "v": 100}, // 上期 -> +200%
	}
	msgs := detectFluctuations(cols, rows, []string{"v"}, 50)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "100 => 300") {
		t.Fatalf("乱序取最近两期失败: %+v", msgs)
	}
}

func TestDetectFluctuationsMissingField(t *testing.T) {
	cols := []string{"day", "consume"}
	rows := []map[string]any{
		{"day": "2026-06-24"}, // 最新期缺该字段
		{"day": "2026-06-23", "consume": 100},
	}
	msgs := detectFluctuations(cols, rows, []string{"consume"}, 50)
	if len(msgs) != 1 || !strings.Contains(msgs[0], "数据缺失") {
		t.Fatalf("缺失字段应提示数据缺失: %+v", msgs)
	}
}

func TestDetectFluctuationsSingleRow(t *testing.T) {
	cols := []string{"day", "v"}
	rows := []map[string]any{{"day": "2026-06-24", "v": 100}}
	if msgs := detectFluctuations(cols, rows, []string{"v"}, 50); msgs != nil {
		t.Fatalf("单行不应比较, got %+v", msgs)
	}
}

package engine

import "testing"

func TestParseKpiConfig(t *testing.T) {
	// 对象式 {items:[...]}
	v := map[string]any{"items": []any{
		map[string]any{"label": "GMV", "value": "销售额", "compare": "上期", "format": "money", "trend": "销售额"},
		map[string]any{"label": "订单", "value": "订单数"},
	}}
	cfg := parseKpiConfig(v)
	if cfg == nil || len(cfg.Items) != 2 {
		t.Fatalf("want 2 items, got %+v", cfg)
	}
	if cfg.Items[0].Value != "销售额" || cfg.Items[0].Compare != "上期" || cfg.Items[0].Format != "money" {
		t.Errorf("item0 = %+v", cfg.Items[0])
	}
}

func TestParseKpiConfigBareArray(t *testing.T) {
	v := []any{map[string]any{"value": "pv"}}
	cfg := parseKpiConfig(v)
	if cfg == nil || len(cfg.Items) != 1 || cfg.Items[0].Value != "pv" {
		t.Fatalf("bare array = %+v", cfg)
	}
	// label 缺省回退到 value
	if cfg.Items[0].Label != "pv" {
		t.Errorf("label default = %q", cfg.Items[0].Label)
	}
}

func TestParseKpiConfigSkipsInvalid(t *testing.T) {
	// value 为空的卡片被跳过; 全空返回 nil
	v := map[string]any{"items": []any{
		map[string]any{"label": "无值"},
	}}
	if cfg := parseKpiConfig(v); cfg != nil {
		t.Errorf("want nil for all-invalid items, got %+v", cfg)
	}
	if cfg := parseKpiConfig("not json"); cfg != nil {
		t.Errorf("want nil for non-json string, got %+v", cfg)
	}
}

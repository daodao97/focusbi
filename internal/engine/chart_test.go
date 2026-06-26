package engine

import (
	"reflect"
	"testing"
)

// 字符串式 @chart 各类型解析。
func TestParseChartStringTypes(t *testing.T) {
	cols := []string{"day", "pv", "uv"}
	cases := []struct {
		in   string
		want *ChartConfig
	}{
		{"scatter:pv,uv", &ChartConfig{Type: "scatter", X: "pv", Y: "uv"}},
		{"area:pv,uv", &ChartConfig{Type: "area", X: "day", Series: []string{"pv", "uv"}}},
		{"area", &ChartConfig{Type: "area", X: "day", Series: []string{"pv", "uv"}}},
		{"funnel:day,pv", &ChartConfig{Type: "funnel", Name: "day", Value: "pv"}},
		{"gauge:pv", &ChartConfig{Type: "gauge", Value: "pv"}},
		{"radar:pv,uv", &ChartConfig{Type: "radar", Series: []string{"pv", "uv"}}},
	}
	for _, c := range cases {
		got := parseChartString(c.in, cols)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseChartString(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

// 字符串式缺省列推断: scatter/funnel/gauge 不写字段时取列。
func TestParseChartStringDefaults(t *testing.T) {
	cols := []string{"a", "b", "c"}
	if got := parseChartString("scatter", cols); got.X != "a" || got.Y != "b" {
		t.Errorf("scatter default = %+v", got)
	}
	if got := parseChartString("funnel", cols); got.Name != "a" || got.Value != "b" {
		t.Errorf("funnel default = %+v", got)
	}
	if got := parseChartString("gauge", cols); got.Value != "b" {
		t.Errorf("gauge default = %+v", got)
	}
}

// 对象式 @chart 各类型解析 + stack。
func TestMapChartTypes(t *testing.T) {
	cols := []string{"day", "pv", "uv"}
	cases := []struct {
		name string
		in   map[string]any
		want *ChartConfig
	}{
		{"scatter", map[string]any{"type": "scatter", "x": "pv", "y": "uv"}, &ChartConfig{Type: "scatter", X: "pv", Y: "uv"}},
		{"funnel", map[string]any{"type": "funnel", "name": "day", "value": "pv"}, &ChartConfig{Type: "funnel", Name: "day", Value: "pv"}},
		{"gauge", map[string]any{"type": "gauge", "value": "pv"}, &ChartConfig{Type: "gauge", Value: "pv"}},
		{"radar", map[string]any{"type": "radar", "series": []any{"pv", "uv"}}, &ChartConfig{Type: "radar", Series: []string{"pv", "uv"}}},
		{"bar-stack", map[string]any{"type": "bar", "x": "day", "series": []any{"pv", "uv"}, "stack": true}, &ChartConfig{Type: "bar", X: "day", Series: []string{"pv", "uv"}, Stack: true}},
		{"area", map[string]any{"type": "area", "y": []any{"pv"}}, &ChartConfig{Type: "area", X: "day", Series: []string{"pv"}}},
	}
	for _, c := range cases {
		got := mapChart(c.in, cols)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("mapChart(%s) = %+v, want %+v", c.name, got, c.want)
		}
	}
}

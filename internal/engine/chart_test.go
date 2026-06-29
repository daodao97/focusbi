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

// 显式多维 X 轴: bar[x=服务/处理方式]:总数 → XFields, Series 取指定列。
func TestParseChartExplicitXFields(t *testing.T) {
	cols := []string{"服务", "处理方式", "总数", "成功数"}
	c := parseChartString("bar[x=服务/处理方式]:总数,成功数", cols)
	if c.Type != "bar" {
		t.Fatalf("type = %q, want bar", c.Type)
	}
	if len(c.XFields) != 2 || c.XFields[0] != "服务" || c.XFields[1] != "处理方式" {
		t.Fatalf("XFields = %v, want [服务 处理方式]", c.XFields)
	}
	if c.X != "" {
		t.Errorf("多维 X 轴不应再设单维 X: %q", c.X)
	}
	if len(c.Series) != 2 || c.Series[0] != "总数" {
		t.Errorf("Series = %v", c.Series)
	}
}

// 多维 X 轴不指定数值列时, Series 默认取剩余非 X 列。
func TestParseChartXFieldsDefaultSeries(t *testing.T) {
	cols := []string{"服务", "处理方式", "总数", "成功数"}
	c := parseChartString("bar[x=服务/处理方式]", cols)
	if len(c.Series) != 2 || c.Series[0] != "总数" || c.Series[1] != "成功数" {
		t.Errorf("默认 Series 应为剩余列, got %v", c.Series)
	}
}

// 对象写法: x 为数组 → XFields; 字符串 → X。
func TestMapChartXArray(t *testing.T) {
	cols := []string{"a", "b", "v"}
	c := mapChart(map[string]any{"type": "bar", "x": []any{"a", "b"}, "series": []any{"v"}}, cols)
	if len(c.XFields) != 2 || c.XFields[0] != "a" || c.XFields[1] != "b" {
		t.Fatalf("XFields = %v", c.XFields)
	}
	c2 := mapChart(map[string]any{"type": "bar", "x": "a"}, cols)
	if c2.X != "a" || len(c2.XFields) != 0 {
		t.Errorf("字符串 x 应为单维 X: X=%q XFields=%v", c2.X, c2.XFields)
	}
}

// X 轴重复值提示: 单维重复触发, 多维区分后不触发。
func TestChartXDupNotice(t *testing.T) {
	rows := []map[string]any{
		{"服务": "GPT", "处理方式": "代付", "v": 1},
		{"服务": "GPT", "处理方式": "退款", "v": 2}, // 服务列重复, 但 (服务,处理方式) 唯一
	}
	if chartXDupNotice(&ChartConfig{Type: "bar", X: "服务"}, rows) == "" {
		t.Error("单维 X=服务 有重复值, 应提示")
	}
	if chartXDupNotice(&ChartConfig{Type: "bar", XFields: []string{"服务", "处理方式"}}, rows) != "" {
		t.Error("多维 X 轴区分后无重复, 不应提示")
	}
	if chartXDupNotice(&ChartConfig{Type: "pie", Name: "服务"}, rows) != "" {
		t.Error("非类目轴族不检查重复")
	}
}

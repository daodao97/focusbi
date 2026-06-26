package engine

import "strings"

// normalizeChart 把报表中的 @chart 注解规整为前端可消费的 ChartConfig。
//
// 支持的形式:
//   - "__auto__"            : 第一列为 X 轴, 其余列为数值序列
//   - "line:f1,f2"          : 折线图, 指定数值列
//   - "bar:f1,f2"           : 柱状图
//   - "pie:category,value"  : 饼图
//   - {object}              : 已是对象, 透传 (尽量映射常见字段)
func normalizeChart(chart any, cols []string) *ChartConfig {
	switch v := chart.(type) {
	case bool:
		if !v {
			return nil
		}
		return autoChart(cols)
	case string:
		return parseChartString(v, cols)
	case map[string]any:
		return mapChart(v, cols)
	default:
		return autoChart(cols)
	}
}

func parseChartString(s string, cols []string) *ChartConfig {
	s = strings.TrimSpace(s)
	if s == "" || s == "__auto__" || s == "auto" {
		return autoChart(cols)
	}

	typ := s
	var fields []string
	if i := strings.Index(s, ":"); i >= 0 {
		typ = strings.TrimSpace(s[:i])
		for _, f := range strings.Split(s[i+1:], ",") {
			if f = strings.TrimSpace(f); f != "" {
				fields = append(fields, f)
			}
		}
	}

	switch typ {
	case "pie":
		c := &ChartConfig{Type: "pie"}
		if len(fields) >= 2 {
			c.Name, c.Value = fields[0], fields[1]
		} else if len(cols) >= 2 {
			c.Name, c.Value = cols[0], cols[1]
		}
		return c
	case "line", "bar":
		c := &ChartConfig{Type: typ}
		if len(cols) > 0 {
			c.X = cols[0]
		}
		if len(fields) > 0 {
			c.Series = fields
		} else if len(cols) > 1 {
			c.Series = cols[1:]
		}
		return c
	default:
		return autoChart(cols)
	}
}

func mapChart(m map[string]any, cols []string) *ChartConfig {
	c := &ChartConfig{Type: "line"}
	if t, ok := m["type"].(string); ok && t != "" {
		c.Type = t
	}
	if x, ok := m["x"].(string); ok {
		c.X = x
	} else if x, ok := m["xAxis"].(string); ok {
		c.X = x
	} else if len(cols) > 0 {
		c.X = cols[0]
	}
	if s, ok := m["series"].([]any); ok {
		for _, it := range s {
			if str, ok := it.(string); ok {
				c.Series = append(c.Series, str)
			}
		}
	}
	if len(c.Series) == 0 && len(cols) > 1 {
		c.Series = cols[1:]
	}
	return c
}

func autoChart(cols []string) *ChartConfig {
	c := &ChartConfig{Type: "line", Auto: true}
	if len(cols) > 0 {
		c.X = cols[0]
	}
	if len(cols) > 1 {
		c.Series = cols[1:]
	}
	return c
}

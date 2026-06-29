package engine

import (
	"regexp"
	"strings"

	"github.com/spf13/cast"
)

// chartXFieldsRe 匹配 type 后的 [x=列1/列2] 显式 X 轴声明 (捕获中括号内的列清单)。
var chartXFieldsRe = regexp.MustCompile(`\[x=([^\]]+)\]`)

// normalizeChart 把报表中的 @chart 注解规整为前端可消费的 ChartConfig。
//
// 支持的形式:
//   - "__auto__"            : 第一列为 X 轴, 其余列为数值序列
//   - "line:f1,f2"          : 折线图, 指定数值列
//   - "bar:f1,f2"           : 柱状图
//   - "area:f1,f2"          : 面积图 (填充折线)
//   - "scatter:x,y"         : 散点图 (两数值轴)
//   - "pie:category,value"  : 饼图
//   - "funnel:category,value": 漏斗图
//   - "gauge:value"         : 仪表盘 (取数据末行该列)
//   - "radar:f1,f2,..."     : 雷达图 (各列为维度)
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

	// 显式 X 轴 (可多维): type 后紧跟 [x=列1/列2], 如 bar[x=服务/处理方式]:总数,成功数。
	// 多维列用 / 分隔, 前端拼成类目轴标签。抠出后 typ 还原为纯图表类型。
	var xFields []string
	if m := chartXFieldsRe.FindStringSubmatch(typ); m != nil {
		typ = strings.TrimSpace(typ[:strings.Index(typ, "[")])
		for _, f := range strings.Split(m[1], "/") {
			if f = strings.TrimSpace(f); f != "" {
				xFields = append(xFields, f)
			}
		}
	}

	switch typ {
	case "pie", "funnel":
		// 分类 + 数值, 与 pie 同构。
		c := &ChartConfig{Type: typ}
		if len(fields) >= 2 {
			c.Name, c.Value = fields[0], fields[1]
		} else if len(cols) >= 2 {
			c.Name, c.Value = cols[0], cols[1]
		}
		return c
	case "scatter":
		// 两数值轴: x, y。
		c := &ChartConfig{Type: "scatter"}
		switch {
		case len(fields) >= 2:
			c.X, c.Y = fields[0], fields[1]
		case len(cols) >= 2:
			c.X, c.Y = cols[0], cols[1]
		}
		return c
	case "gauge":
		// 单值表盘: 取指定列 (缺省第一数值列), 前端按数据末行渲染。
		c := &ChartConfig{Type: "gauge"}
		if len(fields) > 0 {
			c.Value = fields[0]
		} else if len(cols) > 1 {
			c.Value = cols[1]
		} else if len(cols) > 0 {
			c.Value = cols[0]
		}
		return c
	case "radar":
		// 各数值列为一个雷达维度。
		c := &ChartConfig{Type: "radar"}
		if len(fields) > 0 {
			c.Series = fields
		} else if len(cols) > 1 {
			c.Series = cols[1:]
		}
		return c
	case "line", "bar", "area":
		// 类目轴 + 多数值序列, 三者轴/序列逻辑一致, 仅 type 不同。
		c := &ChartConfig{Type: typ}
		if len(xFields) > 0 {
			// 显式多维 X 轴: 整列都不进 Series, 默认序列为剩余非 X 列。
			c.XFields = xFields
			if len(fields) > 0 {
				c.Series = fields
			} else {
				c.Series = excludeCols(cols, xFields)
			}
		} else {
			if len(cols) > 0 {
				c.X = cols[0]
			}
			if len(fields) > 0 {
				c.Series = fields
			} else if len(cols) > 1 {
				c.Series = cols[1:]
			}
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

	// 分类 + 数值族: pie / funnel。
	if c.Type == "pie" || c.Type == "funnel" {
		if name, ok := m["name"].(string); ok {
			c.Name = name
		} else if name, ok := m["category"].(string); ok {
			c.Name = name
		} else if len(cols) > 0 {
			c.Name = cols[0]
		}
		if value, ok := m["value"].(string); ok {
			c.Value = value
		} else if len(cols) > 1 {
			c.Value = cols[1]
		}
		return c
	}

	// 散点: x, y 双数值轴。
	if c.Type == "scatter" {
		if x, ok := m["x"].(string); ok {
			c.X = x
		} else if len(cols) > 0 {
			c.X = cols[0]
		}
		if y, ok := m["y"].(string); ok {
			c.Y = y
		} else if len(cols) > 1 {
			c.Y = cols[1]
		}
		return c
	}

	// 仪表盘: 单值。
	if c.Type == "gauge" {
		if value, ok := m["value"].(string); ok {
			c.Value = value
		} else if len(cols) > 1 {
			c.Value = cols[1]
		} else if len(cols) > 0 {
			c.Value = cols[0]
		}
		return c
	}

	// 雷达: 仅数值序列, 无 X 轴。
	if c.Type == "radar" {
		switch {
		case m["series"] != nil:
			c.Series = chartStringList(m["series"])
		case m["y"] != nil:
			c.Series = chartStringList(m["y"])
		}
		if len(c.Series) == 0 && len(cols) > 1 {
			c.Series = cols[1:]
		}
		return c
	}

	// 类目轴族: line / bar / area。x 支持字符串 (单维) 或数组 (多维)。
	switch xv := m["x"].(type) {
	case string:
		c.X = xv
	case []any:
		c.XFields = chartStringList(xv)
	}
	if c.X == "" && len(c.XFields) == 0 {
		if x, ok := m["xAxis"].(string); ok {
			c.X = x
		} else if xs, ok := m["xAxis"].([]any); ok {
			c.XFields = chartStringList(xs)
		} else if len(cols) > 0 {
			c.X = cols[0]
		}
	}
	switch {
	case m["series"] != nil:
		c.Series = chartStringList(m["series"])
	case m["y"] != nil:
		c.Series = chartStringList(m["y"])
	case m["yAxis"] != nil:
		c.Series = chartStringList(m["yAxis"])
	}
	if len(c.Series) == 0 {
		if len(c.XFields) > 0 {
			c.Series = excludeCols(cols, c.XFields)
		} else if len(cols) > 1 {
			c.Series = cols[1:]
		}
	}
	c.Stack = cast.ToBool(m["stack"])
	return c
}

// excludeCols 返回 cols 中不在 drop 里的列 (保序), 用于多维 X 轴时推断默认数值序列。
func excludeCols(cols, drop []string) []string {
	skip := make(map[string]bool, len(drop))
	for _, d := range drop {
		skip[d] = true
	}
	var out []string
	for _, c := range cols {
		if !skip[c] {
			out = append(out, c)
		}
	}
	return out
}

func chartStringList(v any) []string {
	if s, ok := v.(string); ok && s != "" {
		return []string{s}
	}
	var out []string
	if s, ok := v.([]any); ok {
		for _, it := range s {
			if str, ok := it.(string); ok {
				out = append(out, str)
			}
		}
	}
	return out
}

// chartXDupNotice 检查类目轴 (单维 X 或多维 XFields 拼接) 是否有重复值。
// 类目轴族 (line/bar/area) 才检查; 重复会让图表与表格行对不上 (反馈 2)。
func chartXDupNotice(c *ChartConfig, rows []map[string]any) string {
	if c == nil || len(rows) == 0 {
		return ""
	}
	switch c.Type {
	case "line", "bar", "area":
	default:
		return ""
	}
	keys := c.XFields
	if len(keys) == 0 && c.X != "" {
		keys = []string{c.X}
	}
	if len(keys) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, cast.ToString(row[k]))
		}
		key := strings.Join(parts, " / ")
		if seen[key] {
			return "图表 X 轴存在重复值, 可能导致展示歧义 (可用 @chart=类型[x=列1/列2] 指定多维 X 轴)"
		}
		seen[key] = true
	}
	return ""
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

package engine

import (
	"encoding/json"

	"github.com/spf13/cast"
)

// seriesConfig 描述把"长表"行转列 (透视) 为"宽表"的配置, 移植自 dataddy plugin_series。
//
//	{"x":"day","series":"channel","value":"amount"}
//
// 把  (day, channel, amount) 多行  ->  每个 day 一行, 各 channel 值成列。
// 兼容 dataddy 的键名: xAxis / series(数组取首) / series_value(数组取首)。
type seriesConfig struct {
	X      string `json:"x"`
	XAxis  string `json:"xAxis"`
	Series any    `json:"series"`       // string 或 []string
	Value  string `json:"value"`        // 本项目键名
	SValue any    `json:"series_value"` // dataddy 键名, string 或 []string
}

// parseSeriesConfig 解析 @series 注解为透视配置。返回 nil 表示未配置/无效。
func parseSeriesConfig(v any) (xField, seriesField, valueField string, ok bool) {
	if v == nil {
		return "", "", "", false
	}

	var cfg seriesConfig
	switch val := v.(type) {
	case map[string]any:
		b, _ := json.Marshal(val)
		_ = json.Unmarshal(b, &cfg)
	case string:
		if json.Unmarshal([]byte(val), &cfg) != nil {
			return "", "", "", false
		}
	default:
		return "", "", "", false
	}

	xField = firstNonEmpty(cfg.X, cfg.XAxis)
	seriesField = firstOf(cfg.Series)
	valueField = firstNonEmpty(cfg.Value, firstOf(cfg.SValue))

	if xField == "" || seriesField == "" || valueField == "" {
		return "", "", "", false
	}
	return xField, seriesField, valueField, true
}

// pivotSeries 把长表行转列。返回新的列顺序 (x + 各 series 名) 与新行。
//   - 保持 x 首次出现顺序作为行序
//   - 保持 series 首次出现顺序作为列序
func pivotSeries(cols []string, rows []map[string]any, xField, seriesField, valueField string) (newCols []string, newRows []map[string]any) {
	// 校验字段存在
	has := map[string]bool{}
	for _, c := range cols {
		has[c] = true
	}
	if !has[xField] || !has[seriesField] || !has[valueField] {
		return cols, rows
	}

	var xOrder []string
	xSeen := map[string]bool{}
	var sOrder []string
	sSeen := map[string]bool{}
	// xValue -> seriesName -> value
	grid := map[string]map[string]any{}

	for _, row := range rows {
		xv := cast.ToString(row[xField])
		sv := cast.ToString(row[seriesField])
		if !xSeen[xv] {
			xSeen[xv] = true
			xOrder = append(xOrder, xv)
		}
		if !sSeen[sv] {
			sSeen[sv] = true
			sOrder = append(sOrder, sv)
		}
		if grid[xv] == nil {
			grid[xv] = map[string]any{}
		}
		grid[xv][sv] = row[valueField]
	}

	newCols = append([]string{xField}, sOrder...)
	for _, xv := range xOrder {
		nr := map[string]any{xField: xv}
		for _, sv := range sOrder {
			if v, ok := grid[xv][sv]; ok {
				nr[sv] = v
			} else {
				nr[sv] = 0 // 缺失补 0, 便于图表
			}
		}
		newRows = append(newRows, nr)
	}
	return newCols, newRows
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// firstOf 取 string 或 []string 的首个非空字符串。
func firstOf(v any) string {
	switch val := v.(type) {
	case string:
		return val
	case []any:
		if len(val) > 0 {
			return cast.ToString(val[0])
		}
	case []string:
		if len(val) > 0 {
			return val[0]
		}
	}
	return ""
}

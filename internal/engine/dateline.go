package engine

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/spf13/cast"
)

// 日期补全 (移植自 dataddy plugin_date_line):
// 给趋势数据补全缺失的日期行, 避免折线断裂。配置:
//
//	-- @date_line={"field":"day","start":"-30 days","format":"Y-m-d"}
//
//   - field:  日期列名 (默认 "day"; 兼容 "date")
//   - start:  起始日期。相对偏移 (如 "-30 days") 基于今天计算; 也可写绝对日期。
//     缺省时取数据中的最早日期。
//   - format: PHP 风格日期格式 (默认按数据首行自动识别, 退化为 Y-m-d)。
//
// 区间为 [start, max(数据最晚日期, start)]; 含 H 的格式按小时步进, 否则按天。
// 补全行仅含日期字段, 其余字段为 nil (前端/图表按缺失处理)。原有行保持不动。

type dateLineConfig struct {
	Field  string `json:"field"`
	Start  string `json:"start"`
	Format string `json:"format"`
}

// parseDateLineConfig 解析 @date_line 注解。返回 ok=false 表示未配置/无效。
func parseDateLineConfig(v any) (cfg dateLineConfig, ok bool) {
	switch val := v.(type) {
	case map[string]any:
		b, _ := json.Marshal(val)
		_ = json.Unmarshal(b, &cfg)
	case string:
		if strings.TrimSpace(val) == "" || json.Unmarshal([]byte(val), &cfg) != nil {
			return cfg, false
		}
	default:
		return cfg, false
	}
	if cfg.Field == "" {
		cfg.Field = "day"
	}
	return cfg, true
}

// applyDateLine 返回补全后的行 (保持原顺序, 缺失日期按时间插入)。
// 字段不存在或无法解析任何日期时原样返回。
func applyDateLine(cfg dateLineConfig, cols []string, rows []map[string]any) []map[string]any {
	field := cfg.Field
	if field == "" || len(rows) == 0 {
		return rows
	}
	if !contains(cols, field) {
		// 兼容: 配置写 day 但实际列名为 date
		if field == "day" && contains(cols, "date") {
			field = "date"
		} else {
			return rows
		}
	}

	// 现有日期 -> 原行 (后出现的覆盖, 与原表一致)
	existing := map[string]map[string]any{}
	var minT, maxT time.Time
	haveBound := false
	format := strings.TrimSpace(cfg.Format)

	for _, row := range rows {
		s := cast.ToString(row[field])
		t, ok := parseDateValue(s)
		if !ok {
			continue
		}
		if format == "" {
			format = inferDateFormat(s)
		}
		existing[s] = row
		if !haveBound || t.Before(minT) {
			minT = t
		}
		if !haveBound || t.After(maxT) {
			maxT = t
		}
		haveBound = true
	}
	if !haveBound {
		return rows
	}
	if format == "" {
		format = "Y-m-d"
	}

	// 起始日期: 相对偏移基于今天, 绝对日期直接解析, 否则用数据最早日期。
	startT := minT
	if s := strings.TrimSpace(cfg.Start); s != "" {
		if isDateModifier(s) {
			startT = applyDateModifier(nowFunc().In(time.Local), s)
		} else if t, ok := parseDateValue(s); ok {
			startT = t
		}
	}
	endT := maxT
	if endT.Before(startT) {
		endT = startT
	}

	byHour := strings.Contains(format, "H")
	var out []map[string]any
	cur := truncateTo(startT, byHour)
	end := truncateTo(endT, byHour)
	guard := 0
	for !cur.After(end) {
		key := formatDate(cur, format)
		if row, ok := existing[key]; ok {
			out = append(out, row)
		} else {
			out = append(out, map[string]any{field: key})
		}
		if byHour {
			cur = cur.Add(time.Hour)
		} else {
			cur = cur.AddDate(0, 0, 1)
		}
		if guard++; guard > 100000 { // 防御: 区间异常时止损
			break
		}
	}
	return out
}

// truncateTo 截断到天 (或小时) 边界, 保证步进对齐。
func truncateTo(t time.Time, byHour bool) time.Time {
	if byHour {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// inferDateFormat 依据样例串猜测 PHP 风格格式 (仅区分是否含时分)。
func inferDateFormat(s string) string {
	s = strings.TrimSpace(s)
	switch {
	case len(s) <= 7: // 2026-06
		return "Y-m"
	case len(s) <= 10: // 2026-06-25
		return "Y-m-d"
	default: // 含时间
		return "Y-m-d H:i:s"
	}
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

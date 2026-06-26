package engine

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/spf13/cast"
)

// 结果后置过滤 (移植自 dataddy plugin_filter):
// 在 SQL 执行后对 rows 做行级过滤, 免改 SQL。配置为条件二维数组, 多条件 AND:
//
//	-- @filter=[["status","=","active"],["age",">=","18"]]
//
// 每条 [field, op, value]; op ∈ = != > >= < <= in "not in" between。
// in/"not in"/between 的 value 为逗号分隔列表 (between 取前两项, 开区间)。
// 缺失字段按空值参与比较。

type filterCond struct {
	field string
	op    string
	value string
}

// parseFilterConfig 解析 @filter 注解为条件列表。返回 nil 表示未配置/无效。
func parseFilterConfig(v any) []filterCond {
	raw := normalizeFilterRaw(v)
	if raw == nil {
		return nil
	}
	var conds []filterCond
	for _, item := range raw {
		if len(item) < 3 {
			continue
		}
		field := strings.TrimSpace(cast.ToString(item[0]))
		op := strings.ToLower(strings.TrimSpace(cast.ToString(item[1])))
		if field == "" || op == "" {
			continue
		}
		conds = append(conds, filterCond{field: field, op: op, value: cast.ToString(item[2])})
	}
	if len(conds) == 0 {
		return nil
	}
	return conds
}

// normalizeFilterRaw 把注解统一为 [][]any。注解经 decodeAnnotation 后数组为 []any。
func normalizeFilterRaw(v any) [][]any {
	switch val := v.(type) {
	case []any:
		out := make([][]any, 0, len(val))
		for _, item := range val {
			if row, ok := item.([]any); ok {
				out = append(out, row)
			}
		}
		return out
	case string:
		var raw [][]any
		if json.Unmarshal([]byte(val), &raw) == nil {
			return raw
		}
	}
	return nil
}

// applyResultFilter 就地过滤 rows, 仅保留满足所有条件的行。
func applyResultFilter(conds []filterCond, rows []map[string]any) []map[string]any {
	if len(conds) == 0 {
		return rows
	}
	out := rows[:0]
	for _, row := range rows {
		keep := true
		for _, c := range conds {
			if !c.match(row[c.field]) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, row)
		}
	}
	return out
}

// match 判定单元格值是否满足条件。数值可比时按数值, 否则按字符串。
func (c filterCond) match(cell any) bool {
	left := cast.ToString(cell)
	switch c.op {
	case "=", "is", "==":
		return left == c.value
	case "!=", "<>":
		return left != c.value
	case "in":
		return inList(left, c.value)
	case "not in":
		return !inList(left, c.value)
	case ">", ">=", "<", "<=":
		return compareNum(cell, c.value, c.op)
	case "between":
		return between(cell, c.value)
	}
	return true // 未知操作符不过滤
}

func inList(v, list string) bool {
	for _, item := range strings.Split(list, ",") {
		if strings.TrimSpace(item) == v {
			return true
		}
	}
	return false
}

// compareNum 数值比较; 任一侧非数值则回退字符串比较。
func compareNum(cell any, value, op string) bool {
	lf, lErr := toFloat(cell)
	rf, rErr := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if lErr == nil && rErr == nil {
		switch op {
		case ">":
			return lf > rf
		case ">=":
			return lf >= rf
		case "<":
			return lf < rf
		case "<=":
			return lf <= rf
		}
		return false
	}
	left := cast.ToString(cell)
	switch op {
	case ">":
		return left > value
	case ">=":
		return left >= value
	case "<":
		return left < value
	case "<=":
		return left <= value
	}
	return false
}

// between 开区间 (lo, hi); value 为 "lo,hi"。数值优先, 否则字符串。
func between(cell any, value string) bool {
	parts := strings.SplitN(value, ",", 2)
	if len(parts) != 2 {
		return false
	}
	lo, hi := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	lf, lErr := toFloat(cell)
	loF, loErr := strconv.ParseFloat(lo, 64)
	hiF, hiErr := strconv.ParseFloat(hi, 64)
	if lErr == nil && loErr == nil && hiErr == nil {
		return lf > loF && lf < hiF
	}
	s := cast.ToString(cell)
	return s > lo && s < hi
}

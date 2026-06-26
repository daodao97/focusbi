package subscription

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"xproxy/dao"
	"xproxy/internal/engine"

	"github.com/spf13/cast"
)

// 阈值告警条件判定: 对目标区块某列按聚合方式取值, 与阈值比较, 命中才推送。
// 条件来自 dao.SubCondition (列 + 聚合 + 操作符 + 值)。

// evalCondition 判定条件是否命中。返回 (是否命中, 实际值的人类可读描述)。
// 条件为 nil 视为无条件 (命中, 描述为空) —— 即定时推送。
func evalCondition(cond *dao.SubCondition, r *engine.Result) (bool, string) {
	if cond == nil || strings.TrimSpace(cond.Column) == "" {
		return true, ""
	}
	blk := pickBlock(r, cond.Block)
	if blk == nil {
		return false, "未找到目标区块"
	}

	agg := strings.ToLower(strings.TrimSpace(cond.Agg))
	if agg == "" {
		agg = "any"
	}

	// any: 任一行命中即触发
	if agg == "any" {
		for _, row := range blk.Rows {
			if compareCell(row[cond.Column], cond.Op, cond.Value) {
				return true, fmt.Sprintf("%s=%s %s %s", cond.Column,
					cast.ToString(row[cond.Column]), cond.Op, cond.Value)
			}
		}
		return false, fmt.Sprintf("无任一行 %s %s %s", cond.Column, cond.Op, cond.Value)
	}

	// first/sum/max/min/count: 先聚合出一个数, 再比较
	got, ok := aggregate(blk, cond.Column, agg)
	if !ok {
		return false, fmt.Sprintf("列 %s 无可用数值", cond.Column)
	}
	gotStr := trimNum(got)
	hit := compareNumeric(got, cond.Op, cond.Value)
	return hit, fmt.Sprintf("%s(%s)=%s %s %s", agg, cond.Column, gotStr, cond.Op, cond.Value)
}

// pickBlock 选目标区块: 指定 ID 则按 ID 找; 否则取首个 table 区块。
func pickBlock(r *engine.Result, id string) *engine.Block {
	id = strings.TrimSpace(id)
	for i := range r.Blocks {
		b := &r.Blocks[i]
		if id != "" {
			if b.ID == id {
				return b
			}
			continue
		}
		if b.Type == "table" {
			return b
		}
	}
	return nil
}

// aggregate 对某列做 first/sum/max/min/count 聚合, 返回结果与是否有有效数值。
func aggregate(blk *engine.Block, col, agg string) (float64, bool) {
	if agg == "count" {
		return float64(len(blk.Rows)), true
	}
	var sum, max, min float64
	n := 0
	for _, row := range blk.Rows {
		v, ok := row[col]
		if !ok || v == nil {
			continue
		}
		f, err := toNum(v)
		if err != nil {
			continue
		}
		if agg == "first" {
			return f, true
		}
		if n == 0 {
			max, min = f, f
		} else {
			max = math.Max(max, f)
			min = math.Min(min, f)
		}
		sum += f
		n++
	}
	if n == 0 {
		return 0, false
	}
	switch agg {
	case "sum":
		return sum, true
	case "max":
		return max, true
	case "min":
		return min, true
	}
	return 0, false
}

// compareCell 用操作符比较单元格值与阈值 (数值优先, 退化字符串)。
func compareCell(cell any, op, value string) bool {
	lf, lErr := toNum(cell)
	rf, rErr := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if lErr == nil && rErr == nil {
		return cmpNum(lf, rf, op)
	}
	return cmpStr(cast.ToString(cell), value, op)
}

// compareNumeric 比较一个已聚合的数值与阈值。
func compareNumeric(left float64, op, value string) bool {
	rf, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return false
	}
	return cmpNum(left, rf, op)
}

func cmpNum(l, r float64, op string) bool {
	switch op {
	case "=", "==":
		return l == r
	case "!=", "<>":
		return l != r
	case ">":
		return l > r
	case ">=":
		return l >= r
	case "<":
		return l < r
	case "<=":
		return l <= r
	}
	return false
}

func cmpStr(l, r, op string) bool {
	switch op {
	case "=", "==":
		return l == r
	case "!=", "<>":
		return l != r
	case ">":
		return l > r
	case ">=":
		return l >= r
	case "<":
		return l < r
	case "<=":
		return l <= r
	}
	return false
}

// toNum 把单元格值转 float64 (字符串里可能带 % / 千分位, 做基本清理)。
func toNum(v any) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case float32:
		return float64(val), nil
	case int, int64, int32:
		return cast.ToFloat64(val), nil
	}
	s := strings.TrimSpace(cast.ToString(v))
	s = strings.TrimSuffix(s, "%")
	s = strings.ReplaceAll(s, ",", "")
	return strconv.ParseFloat(s, 64)
}

func trimNum(f float64) string {
	if f == math.Trunc(f) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', 2, 64)
}

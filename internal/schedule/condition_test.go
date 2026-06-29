package schedule

import (
	"testing"

	"xproxy/dao"
	"xproxy/internal/engine"
)

func sampleResult() *engine.Result {
	return &engine.Result{
		Blocks: []engine.Block{
			{
				ID: "block_1", Type: "table", Title: "渠道汇总",
				Columns: []engine.Column{{Name: "ch", Header: "渠道"}, {Name: "gmv", Header: "GMV"}},
				Rows: []map[string]any{
					{"ch": "web", "gmv": 8000},
					{"ch": "app", "gmv": 12000},
				},
			},
		},
	}
}

func TestEvalConditionNil(t *testing.T) {
	hit, _ := evalCondition(nil, sampleResult())
	if !hit {
		t.Error("nil 条件应视为命中 (定时推送)")
	}
}

func TestEvalConditionAny(t *testing.T) {
	// 任一行 gmv < 10000 → 命中 (web=8000)
	c := &dao.ScheduleCondition{Column: "gmv", Agg: "any", Op: "<", Value: "10000"}
	hit, detail := evalCondition(c, sampleResult())
	if !hit {
		t.Fatalf("应命中, detail=%s", detail)
	}
}

func TestEvalConditionAnyNoHit(t *testing.T) {
	// 任一行 gmv < 5000 → 不命中
	c := &dao.ScheduleCondition{Column: "gmv", Agg: "any", Op: "<", Value: "5000"}
	hit, _ := evalCondition(c, sampleResult())
	if hit {
		t.Error("不应命中")
	}
}

func TestEvalConditionSum(t *testing.T) {
	// sum(gmv)=20000 >= 15000 → 命中
	c := &dao.ScheduleCondition{Column: "gmv", Agg: "sum", Op: ">=", Value: "15000"}
	hit, detail := evalCondition(c, sampleResult())
	if !hit {
		t.Fatalf("sum 应命中, detail=%s", detail)
	}
}

func TestEvalConditionMaxMinFirstCount(t *testing.T) {
	r := sampleResult()
	cases := []struct {
		agg, op, val string
		want         bool
	}{
		{"max", ">", "11000", true},  // max=12000
		{"min", "<", "9000", true},   // min=8000
		{"first", "=", "8000", true}, // first=8000
		{"count", "=", "2", true},    // 2 行
		{"count", ">", "5", false},
	}
	for _, c := range cases {
		cond := &dao.ScheduleCondition{Column: "gmv", Agg: c.agg, Op: c.op, Value: c.val}
		hit, detail := evalCondition(cond, r)
		if hit != c.want {
			t.Errorf("%s %s %s = %v, want %v (%s)", c.agg, c.op, c.val, hit, c.want, detail)
		}
	}
}

func TestEvalConditionMissingBlock(t *testing.T) {
	c := &dao.ScheduleCondition{Block: "nope", Column: "gmv", Agg: "any", Op: "<", Value: "1"}
	hit, _ := evalCondition(c, sampleResult())
	if hit {
		t.Error("区块不存在不应命中")
	}
}

func TestToNumCleansFormat(t *testing.T) {
	for _, in := range []any{"1,234", "56%", 78, 9.5} {
		if _, err := toNum(in); err != nil {
			t.Errorf("toNum(%v) 失败: %v", in, err)
		}
	}
}

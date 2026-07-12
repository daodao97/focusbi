package engine

import (
	"context"
	"testing"
)

func TestExplainContextSQLite(t *testing.T) {
	setupSQLiteDefault(t)
	content := `${day|日期|2026-06-24|date;required}
#!MARKDOWN
# ignored
#!END
-- @id=pv
SELECT day, pv FROM pv WHERE day = '{day}';`
	plans, err := NewRunner("default").ExplainContext(context.Background(), content, nil)
	if err != nil {
		t.Fatalf("ExplainContext: %v", err)
	}
	if len(plans) != 1 || plans[0].BlockID != "pv" || len(plans[0].Columns) == 0 || len(plans[0].Rows) == 0 || plans[0].Error != "" {
		t.Fatalf("plans=%+v", plans)
	}
}

func TestExplainContextRejectsInvalidParams(t *testing.T) {
	content := `${age|年龄||number;required,min=1}
SELECT {age};`
	if _, err := NewRunner("default").ExplainContext(context.Background(), content, map[string]string{"age": "0"}); err == nil {
		t.Fatal("EXPLAIN 应执行参数约束校验")
	}
}

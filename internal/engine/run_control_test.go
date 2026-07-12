package engine

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"xproxy/internal/datasource"
)

func TestRunMergesIdenticalQueries(t *testing.T) {
	original := executeQueryContext
	defer func() { executeQueryContext = original }()
	var calls atomic.Int32
	executeQueryContext = func(context.Context, string, string, ...any) (*datasource.QueryResult, error) {
		calls.Add(1)
		time.Sleep(20 * time.Millisecond) // 让两个并发区块稳定重叠
		return &datasource.QueryResult{Columns: []string{"n"}, Rows: []map[string]any{{"n": 1}}}, nil
	}
	res, err := NewRunner("default").Run("SELECT 1 AS n;\nSELECT 1 AS n;", nil)
	if err != nil || len(res.Blocks) != 2 {
		t.Fatalf("Run: blocks=%d err=%v", len(res.Blocks), err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("相同查询执行次数=%d, want 1", got)
	}
}

func TestRunContextCancelsQuery(t *testing.T) {
	original := executeQueryContext
	defer func() { executeQueryContext = original }()
	executeQueryContext = func(ctx context.Context, _, _ string, _ ...any) (*datasource.QueryResult, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := NewRunner("default").RunContext(ctx, "SELECT 1", nil)
	if err != nil {
		t.Fatalf("区块错误应保持 in-band, got %v", err)
	}
	if len(res.Blocks) != 1 || res.Blocks[0].Error == "" {
		t.Fatalf("取消后应返回错误区块: %+v", res.Blocks)
	}
}

func TestDefaultLimitReportsTruncation(t *testing.T) {
	original := executeQueryContext
	defer func() { executeQueryContext = original }()
	executeQueryContext = func(context.Context, string, string, ...any) (*datasource.QueryResult, error) {
		rows := make([]map[string]any, 1001)
		for i := range rows {
			rows[i] = map[string]any{"n": i}
		}
		return &datasource.QueryResult{Columns: []string{"n"}, Rows: rows}, nil
	}
	res, err := NewRunner("default").Run("SELECT n FROM numbers", nil)
	if err != nil || len(res.Blocks) != 1 {
		t.Fatalf("Run: %+v err=%v", res, err)
	}
	b := res.Blocks[0]
	if !b.Truncated || b.RowLimit != 1000 || len(b.Rows) != 1000 {
		t.Fatalf("默认限制截断信息错误: truncated=%v limit=%d rows=%d", b.Truncated, b.RowLimit, len(b.Rows))
	}
}

func TestSerialRunMergesKnownDuplicateLargeQueries(t *testing.T) {
	setupSQLiteConcurrency(t)
	original := executeQueryContext
	defer func() { executeQueryContext = original }()
	var calls atomic.Int32
	executeQueryContext = func(context.Context, string, string, ...any) (*datasource.QueryResult, error) {
		calls.Add(1)
		rows := make([]map[string]any, maxRunMemoRows+1)
		return &datasource.QueryResult{Columns: []string{"n"}, Rows: rows}, nil
	}
	res, err := runWithQueryConcurrency(t, 1, "SELECT n FROM big;\nSELECT n FROM big; ")
	if err != nil || len(res.Blocks) != 2 {
		t.Fatalf("Run: blocks=%d err=%v", len(res.Blocks), err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("串行重复大查询执行次数=%d, want 1", got)
	}
}

func TestScriptMergesSequentialDuplicateLargeQueries(t *testing.T) {
	original := executeQueryContext
	defer func() { executeQueryContext = original }()
	var calls atomic.Int32
	executeQueryContext = func(context.Context, string, string, ...any) (*datasource.QueryResult, error) {
		calls.Add(1)
		rows := make([]map[string]any, maxRunMemoRows+1)
		return &datasource.QueryResult{Columns: []string{"n"}, Rows: rows}, nil
	}
	content := `#!SCRIPT
query('SELECT n FROM big')
query('SELECT n FROM big')
dump('done')
#!END`
	res, err := NewRunner("default").Run(content, nil)
	if err != nil || len(res.Blocks) != 1 || res.Blocks[0].Error != "" {
		t.Fatalf("Run: blocks=%+v err=%v", res.Blocks, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("脚本顺序重复大查询执行次数=%d, want 1", got)
	}
}

func TestDifferentCachePoliciesDoNotShareRunMemo(t *testing.T) {
	original := executeQueryContext
	defer func() { executeQueryContext = original }()
	var calls atomic.Int32
	executeQueryContext = func(context.Context, string, string, ...any) (*datasource.QueryResult, error) {
		calls.Add(1)
		return &datasource.QueryResult{Columns: []string{"n"}, Rows: []map[string]any{{"n": 1}}}, nil
	}
	content := "SELECT 1 AS n;\n-- @sql_cache=60\nSELECT 1 AS n;"
	res, err := NewRunner("default").Run(content, nil)
	if err != nil || len(res.Blocks) != 2 {
		t.Fatalf("Run: blocks=%+v err=%v", res.Blocks, err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("不同缓存策略不能共享请求内结果, 执行次数=%d, want 2", got)
	}
}

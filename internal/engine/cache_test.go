package engine

import (
	"testing"
	"time"

	"xproxy/internal/datasource"
)

func resetQueryCacheForTest() {
	queryCacheMu.Lock()
	defer queryCacheMu.Unlock()
	queryCache = map[string]cacheEntry{}
}

func TestCacheKeyDistinct(t *testing.T) {
	if cacheKey("a", "SELECT 1") == cacheKey("b", "SELECT 1") {
		t.Error("不同 dsn 应得不同键")
	}
	if cacheKey("a", "SELECT 1") == cacheKey("a", "SELECT 2") {
		t.Error("不同 sql 应得不同键")
	}
	if cacheKey("a", "SELECT ?", 1) == cacheKey("a", "SELECT ?", 2) {
		t.Error("不同 args 应得不同键")
	}
	k1, k2 := cacheKey("a", "SELECT ?", 1), cacheKey("a", "SELECT ?", 1)
	if k1 != k2 {
		t.Error("相同输入应得相同键")
	}
}

func TestPruneExpired(t *testing.T) {
	queryCacheMu.Lock()
	queryCache = map[string]cacheEntry{}
	base := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	queryCache["live"] = cacheEntry{expires: base.Add(time.Hour)}
	queryCache["dead"] = cacheEntry{expires: base.Add(-time.Hour)}
	pruneExpiredLocked(base)
	_, liveOK := queryCache["live"]
	_, deadOK := queryCache["dead"]
	queryCache = map[string]cacheEntry{}
	queryCacheMu.Unlock()

	if !liveOK {
		t.Error("未过期项不应被清理")
	}
	if deadOK {
		t.Error("过期项应被清理")
	}
}

func TestCacheCapacityLimits(t *testing.T) {
	resetQueryCacheForTest()
	setupSQLiteDefault(t)

	// 条目数上限: 满员后新键不入缓存, 已有键仍可更新。
	origEntries, origRows := maxCacheEntries, maxCacheRows
	maxCacheEntries, maxCacheRows = 1, 1
	defer func() { maxCacheEntries, maxCacheRows = origEntries, origRows }()

	if _, err := cachedQuery("default", `SELECT day FROM pv WHERE day = '2026-06-24'`, 60, false); err != nil {
		t.Fatalf("query1: %v", err)
	}
	if _, err := cachedQuery("default", `SELECT day FROM pv WHERE day = '2026-06-23'`, 60, false); err != nil {
		t.Fatalf("query2: %v", err)
	}
	queryCacheMu.Lock()
	n := len(queryCache)
	queryCacheMu.Unlock()
	if n != 1 {
		t.Errorf("缓存条目 = %d, 满员后应保持 1", n)
	}

	// 行数上限: 结果行数超限不缓存。
	resetQueryCacheForTest()
	if _, err := cachedQuery("default", `SELECT day FROM pv`, 60, false); err != nil {
		t.Fatalf("query big: %v", err)
	}
	queryCacheMu.Lock()
	n = len(queryCache)
	queryCacheMu.Unlock()
	if n != 0 {
		t.Errorf("超行数上限的结果不应入缓存, 实际条目 = %d", n)
	}
}

func TestRunSQLCacheUsesCachedResult(t *testing.T) {
	resetQueryCacheForTest()
	setupSQLiteDefault(t)

	content := `
-- @id=pv_cached
-- @sql_cache=60
SELECT day, pv FROM pv WHERE day = '2026-06-24';
`
	r := NewRunner("default")
	res, err := r.Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	first, _ := toFloat(res.Blocks[0].Rows[0]["pv"])
	if first != 300 {
		t.Fatalf("first pv = %v, want 300", res.Blocks[0].Rows[0]["pv"])
	}

	if _, err := datasource.Query("default", `UPDATE pv SET pv = 999 WHERE day = '2026-06-24'`); err != nil {
		t.Fatalf("update pv: %v", err)
	}

	res, err = r.Run(content, nil)
	if err != nil {
		t.Fatalf("Run cached: %v", err)
	}
	cached, _ := toFloat(res.Blocks[0].Rows[0]["pv"])
	if cached != 300 {
		t.Fatalf("cached pv = %v, want cached 300", res.Blocks[0].Rows[0]["pv"])
	}
}

func TestRunSQLCacheBypass(t *testing.T) {
	resetQueryCacheForTest()
	setupSQLiteDefault(t)

	content := `
-- @id=pv_cached
-- @sql_cache=60
SELECT day, pv FROM pv WHERE day = '2026-06-24';
`
	r := NewRunner("default")
	if _, err := r.Run(content, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := datasource.Query("default", `UPDATE pv SET pv = 888 WHERE day = '2026-06-24'`); err != nil {
		t.Fatalf("update pv: %v", err)
	}

	res, err := r.WithNoCache(true).Run(content, nil)
	if err != nil {
		t.Fatalf("Run no-cache: %v", err)
	}
	got, _ := toFloat(res.Blocks[0].Rows[0]["pv"])
	if got != 888 {
		t.Fatalf("no-cache pv = %v, want 888", res.Blocks[0].Rows[0]["pv"])
	}
}

func TestRunScriptQueryCacheUsesCachedResult(t *testing.T) {
	resetQueryCacheForTest()
	setupSQLiteDefault(t)

	content := `
#!SCRIPT
const rows = query(
  'SELECT day, pv FROM pv WHERE day = ?',
  ['2026-06-24'],
  {sql_cache: 60}
)
result.table({id:'script_cached', rows})
#!END
`
	r := NewRunner("default")
	if _, err := r.Run(content, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := datasource.Query("default", `UPDATE pv SET pv = 777 WHERE day = '2026-06-24'`); err != nil {
		t.Fatalf("update pv: %v", err)
	}

	res, err := r.Run(content, nil)
	if err != nil {
		t.Fatalf("Run cached: %v", err)
	}
	got, _ := toFloat(res.Blocks[0].Rows[0]["pv"])
	if got != 300 {
		t.Fatalf("script cached pv = %v, want cached 300", res.Blocks[0].Rows[0]["pv"])
	}
}

func TestRunScriptQueryCacheBypass(t *testing.T) {
	resetQueryCacheForTest()
	setupSQLiteDefault(t)

	content := `
#!SCRIPT
const rows = query(
  'SELECT day, pv FROM pv WHERE day = ?',
  ['2026-06-24'],
  {sql_cache: 60}
)
result.table({id:'script_cached', rows})
#!END
`
	r := NewRunner("default")
	if _, err := r.Run(content, nil); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := datasource.Query("default", `UPDATE pv SET pv = 666 WHERE day = '2026-06-24'`); err != nil {
		t.Fatalf("update pv: %v", err)
	}

	res, err := r.WithNoCache(true).Run(content, nil)
	if err != nil {
		t.Fatalf("Run no-cache: %v", err)
	}
	got, _ := toFloat(res.Blocks[0].Rows[0]["pv"])
	if got != 666 {
		t.Fatalf("script no-cache pv = %v, want 666", res.Blocks[0].Rows[0]["pv"])
	}
}

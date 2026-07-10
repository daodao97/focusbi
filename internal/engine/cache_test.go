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

	// 刷新应回写缓存: 再改库后普通运行, 命中的应是刷新时写入的 888 而非最新值。
	if _, err := datasource.Query("default", `UPDATE pv SET pv = 555 WHERE day = '2026-06-24'`); err != nil {
		t.Fatalf("update pv: %v", err)
	}
	res, err = r.Run(content, nil)
	if err != nil {
		t.Fatalf("Run after refresh: %v", err)
	}
	got, _ = toFloat(res.Blocks[0].Rows[0]["pv"])
	if got != 888 {
		t.Fatalf("刷新后缓存 pv = %v, want 888 (刷新应回写缓存)", res.Blocks[0].Rows[0]["pv"])
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

func TestScriptCacheSetGet(t *testing.T) {
	scriptCacheMu.Lock()
	scriptCache = map[string]scriptCacheEntry{}
	scriptCacheMu.Unlock()

	code := `
if (cache.get('k') === undefined) {
  cache.set('k', {n: 42, list: [1, 2]}, 60)
}
const v = cache.get('k')
result.table({rows: [{n: v.n, len: v.list.length}]})
`
	blocks, _, _ := runScript(code, scriptContext{cacheScope: "report:1"})
	if len(blocks) != 1 || blocks[0].Error != "" {
		t.Fatalf("blocks = %+v", blocks)
	}
	n, _ := toFloat(blocks[0].Rows[0]["n"])
	if n != 42 {
		t.Errorf("n = %v, want 42", blocks[0].Rows[0]["n"])
	}

	// 第二次运行命中缓存 (值不同也应返回旧值)
	code2 := `
const v = cache.get('k')
result.table({rows: [{hit: v !== undefined && v.n === 42}]})
`
	blocks, _, _ = runScript(code2, scriptContext{cacheScope: "report:1"})
	if hit, _ := blocks[0].Rows[0]["hit"].(bool); !hit {
		t.Errorf("第二次运行应命中缓存, rows = %v", blocks[0].Rows)
	}
	blocks, _, _ = runScript(code2, scriptContext{cacheScope: "report:2"})
	if hit, _ := blocks[0].Rows[0]["hit"].(bool); hit {
		t.Error("不同报表作用域不应共享脚本缓存")
	}

	// noCache 时 get 强制未命中
	blocks, _, _ = runScript(code2, scriptContext{noCache: true, cacheScope: "report:1"})
	if hit, _ := blocks[0].Rows[0]["hit"].(bool); hit {
		t.Error("noCache 时 cache.get 应未命中")
	}

	// ttl<=0 不缓存; 不可序列化的值不缓存且不报错
	code3 := `
cache.set('zero', 1, 0)
cache.set('fn', function(){}, 60)
result.table({rows: [{a: cache.get('zero') === undefined, b: cache.get('fn') === undefined}]})
`
	blocks, _, _ = runScript(code3, scriptContext{cacheScope: "report:1"})
	if blocks[0].Error != "" {
		t.Fatalf("err = %s", blocks[0].Error)
	}
	if a, _ := blocks[0].Rows[0]["a"].(bool); !a {
		t.Error("ttl=0 不应缓存")
	}
	if b, _ := blocks[0].Rows[0]["b"].(bool); !b {
		t.Error("函数值不应缓存")
	}
}

func TestInvalidateQueryCacheByDSN(t *testing.T) {
	queryCacheMu.Lock()
	queryCache = map[string]cacheEntry{
		"a": {dsn: "one", expires: time.Now().Add(time.Hour)},
		"b": {dsn: "two", expires: time.Now().Add(time.Hour)},
	}
	queryCacheMu.Unlock()
	InvalidateQueryCache("one")
	queryCacheMu.Lock()
	defer queryCacheMu.Unlock()
	if _, ok := queryCache["a"]; ok {
		t.Fatal("目标数据源缓存未清理")
	}
	if _, ok := queryCache["b"]; !ok {
		t.Fatal("其它数据源缓存不应被清理")
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

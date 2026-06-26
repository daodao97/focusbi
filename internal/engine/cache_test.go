package engine

import (
	"testing"
	"time"
)

func TestCacheKeyDistinct(t *testing.T) {
	if cacheKey("a", "SELECT 1") == cacheKey("b", "SELECT 1") {
		t.Error("不同 dsn 应得不同键")
	}
	if cacheKey("a", "SELECT 1") == cacheKey("a", "SELECT 2") {
		t.Error("不同 sql 应得不同键")
	}
	k1, k2 := cacheKey("a", "SELECT 1"), cacheKey("a", "SELECT 1")
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

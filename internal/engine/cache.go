package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	"xproxy/internal/datasource"
)

// 查询结果缓存 (移植自 dataddy sql_cache):
// 以 dsn + SQL 为键缓存 datasource.Query 的结果, 在 TTL 内重复执行直接命中,
// 降低数据库压力、加速重查询。仅缓存成功结果; 出错不缓存。

// nowFunc 便于测试覆写时间源。
var nowFunc = time.Now

type cacheEntry struct {
	res     *datasource.QueryResult
	expires time.Time
}

var (
	queryCacheMu sync.Mutex
	queryCache   = map[string]cacheEntry{}
)

// cacheKey 生成 dsn+sql 的稳定键。
func cacheKey(dsn, sql string) string {
	h := sha256.Sum256([]byte(dsn + "\x00" + sql))
	return hex.EncodeToString(h[:])
}

// cachedQuery 在 ttl>0 时走缓存执行查询; ttl<=0 或 bypass=true 时直连。
// 命中缓存返回结果的浅拷贝列与共享行 (调用方 buildColumns 会另建 Column, 行只读展示)。
func cachedQuery(dsn, sql string, ttlSec int, bypass bool) (*datasource.QueryResult, error) {
	if ttlSec <= 0 || bypass {
		return datasource.Query(dsn, sql)
	}
	key := cacheKey(dsn, sql)
	now := nowFunc()

	queryCacheMu.Lock()
	if e, ok := queryCache[key]; ok && now.Before(e.expires) {
		queryCacheMu.Unlock()
		return e.res, nil
	}
	queryCacheMu.Unlock()

	res, err := datasource.Query(dsn, sql)
	if err != nil {
		return nil, err
	}

	queryCacheMu.Lock()
	queryCache[key] = cacheEntry{res: res, expires: now.Add(time.Duration(ttlSec) * time.Second)}
	pruneExpiredLocked(now)
	queryCacheMu.Unlock()
	return res, nil
}

// pruneExpiredLocked 顺带清理过期项, 防止键无限增长。调用方须持锁。
func pruneExpiredLocked(now time.Time) {
	for k, e := range queryCache {
		if now.After(e.expires) {
			delete(queryCache, k)
		}
	}
}

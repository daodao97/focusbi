package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// 容量上限: 防止长 TTL + 大结果集把内存稳定占满。超限的结果直接不缓存 (每次查库),
// 缓存满时不淘汰未过期项 (新键不缓存)。var 便于测试覆写。
// ponytail: 简单硬上限, 命中率不够再上 LRU。
var (
	maxCacheEntries = 500  // 缓存条目数上限
	maxCacheRows    = 5000 // 单结果可缓存的行数上限
)

var (
	queryCacheMu sync.Mutex
	queryCache   = map[string]cacheEntry{}
)

// cacheKey 生成 dsn+sql+args 的稳定键。
func cacheKey(dsn, sql string, args ...any) string {
	argBytes, err := json.Marshal(args)
	if err != nil {
		argBytes = []byte(fmt.Sprint(args...))
	}
	h := sha256.Sum256([]byte(dsn + "\x00" + sql + "\x00" + string(argBytes)))
	return hex.EncodeToString(h[:])
}

// cachedQuery 在 ttl>0 时走缓存执行查询; ttl<=0 或 bypass=true 时直连。
// 命中缓存返回结果副本, 避免后续列格式化/排序/透视等处理污染缓存。
func cachedQuery(dsn, sql string, ttlSec int, bypass bool, args ...any) (*datasource.QueryResult, error) {
	if ttlSec <= 0 || bypass {
		return datasource.Query(dsn, sql, args...)
	}
	key := cacheKey(dsn, sql, args...)
	now := nowFunc()

	queryCacheMu.Lock()
	if e, ok := queryCache[key]; ok && now.Before(e.expires) {
		queryCacheMu.Unlock()
		return cloneQueryResult(e.res), nil
	}
	queryCacheMu.Unlock()

	res, err := datasource.Query(dsn, sql, args...)
	if err != nil {
		return nil, err
	}
	if len(res.Rows) > maxCacheRows {
		return res, nil // 结果过大不缓存
	}

	queryCacheMu.Lock()
	pruneExpiredLocked(now)
	if _, exists := queryCache[key]; exists || len(queryCache) < maxCacheEntries {
		queryCache[key] = cacheEntry{res: cloneQueryResult(res), expires: now.Add(time.Duration(ttlSec) * time.Second)}
	}
	queryCacheMu.Unlock()
	return res, nil
}

// 脚本自定义缓存 (脚本 API cache.get/set): 进程内 KV, 值 JSON 序列化存储 (天然深拷贝,
// 防止多次运行共享可变对象)。键为脚本自定义字符串, **全局命名空间** (跨报表共享, 也可能冲突,
// 建议脚本自带前缀如 'sales:xxx')。
// ponytail: 与 queryCache 同样的硬上限策略, 不够再统一抽象。
var (
	maxScriptCacheEntries = 500     // 条目数上限
	maxScriptCacheBytes   = 1 << 20 // 单值序列化后 1MB 上限
)

type scriptCacheEntry struct {
	data    []byte // JSON
	expires time.Time
}

var (
	scriptCacheMu sync.Mutex
	scriptCache   = map[string]scriptCacheEntry{}
)

// scriptCacheGet 返回反序列化后的值; 未命中/已过期返回 (nil, false)。
func scriptCacheGet(key string) (any, bool) {
	now := nowFunc()
	scriptCacheMu.Lock()
	e, ok := scriptCache[key]
	scriptCacheMu.Unlock()
	if !ok || now.After(e.expires) {
		return nil, false
	}
	var v any
	if err := json.Unmarshal(e.data, &v); err != nil {
		return nil, false
	}
	return v, true
}

// scriptCacheSet 序列化后写入; ttl<=0、序列化失败或超限时静默不缓存 (缓存本就是尽力而为)。
func scriptCacheSet(key string, val any, ttlSec int) {
	if key == "" || ttlSec <= 0 {
		return
	}
	data, err := json.Marshal(val)
	if err != nil || len(data) > maxScriptCacheBytes {
		return
	}
	now := nowFunc()
	scriptCacheMu.Lock()
	for k, e := range scriptCache {
		if now.After(e.expires) {
			delete(scriptCache, k)
		}
	}
	if _, exists := scriptCache[key]; exists || len(scriptCache) < maxScriptCacheEntries {
		scriptCache[key] = scriptCacheEntry{data: data, expires: now.Add(time.Duration(ttlSec) * time.Second)}
	}
	scriptCacheMu.Unlock()
}

func cloneQueryResult(res *datasource.QueryResult) *datasource.QueryResult {
	if res == nil {
		return nil
	}
	return &datasource.QueryResult{
		Columns: append([]string(nil), res.Columns...),
		Rows:    cloneRows(res.Rows),
	}
}

// pruneExpiredLocked 顺带清理过期项, 防止键无限增长。调用方须持锁。
func pruneExpiredLocked(now time.Time) {
	for k, e := range queryCache {
		if now.After(e.expires) {
			delete(queryCache, k)
		}
	}
}

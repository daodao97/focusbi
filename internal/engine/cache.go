package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"xproxy/internal/datasource"

	"golang.org/x/sync/singleflight"
)

// 查询结果缓存 (移植自 dataddy sql_cache):
// 以 dsn + SQL + 参数 + TTL 策略为键缓存 datasource.Query 的结果, 在 TTL 内重复执行直接命中,
// 降低数据库压力、加速重查询。仅缓存成功结果; 出错不缓存。

// nowFunc 便于测试覆写时间源。
var nowFunc = time.Now

type cacheEntry struct {
	dsn     string
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

// executeQueryContext 便于验证请求取消与单次运行查询合并。
var executeQueryContext = datasource.QueryContext

var (
	queryCacheMu    sync.Mutex
	queryCache      = map[string]cacheEntry{}
	queryCacheGroup singleflight.Group
)

// cacheKey 生成 dsn+sql+args 的稳定键。
func cacheKey(identity, sql string, args ...any) string {
	argBytes, err := json.Marshal(args)
	if err != nil {
		argBytes = []byte(fmt.Sprint(args...))
	}
	h := sha256.Sum256([]byte(identity + "\x00" + sql + "\x00" + string(argBytes)))
	return hex.EncodeToString(h[:])
}

// cachedQuery 在 ttl>0 时走缓存执行查询; ttl<=0 时直连。
// bypass=true (前端刷新) 时跳过读缓存但仍回写, 让刷新后的新数据对其他访问者生效。
// 命中缓存返回结果副本, 避免后续列格式化/排序/透视等处理污染缓存。
func cachedQuery(dsn, sql string, ttlSec int, bypass bool, args ...any) (*datasource.QueryResult, error) {
	return cachedQueryContext(context.Background(), dsn, sql, ttlSec, bypass, args...)
}

func cachedQueryContext(ctx context.Context, dsn, sql string, ttlSec int, bypass bool, args ...any) (*datasource.QueryResult, error) {
	if ttlSec <= 0 {
		return executeQueryContext(ctx, dsn, sql, args...)
	}
	identity, err := datasource.CacheIdentity(dsn)
	if err != nil {
		return nil, err
	}
	// TTL 是缓存策略的一部分。同一 SQL 使用不同 TTL 时不能共享条目，否则短 TTL
	// 调用方可能持续命中另一个报表写入的长 TTL 数据。
	key := fmt.Sprintf("%s:ttl=%d", cacheKey(identity, sql, args...), ttlSec)
	now := nowFunc()

	if !bypass {
		queryCacheMu.Lock()
		if e, ok := queryCache[key]; ok && now.Before(e.expires) {
			queryCacheMu.Unlock()
			return cloneQueryResult(e.res), nil
		}
		queryCacheMu.Unlock()
	}

	// 同缓存键、同 TTL 的并发回源只执行一次。共享查询使用独立上下文，单个客户端断开
	// 只停止自己的等待，不会取消其它请求仍在等待的数据库查询。
	ch := queryCacheGroup.DoChan(key, func() (any, error) {
		// 首次查缓存与进入 singleflight 之间可能已有其它回源完成，执行前再检查一次。
		if !bypass {
			now := nowFunc()
			queryCacheMu.Lock()
			if e, ok := queryCache[key]; ok && now.Before(e.expires) {
				queryCacheMu.Unlock()
				return cloneQueryResult(e.res), nil
			}
			queryCacheMu.Unlock()
		}

		res, err := executeQueryContext(context.Background(), dsn, sql, args...)
		if err != nil {
			return nil, err
		}
		if len(res.Rows) <= maxCacheRows {
			storedAt := nowFunc()
			queryCacheMu.Lock()
			pruneExpiredLocked(storedAt)
			if _, exists := queryCache[key]; exists || len(queryCache) < maxCacheEntries {
				queryCache[key] = cacheEntry{dsn: normalizedDSNName(dsn), res: cloneQueryResult(res), expires: storedAt.Add(time.Duration(ttlSec) * time.Second)}
			}
			queryCacheMu.Unlock()
		}
		return res, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-ch:
		if result.Err != nil {
			return nil, result.Err
		}
		return cloneQueryResult(result.Val.(*datasource.QueryResult)), nil
	}
}

// InvalidateQueryCache 清理某个数据源在当前进程的全部查询结果缓存。
// 多实例场景仍由 CacheIdentity 保证其它实例不会命中旧配置条目。
func InvalidateQueryCache(dsn string) {
	dsn = normalizedDSNName(dsn)
	queryCacheMu.Lock()
	for key, entry := range queryCache {
		if entry.dsn == dsn {
			delete(queryCache, key)
		}
	}
	queryCacheMu.Unlock()
}

func normalizedDSNName(dsn string) string {
	if dsn == "" {
		return "default"
	}
	return dsn
}

// 脚本自定义缓存 (脚本 API cache.get/set): 进程内 KV, 值 JSON 序列化存储 (天然深拷贝,
// 防止多次运行共享可变对象)。键自动按报表作用域隔离。
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
func scriptCacheGet(scope, key string) (any, bool) {
	key = scriptCacheKey(scope, key)
	if key == "" {
		return nil, false
	}
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
func scriptCacheSet(scope, key string, val any, ttlSec int) {
	key = scriptCacheKey(scope, key)
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

func scriptCacheKey(scope, key string) string {
	if scope == "" || key == "" {
		return ""
	}
	return scope + "\x00" + key
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

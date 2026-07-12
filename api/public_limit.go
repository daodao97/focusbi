package api

import (
	"context"
	"time"

	"github.com/daodao97/xgo/xredis"
)

const (
	publicRunGlobalLimit   = 10
	publicRunPerTokenLimit = 2
	// 给请求收尾和 Redis 释放留出余量, 避免租约早于 Runner deadline 到期。
	publicRunLeaseBuffer = time.Minute
)

const publicRunAcquireScript = `
local redisTime = redis.call('TIME')
local now = tonumber(redisTime[1]) * 1000 + math.floor(tonumber(redisTime[2]) / 1000)
local ttlSec = tonumber(ARGV[3])
local expires = now + ttlSec * 1000
redis.call('ZREMRANGEBYSCORE', KEYS[1], '-inf', now)
redis.call('ZREMRANGEBYSCORE', KEYS[2], '-inf', now)
if redis.call('ZCARD', KEYS[1]) >= tonumber(ARGV[1]) or redis.call('ZCARD', KEYS[2]) >= tonumber(ARGV[2]) then
  return 0
end
redis.call('ZADD', KEYS[1], expires, ARGV[4])
redis.call('ZADD', KEYS[2], expires, ARGV[4])
local function expireAfterLastLease(key)
  local last = redis.call('ZREVRANGE', key, 0, 0, 'WITHSCORES')
  if #last >= 2 then
    redis.call('PEXPIREAT', key, math.ceil(tonumber(last[2])))
  end
end
expireAfterLastLease(KEYS[1])
expireAfterLastLease(KEYS[2])
return 1
`

const publicRunReleaseScript = `
for _, key in ipairs(KEYS) do
  redis.call('ZREM', key, ARGV[1])
  if redis.call('ZCARD', key) == 0 then
    redis.call('DEL', key)
  end
end
return 1
`

// publicRunEval 便于单测替换; 生产环境统一使用项目已初始化的默认 Redis。
var publicRunEval = func(ctx context.Context, script string, keys []string, args ...any) (int64, error) {
	return xredis.Get().Eval(ctx, script, keys, args...).Int64()
}

func acquirePublicRun(ctx context.Context, token string, reportTimeout time.Duration) (release func(), allowed bool, err error) {
	keys := publicRunKeys(token)
	leaseTTL := reportTimeout + publicRunLeaseBuffer
	leaseID := genShareToken()
	allowedInt, err := publicRunEval(ctx, publicRunAcquireScript, keys,
		publicRunGlobalLimit, publicRunPerTokenLimit, int(leaseTTL/time.Second), leaseID)
	if err != nil || allowedInt == 0 {
		return nil, false, err
	}
	return func() {
		// 请求可能已取消, 释放操作使用独立短超时, 避免名额只能等待 TTL 回收。
		releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, _ = publicRunEval(releaseCtx, publicRunReleaseScript, keys, leaseID)
	}, true, nil
}

func publicRunKeys(token string) []string {
	// Redis Cluster 的 Lua 脚本要求所有 key 位于同一 slot, 故使用相同 hash tag。
	return []string{"{focusbi:public_run}:global", "{focusbi:public_run}:token:" + token}
}

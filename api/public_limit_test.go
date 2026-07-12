package api

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPublicRunLeaseDoesNotShortenExistingTTL(t *testing.T) {
	if !strings.Contains(publicRunAcquireScript, "ZREVRANGE") ||
		!strings.Contains(publicRunAcquireScript, "PEXPIREAT") ||
		strings.Contains(publicRunAcquireScript, "redis.call('EXPIRE'") {
		t.Fatal("申请脚本必须按最晚租约设置过期时间, 不能用当前请求 TTL 覆盖旧租约")
	}
}

func TestAcquirePublicRun(t *testing.T) {
	original := publicRunEval
	defer func() { publicRunEval = original }()

	var scripts []string
	var gotKeys [][]string
	var acquireArgs []any
	publicRunEval = func(_ context.Context, script string, keys []string, args ...any) (int64, error) {
		scripts = append(scripts, script)
		gotKeys = append(gotKeys, append([]string(nil), keys...))
		if script == publicRunAcquireScript {
			acquireArgs = append([]any(nil), args...)
		}
		return 1, nil
	}

	reportTimeout := 8 * time.Minute
	release, allowed, err := acquirePublicRun(context.Background(), "abc", reportTimeout)
	if err != nil || !allowed || release == nil {
		t.Fatalf("申请应成功: allowed=%v err=%v", allowed, err)
	}
	release()
	wantKeys := []string{"{focusbi:public_run}:global", "{focusbi:public_run}:token:abc"}
	if len(scripts) != 2 || scripts[0] != publicRunAcquireScript || scripts[1] != publicRunReleaseScript {
		t.Fatalf("应依次执行申请和释放脚本")
	}
	for _, keys := range gotKeys {
		if !reflect.DeepEqual(keys, wantKeys) {
			t.Fatalf("Redis keys=%v, want %v", keys, wantKeys)
		}
	}
	wantTTL := int((reportTimeout + publicRunLeaseBuffer) / time.Second)
	if len(acquireArgs) != 4 || acquireArgs[2] != wantTTL {
		t.Fatalf("租约 TTL 参数=%v, want %d", acquireArgs, wantTTL)
	}
}

func TestAcquirePublicRunRejectedAndRedisError(t *testing.T) {
	original := publicRunEval
	defer func() { publicRunEval = original }()

	publicRunEval = func(context.Context, string, []string, ...any) (int64, error) { return 0, nil }
	if release, allowed, err := acquirePublicRun(context.Background(), "abc", 10*time.Minute); err != nil || allowed || release != nil {
		t.Fatalf("达到上限应拒绝: allowed=%v err=%v", allowed, err)
	}

	wantErr := errors.New("redis unavailable")
	publicRunEval = func(context.Context, string, []string, ...any) (int64, error) { return 0, wantErr }
	if _, allowed, err := acquirePublicRun(context.Background(), "abc", 10*time.Minute); allowed || !errors.Is(err, wantErr) {
		t.Fatalf("Redis 错误应向上返回: allowed=%v err=%v", allowed, err)
	}
}

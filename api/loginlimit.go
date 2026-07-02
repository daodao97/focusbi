package api

import (
	"strings"
	"sync"
	"time"
)

// 登录失败退避 (防暴力破解): 按 IP+用户名 记失败次数, 达到阈值后锁定一段时间,
// 时长随失败次数指数增长。成功登录清零。单实例内存实现, 重启即清。
// ponytail: 内存 map + 全局锁, 多实例共享限流再上 redis。

const (
	loginFailThreshold = 5                // 连续失败这么多次后开始锁定
	loginLockBase      = 30 * time.Second // 首次锁定时长, 之后每多错一次翻倍
	loginLockMax       = 15 * time.Minute // 锁定时长上限
	loginFailTTL       = time.Hour        // 记录过期时间 (最后一次失败起算)
)

type loginFailEntry struct {
	fails    int
	lastFail time.Time
	lockedTo time.Time
}

var (
	loginFailMu sync.Mutex
	loginFails  = map[string]*loginFailEntry{}
)

func loginFailKey(ip, username string) string {
	return ip + "\x00" + strings.ToLower(strings.TrimSpace(username))
}

// loginLocked 返回该 IP+用户名当前是否处于锁定期, 以及剩余等待时长。
func loginLocked(ip, username string) (bool, time.Duration) {
	now := time.Now()
	loginFailMu.Lock()
	defer loginFailMu.Unlock()

	e, ok := loginFails[loginFailKey(ip, username)]
	if !ok {
		return false, 0
	}
	if now.Sub(e.lastFail) > loginFailTTL {
		delete(loginFails, loginFailKey(ip, username))
		return false, 0
	}
	if now.Before(e.lockedTo) {
		return true, e.lockedTo.Sub(now).Round(time.Second)
	}
	return false, 0
}

// loginFailed 记一次失败; 达到阈值后设置指数退避锁定。
func loginFailed(ip, username string) {
	now := time.Now()
	key := loginFailKey(ip, username)

	loginFailMu.Lock()
	defer loginFailMu.Unlock()

	e := loginFails[key]
	if e == nil || now.Sub(e.lastFail) > loginFailTTL {
		e = &loginFailEntry{}
		loginFails[key] = e
	}
	e.fails++
	e.lastFail = now
	if e.fails >= loginFailThreshold {
		lock := loginLockBase << (e.fails - loginFailThreshold) // 30s, 1m, 2m, ...
		if lock > loginLockMax || lock <= 0 {
			lock = loginLockMax
		}
		e.lockedTo = now.Add(lock)
	}

	// 顺带清理过期记录, 防 map 无限增长。
	for k, v := range loginFails {
		if now.Sub(v.lastFail) > loginFailTTL {
			delete(loginFails, k)
		}
	}
}

// loginSucceeded 成功登录清零。
func loginSucceeded(ip, username string) {
	loginFailMu.Lock()
	delete(loginFails, loginFailKey(ip, username))
	loginFailMu.Unlock()
}

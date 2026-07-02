package api

import (
	"testing"
	"time"
)

func resetLoginFailsForTest() {
	loginFailMu.Lock()
	loginFails = map[string]*loginFailEntry{}
	loginFailMu.Unlock()
}

func TestLoginBackoff(t *testing.T) {
	resetLoginFailsForTest()
	ip, user := "1.2.3.4", "admin"

	// 阈值内不锁定
	for i := 0; i < loginFailThreshold-1; i++ {
		loginFailed(ip, user)
	}
	if locked, _ := loginLocked(ip, user); locked {
		t.Fatal("阈值内不应锁定")
	}

	// 达阈值后锁定
	loginFailed(ip, user)
	locked, wait := loginLocked(ip, user)
	if !locked {
		t.Fatal("达阈值后应锁定")
	}
	if wait <= 0 || wait > loginLockBase {
		t.Fatalf("首次锁定时长 = %v, 应在 (0, %v]", wait, loginLockBase)
	}

	// 继续失败, 锁定时长增长
	loginFailed(ip, user)
	_, wait2 := loginLocked(ip, user)
	if wait2 <= wait {
		t.Fatalf("锁定时长应指数增长: %v -> %v", wait, wait2)
	}

	// 不同 IP / 用户名互不影响
	if locked, _ := loginLocked("5.6.7.8", user); locked {
		t.Error("其他 IP 不应被锁定")
	}
	if locked, _ := loginLocked(ip, "other"); locked {
		t.Error("其他用户名不应被锁定")
	}

	// 成功登录清零
	loginSucceeded(ip, user)
	if locked, _ := loginLocked(ip, user); locked {
		t.Error("成功登录后应解除锁定")
	}
}

func TestLoginBackoffLockCap(t *testing.T) {
	resetLoginFailsForTest()
	ip, user := "1.2.3.4", "admin"
	for i := 0; i < loginFailThreshold+30; i++ { // 疯狂失败, 触发上限
		loginFailed(ip, user)
	}
	_, wait := loginLocked(ip, user)
	if wait > loginLockMax {
		t.Fatalf("锁定时长 = %v, 不应超过上限 %v", wait, loginLockMax)
	}
	if wait < loginLockMax-time.Second {
		t.Fatalf("大量失败后应达到上限 %v, 实际 %v", loginLockMax, wait)
	}
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// fail: 生产环境下 5xx 泛化不泄露原文, 4xx 业务提示原样返回; dev 下 5xx 透出原文。
func TestFailMasksInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	msgOf := func(status int, detail string, dev bool) string {
		orig := devEnv
		devEnv = func() bool { return dev }
		defer func() { devEnv = orig }()

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		fail(c, status, detail)

		var body struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return body.Msg
	}

	secret := "dial tcp 10.0.0.1:3306: connection refused"

	// 生产: 500 泛化, 不含内部细节。
	if got := msgOf(http.StatusInternalServerError, secret, false); strings.Contains(got, "10.0.0.1") || got == secret {
		t.Errorf("生产 500 应泛化, 却泄露原文: %q", got)
	}
	// dev: 500 透出原文便于调试。
	if got := msgOf(http.StatusInternalServerError, secret, true); got != secret {
		t.Errorf("dev 500 应透出原文, got %q", got)
	}
	// 4xx 业务提示始终原样 (即便生产)。
	if got := msgOf(http.StatusBadRequest, "报表不存在", false); got != "报表不存在" {
		t.Errorf("4xx 业务提示应原样返回, got %q", got)
	}
	if got := msgOf(http.StatusForbidden, "无权修改该报表", false); got != "无权修改该报表" {
		t.Errorf("403 提示应原样返回, got %q", got)
	}
}

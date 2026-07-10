package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"xproxy/conf"

	"github.com/gin-gonic/gin"
)

func TestIssueAndReturnUsesHttpOnlyCookie(t *testing.T) {
	old := conf.ConfInstance
	conf.ConfInstance = &conf.Conf{Site: conf.SiteConf{JWTSecret: "test-secret-with-enough-entropy"}}
	defer func() { conf.ConfInstance = old }()

	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/auth/login", nil)
	issueAndReturn(c, 1, "admin", true)

	cookie := w.Header().Get("Set-Cookie")
	for _, want := range []string{"focusbi_session=", "HttpOnly", "SameSite=Strict"} {
		if !strings.Contains(cookie, want) {
			t.Fatalf("Set-Cookie 缺少 %q: %s", want, cookie)
		}
	}
	if strings.Contains(w.Body.String(), `"token"`) {
		t.Fatalf("响应体不应暴露 JWT: %s", w.Body.String())
	}
}

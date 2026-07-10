package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBearerTokenSupportsCookieAndHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/", nil)
	c.Request.AddCookie(&http.Cookie{Name: SessionCookieName, Value: "cookie-token"})
	if got := bearerToken(c); got != "cookie-token" {
		t.Fatalf("cookie token = %q", got)
	}
	c.Request.Header.Set("Authorization", "Bearer header-token")
	if got := bearerToken(c); got != "header-token" {
		t.Fatalf("header token = %q", got)
	}
}

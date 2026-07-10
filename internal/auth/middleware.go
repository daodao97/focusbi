package auth

import (
	"net/http"
	"strings"

	"xproxy/dao"

	"github.com/gin-gonic/gin"
)

const (
	ctxUser = "auth_user"
	ctxPerm = "auth_perm"
	// SessionCookieName 是控制台浏览器登录态使用的 HttpOnly Cookie。
	SessionCookieName = "focusbi_session"
)

// Middleware 从 Bearer 或 HttpOnly 会话 Cookie 解析 JWT, 加载用户并构建权限, 注入 gin.Context。
// 未携带/无效 token 时返回 401。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c)
		if token == "" {
			abort401(c, "未登录")
			return
		}
		claims, err := ParseToken(token)
		if err != nil {
			abort401(c, "登录已失效")
			return
		}
		user, err := dao.GetUserByID(claims.UID)
		if err != nil {
			abort401(c, "用户不存在")
			return
		}
		perm, err := NewPermission(user)
		if err != nil {
			abort401(c, "加载权限失败")
			return
		}
		c.Set(ctxUser, user)
		c.Set(ctxPerm, perm)
		c.Next()
	}
}

// Require 返回一个守卫: 要求当前用户对 resource 拥有 mode 权限, 否则 403。
func Require(resource, mode string) gin.HandlerFunc {
	return func(c *gin.Context) {
		p := PermOf(c)
		if p == nil || !p.Check(resource, mode) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权限: " + resource})
			return
		}
		c.Next()
	}
}

// RequireReportWriter 返回一个守卫: 要求当前用户在任意范围拥有报表写权限
// (即"是报表开发者"), 否则 403。用于没有具体报表 id 的写入口 (模板预览、AI 改写、
// 根目录建报表、全局定时任务管理页)。具体某报表能否写由 handler 内 requireReportWrite 判定。
func RequireReportWriter() gin.HandlerFunc {
	return func(c *gin.Context) {
		p := PermOf(c)
		if p == nil || !p.CanWriteAnyReport() {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权限: 需要报表写权限"})
			return
		}
		c.Next()
	}
}

// RequireAdmin 要求当前用户为超管。
func RequireAdmin() gin.HandlerFunc {
	return func(c *gin.Context) {
		u := UserOf(c)
		if u == nil || !u.IsAdmin {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 1, "msg": "需要管理员权限"})
			return
		}
		c.Next()
	}
}

// UserOf 取当前登录用户 (未登录返回 nil)。
func UserOf(c *gin.Context) *dao.UserRecord {
	if v, ok := c.Get(ctxUser); ok {
		if u, ok := v.(*dao.UserRecord); ok {
			return u
		}
	}
	return nil
}

// PermOf 取当前用户的权限判定器。
func PermOf(c *gin.Context) *Permission {
	if v, ok := c.Get(ctxPerm); ok {
		if p, ok := v.(*Permission); ok {
			return p
		}
	}
	return nil
}

func bearerToken(c *gin.Context) string {
	h := c.GetHeader("Authorization")
	if h == "" {
		token, _ := c.Cookie(SessionCookieName)
		return strings.TrimSpace(token)
	}
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return strings.TrimSpace(h)
}

func abort401(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": msg})
}

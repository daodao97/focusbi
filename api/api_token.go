package api

import (
	"net/http"
	"time"

	"xproxy/dao"
	"xproxy/internal/auth"

	"github.com/gin-gonic/gin"
)

// API Token 管理 (登录态): 供用户生成长期令牌, 用于 MCP 等程序化访问。
// 令牌明文仅在创建时返回一次, 之后只保留哈希。

// listAPITokens 列出当前用户的全部令牌 (不含明文/哈希)。
func listAPITokens(c *gin.Context) {
	u := auth.UserOf(c)
	if u == nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	tokens, err := dao.ListAPITokensByUser(u.Id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, tokens)
}

type createAPITokenRequest struct {
	Name       string `json:"name"`
	ExpireDays int    `json:"expire_days"` // 0 = 永不过期
}

// createAPIToken 生成新令牌; 明文 token 仅此一次返回。
func createAPIToken(c *gin.Context) {
	u := auth.UserOf(c)
	if u == nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	var req createAPITokenRequest
	_ = c.ShouldBindJSON(&req)

	var ttl *time.Duration
	if req.ExpireDays > 0 {
		d := time.Duration(req.ExpireDays) * 24 * time.Hour
		ttl = &d
	}
	plain, rec, err := dao.CreateAPIToken(u.Id, req.Name, ttl)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// token 字段仅此一次出现; 前端须提示用户立即复制保存。
	ok(c, gin.H{
		"token":        plain,
		"id":           rec.Id,
		"name":         rec.Name,
		"token_prefix": rec.TokenPrefix,
		"expires_at":   rec.ExpiresAt,
		"created_at":   rec.CreatedAt,
	})
}

// deleteAPIToken 删除当前用户的某个令牌 (带 user_id 条件防越权)。
func deleteAPIToken(c *gin.Context) {
	u := auth.UserOf(c)
	if u == nil {
		fail(c, http.StatusUnauthorized, "未登录")
		return
	}
	id, ok2 := paramID(c)
	if !ok2 {
		return
	}
	if err := dao.DeleteAPIToken(id, u.Id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"deleted": true})
}

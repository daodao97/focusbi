// Package mcpserver 暴露 FocusBI 的报表开发能力为 MCP (Model Context Protocol) 服务,
// 让 Codex / Claude Code 等 AI 工具直接读语法、探 schema、试跑模板、开发报表。
//
// 鉴权复用现有体系: Bearer 令牌 (API Token 或登录 JWT) -> 用户 -> RBAC 权限判定,
// 绝不绕过权限。每个工具入口都基于调用者的 *auth.Permission 做校验。
package mcpserver

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xproxy/dao"
	"xproxy/internal/auth"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
)

// ctxUserKey 在请求上下文里携带已解析的用户 + 权限, 避免每个工具重复查库。
type ctxUserKey struct{}

type principal struct {
	user *dao.UserRecord
	perm *auth.Permission
}

// VerifyToken 是 MCP SDK RequireBearerToken 的 TokenVerifier:
// 校验 Bearer 令牌并解析出用户, 校验失败返回 unwrap 到 mcpauth.ErrInvalidToken 的错误。
//
// 支持两类令牌:
//   - fbt_ 开头: API Token (长期, 供 MCP 程序化访问) —— 算哈希查库 + 过期校验。
//   - 其它: 登录签发的 JWT (兼容直接用登录 token 的场景)。
func VerifyToken(ctx context.Context, token string, _ *http.Request) (*mcpauth.TokenInfo, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, fmt.Errorf("%w: empty token", mcpauth.ErrInvalidToken)
	}

	uid, exp, err := resolveUserID(token)
	if err != nil {
		return nil, err
	}

	user, err := dao.GetUserByID(uid)
	if err != nil || user == nil {
		return nil, fmt.Errorf("%w: user not found", mcpauth.ErrInvalidToken)
	}

	// SDK 要求 TokenInfo.Expiration 为将来的非零时间 (否则 401 "token missing expiration")。
	// 令牌自带过期时间则用之; 永不过期的令牌给一个滑动窗口, 仅用于约束 MCP 会话时长。
	return &mcpauth.TokenInfo{
		UserID:     strconv.Itoa(uid),
		Expiration: exp,
	}, nil
}

// sessionWindow 是无显式过期的令牌 (永不过期 API token / 登录 JWT) 在 MCP 侧的会话上限。
const sessionWindow = 24 * time.Hour

// resolveUserID 把 Bearer 令牌解析为用户 id 与该令牌的过期时间 (供 SDK 约束会话)。
func resolveUserID(token string) (int, time.Time, error) {
	if strings.HasPrefix(token, "fbt_") {
		rec, err := dao.GetAPITokenByHash(dao.HashAPIToken(token))
		if err != nil {
			return 0, time.Time{}, fmt.Errorf("%w: api token invalid", mcpauth.ErrInvalidToken)
		}
		if rec.Expired() {
			return 0, time.Time{}, fmt.Errorf("%w: api token expired", mcpauth.ErrInvalidToken)
		}
		// 尽力更新 last_used_at, 失败不影响鉴权。
		_ = dao.TouchAPIToken(rec.Id)
		exp := time.Now().Add(sessionWindow)
		if rec.ExpiresAt != nil && rec.ExpiresAt.Before(exp) {
			exp = *rec.ExpiresAt // 令牌真实过期更早则以它为准
		}
		return rec.UserID, exp, nil
	}

	claims, err := auth.ParseToken(token)
	if err != nil {
		return 0, time.Time{}, fmt.Errorf("%w: %v", mcpauth.ErrInvalidToken, err)
	}
	return claims.UID, time.Now().Add(sessionWindow), nil
}

// withPrincipal 在 ctx 上挂载已解析的用户 + 权限。
func withPrincipal(ctx context.Context, p *principal) context.Context {
	return context.WithValue(ctx, ctxUserKey{}, p)
}

// principalFromContext 从 ctx 取出调用者的用户 + 权限。
// SDK 已在 ctx 放入 TokenInfo(含 UserID); 这里据此加载 user 并编译权限, 结果缓存到 ctx。
func principalFromContext(ctx context.Context) (*principal, error) {
	if p, ok := ctx.Value(ctxUserKey{}).(*principal); ok && p != nil {
		return p, nil
	}
	ti := mcpauth.TokenInfoFromContext(ctx)
	if ti == nil || ti.UserID == "" {
		return nil, fmt.Errorf("未认证")
	}
	uid, err := strconv.Atoi(ti.UserID)
	if err != nil {
		return nil, fmt.Errorf("无效的用户标识")
	}
	user, err := dao.GetUserByID(uid)
	if err != nil || user == nil {
		return nil, fmt.Errorf("用户不存在")
	}
	perm, err := auth.NewPermission(user)
	if err != nil {
		return nil, fmt.Errorf("加载权限失败: %w", err)
	}
	return &principal{user: user, perm: perm}, nil
}

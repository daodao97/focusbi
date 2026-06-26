package api

import (
	"net/http"

	"xproxy/internal/mcpserver"

	"github.com/gin-gonic/gin"
	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// registerMCP 把 MCP (Model Context Protocol) 服务挂到 /mcp。
//
// 鉴权: 不走 gin 的 auth.Middleware, 改用 MCP SDK 的 RequireBearerToken 中间件 ——
// 它校验 Authorization: Bearer 令牌 (API Token 或登录 JWT), 把 TokenInfo 注入请求
// 上下文并贯穿到各工具 handler; 无效令牌返回 401 + WWW-Authenticate (RFC 9728)。
// 工具内部再基于调用者的 RBAC 权限做细粒度判定。
func registerMCP(e *gin.Engine) {
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return mcpserver.NewServer() },
		nil,
	)
	authed := mcpauth.RequireBearerToken(mcpserver.VerifyToken, &mcpauth.RequireBearerTokenOptions{})(handler)

	// StreamableHTTP 使用同一路径处理 GET/POST/DELETE。
	e.Any("/mcp", gin.WrapH(authed))
	e.Any("/mcp/*any", gin.WrapH(authed))
}

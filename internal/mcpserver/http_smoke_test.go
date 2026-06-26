package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 验证 /mcp 的 Bearer 中间件: 无 token -> 401。
func TestMCPHTTPRequiresBearer(t *testing.T) {
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return NewServer() }, nil)
	authed := mcpauth.RequireBearerToken(VerifyToken, &mcpauth.RequireBearerTokenOptions{})(handler)

	srv := httptest.NewServer(authed)
	defer srv.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("req: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401, got %d", resp.StatusCode)
	}
}

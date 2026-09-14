package mcpserver

import "testing"

// 覆盖全部工具的 schema 推导，避免请求到达时才因非法标签 panic。
func TestNewServerRegistersTools(t *testing.T) {
	if NewServer() == nil {
		t.Fatal("MCP 服务初始化失败")
	}
}

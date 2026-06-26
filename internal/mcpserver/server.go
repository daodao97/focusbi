package mcpserver

import (
	"context"

	"xproxy/docs"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// serverInstructions 指导 AI 客户端如何用本服务开发报表。
const serverInstructions = `FocusBI 报表开发 MCP 服务。典型流程:
1. 先用 get_syntax_doc (或读资源 focusbi://syntax) 了解报表模板语法;
2. 用 list_datasources / list_tables / describe_table / query_raw 探数据源 schema;
3. 编写模板后用 preview_template 试跑校验 (看每个 block 的 error 字段);
4. create_report 创建, update_report 改开发版草稿, publish_report 发布。
所有操作都受调用者的 RBAC 权限限制。`

// NewServer 构造一个注册了报表开发工具与资源的 MCP server。
// 每次 HTTP 会话调用 (getServer), 保证工具集一致; 鉴权由 HTTP 层 Bearer 中间件负责。
func NewServer() *mcp.Server {
	s := mcp.NewServer(
		&mcp.Implementation{Name: "focusbi", Title: "FocusBI 报表开发", Version: "1.0.0"},
		&mcp.ServerOptions{Instructions: serverInstructions},
	)

	registerTools(s)

	// 资源: 报表语法文档 (与 get_syntax_doc 同源, 资源形式更符合 MCP 习惯)。
	s.AddResource(
		&mcp.Resource{
			URI:         "focusbi://syntax",
			Name:        "report-syntax",
			Title:       "报表模板语法",
			Description: "FocusBI 报表模板的完整权威语法说明",
			MIMEType:    "text/markdown",
		},
		func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
			return &mcp.ReadResourceResult{
				Contents: []*mcp.ResourceContents{{
					URI:      "focusbi://syntax",
					MIMEType: "text/markdown",
					Text:     docs.SyntaxMarkdown,
				}},
			}, nil
		},
	)

	return s
}

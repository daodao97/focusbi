// Package docs 内嵌项目文档, 供后端复用 (AI prompt / 文档接口)。
package docs

import _ "embed"

// SyntaxMarkdown 是报表模板语法的权威说明 (docs/SYNTAX.md)。
// 既作为 AI 助手的 system prompt 依据, 也由前端文档按钮展示, 保证两者同源。
//
//go:embed SYNTAX.md
var SyntaxMarkdown string

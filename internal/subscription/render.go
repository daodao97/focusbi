package subscription

import (
	"fmt"
	"strings"

	"xproxy/internal/engine"

	"github.com/spf13/cast"
)

// 渲染上限: 控制推送消息长度 (IM 对单条消息有长度限制)。
const (
	maxBlocks   = 6  // 最多展示的区块数
	maxRows     = 8  // 每个表格最多展示的行数
	maxCols     = 6  // 每个表格最多展示的列数
	maxCellText = 24 // 单元格文本截断长度
)

// RenderText 把报表执行结果渲染为纯文本消息 (飞书/企微 text 通用)。
// reportName 报表名, viewURL 为空时不附链接。
func RenderText(reportName string, r *engine.Result, viewURL string) string {
	var b strings.Builder
	b.WriteString("📊 ")
	b.WriteString(reportName)
	b.WriteString("\n")

	shown := 0
	for _, blk := range r.Blocks {
		if shown >= maxBlocks {
			b.WriteString(fmt.Sprintf("…还有 %d 个区块未展示\n", len(r.Blocks)-shown))
			break
		}
		switch blk.Type {
		case "table":
			b.WriteString(renderTable(&blk))
			shown++
		case "markdown", "raw":
			if txt := strings.TrimSpace(blk.Markdown); txt != "" {
				b.WriteString("\n")
				b.WriteString(truncate(txt, 200))
				b.WriteString("\n")
				shown++
			}
		}
	}

	if viewURL != "" {
		b.WriteString("\n🔗 ")
		b.WriteString(viewURL)
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderTable 渲染单个表格区块为缩进文本摘要。
func renderTable(blk *engine.Block) string {
	var b strings.Builder
	title := blk.Title
	if title == "" {
		title = blk.ID
	}
	b.WriteString("\n【")
	b.WriteString(title)
	b.WriteString("】")
	if blk.Error != "" {
		b.WriteString(" ⚠️ " + truncate(blk.Error, 80))
		b.WriteString("\n")
		return b.String()
	}
	if len(blk.Rows) == 0 {
		b.WriteString(" 无数据\n")
		return b.String()
	}
	b.WriteString("\n")

	cols := blk.Columns
	if len(cols) > maxCols {
		cols = cols[:maxCols]
	}

	// 表头
	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = c.Header
	}
	b.WriteString(strings.Join(heads, " | "))
	b.WriteString("\n")

	// 数据行
	n := len(blk.Rows)
	if n > maxRows {
		n = maxRows
	}
	for i := 0; i < n; i++ {
		cells := make([]string, len(cols))
		for j, c := range cols {
			cells[j] = truncate(cast.ToString(blk.Rows[i][c.Name]), maxCellText)
		}
		b.WriteString(strings.Join(cells, " | "))
		b.WriteString("\n")
	}
	if len(blk.Rows) > maxRows {
		b.WriteString(fmt.Sprintf("…共 %d 行\n", len(blk.Rows)))
	}
	return b.String()
}

func truncate(s string, max int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

package engine

import (
	"strings"
	"testing"
)

// 回归: #!MARKDOWN/#!RAW 放在 SQL 前不再吞掉后续 SQL (用户反馈的 bug)。

func TestMarkdownBlockEndDoesNotSwallowSQL(t *testing.T) {
	content := "#!MARKDOWN\n# 报表说明\n本表 T+1 更新\n#!END\nSELECT day, amount FROM sales;"
	blocks := splitBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("应切成 2 块 (markdown + sql), got %d: %#v", len(blocks), blocks)
	}
	// 第一块是 markdown, 不含 SQL
	if !strings.HasPrefix(strings.TrimSpace(blocks[0]), "#!MARKDOWN") {
		t.Errorf("第一块应是 markdown: %q", blocks[0])
	}
	if strings.Contains(blocks[0], "SELECT") {
		t.Errorf("markdown 块不应吞掉 SELECT: %q", blocks[0])
	}
	// 第二块是 SQL, 能被判定为 sql kind
	rb := parseBlock(blocks[1])
	if rb.kind != "sql" {
		t.Errorf("第二块应为 sql, got %s (%q)", rb.kind, blocks[1])
	}
}

func TestRawBlockEndDoesNotSwallowSQL(t *testing.T) {
	content := "#!RAW\n执行时间: {ts}\n#!END\nSELECT 1 AS x;"
	blocks := splitBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("应切成 2 块, got %d: %#v", len(blocks), blocks)
	}
	if parseBlock(blocks[0]).kind != "raw" {
		t.Errorf("第一块应为 raw: %q", blocks[0])
	}
	if parseBlock(blocks[1]).kind != "sql" {
		t.Errorf("第二块应为 sql: %q", blocks[1])
	}
}

// markdown 正文里以 `-- @` 开头的行不应被当成块注解抽走。
func TestMarkdownBodyKeepsDashAt(t *testing.T) {
	content := "#!MARKDOWN\n用法: -- @id=区块标识\n#!END"
	rb := parseBlock(splitBlocks(content)[0])
	if rb.kind != "markdown" {
		t.Fatalf("应为 markdown, got %s", rb.kind)
	}
	if _, ok := rb.annotations["id"]; ok {
		t.Error("markdown 正文里的 -- @id 不应被当成注解")
	}
	body := stripMarker(rb.body)
	if !strings.Contains(body, "-- @id=区块标识") {
		t.Errorf("markdown 正文应原样保留 -- @id 行, got %q", body)
	}
}

// #!END 应在 stripMarker 中被去掉, 不出现在最终正文。
func TestStripMarkerRemovesEnd(t *testing.T) {
	got := stripMarker("#!MARKDOWN\n## 标题\n说明\n#!END")
	if strings.Contains(got, "#!END") || strings.Contains(got, "#!MARKDOWN") {
		t.Errorf("应去掉前后标记, got %q", got)
	}
	if strings.TrimSpace(got) != "## 标题\n说明" {
		t.Errorf("正文不符, got %q", got)
	}
}

// 兼容: 无 #!END 时, markdown 段到下一个标记段起始处结束。
func TestMarkdownWithoutEndStopsAtNextMarker(t *testing.T) {
	content := "#!MARKDOWN\nA\n#!RAW\nB\n#!END"
	blocks := splitBlocks(content)
	if len(blocks) != 2 {
		t.Fatalf("应切成 2 块, got %d: %#v", len(blocks), blocks)
	}
	if parseBlock(blocks[0]).kind != "markdown" || parseBlock(blocks[1]).kind != "raw" {
		t.Errorf("应为 markdown + raw, got %s + %s", parseBlock(blocks[0]).kind, parseBlock(blocks[1]).kind)
	}
}

// 既有契约不变: 末尾的 markdown 仍单独成块。
func TestExistingSplitContractUnchanged(t *testing.T) {
	blocks := splitBlocks("SELECT 1;\nSELECT 2;\n#!MARKDOWN\nhello")
	if len(blocks) != 3 {
		t.Fatalf("应为 3 块, got %d: %#v", len(blocks), blocks)
	}
}

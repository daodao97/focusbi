package ai

import (
	"strings"
	"testing"
)

func TestParseDiffBlocks(t *testing.T) {
	out := "前言\n" +
		markSearch + "\nOLD A\n" + markSep + "\nNEW A\n" + markReplace + "\n" +
		"中间废话\n" +
		markSearch + "\nOLD B\n" + markSep + "\nNEW B\n" + markReplace + "\n"
	blocks, full, ok := parseDiff(out)
	if !ok || full != "" || len(blocks) != 2 {
		t.Fatalf("blocks=%d full=%q ok=%v", len(blocks), full, ok)
	}
	if blocks[0].search != "OLD A" || blocks[0].replace != "NEW A" {
		t.Errorf("block0=%+v", blocks[0])
	}
	if blocks[1].search != "OLD B" || blocks[1].replace != "NEW B" {
		t.Errorf("block1=%+v", blocks[1])
	}
}

func TestParseDiffFull(t *testing.T) {
	out := markFull + "\nSELECT 1;\n" + markFullEnd
	blocks, full, ok := parseDiff(out)
	if !ok || len(blocks) != 0 || full != "SELECT 1;" {
		t.Fatalf("full 通道解析错误: blocks=%d full=%q ok=%v", len(blocks), full, ok)
	}
}

func TestApplyDiffReplace(t *testing.T) {
	cur := "SELECT a FROM t WHERE x=1;"
	got, n := applyDiff(cur, []searchReplace{{search: "x=1", replace: "x=2"}})
	if n != 1 || got != "SELECT a FROM t WHERE x=2;" {
		t.Fatalf("got=%q n=%d", got, n)
	}
}

func TestApplyDiffAppend(t *testing.T) {
	cur := "SELECT 1;"
	got, n := applyDiff(cur, []searchReplace{{search: "", replace: "SELECT 2;"}})
	if n != 1 || !strings.Contains(got, "SELECT 1;") || !strings.Contains(got, "SELECT 2;") {
		t.Fatalf("append 失败: got=%q n=%d", got, n)
	}
}

func TestApplyDiffMiss(t *testing.T) {
	cur := "SELECT 1;"
	_, n := applyDiff(cur, []searchReplace{{search: "NOT EXIST", replace: "x"}})
	if n != 0 {
		t.Errorf("失配应不计入 applied, got %d", n)
	}
}

func TestApplyDiffFuzzyWhitespace(t *testing.T) {
	// SEARCH 行首尾空白与原文不同, 精确匹配失败但 fuzzy 命中
	cur := "line1\n    WHERE day >= '{from}'\nline3"
	blocks := []searchReplace{{search: "WHERE day >= '{from}'", replace: "WHERE day >= '{from}' AND ch='{c}'"}}
	got, n := applyDiff(cur, blocks)
	if n != 1 || !strings.Contains(got, "AND ch='{c}'") {
		t.Fatalf("fuzzy 容错失败: got=%q n=%d", got, n)
	}
	// 其余行不应被破坏
	if !strings.Contains(got, "line1") || !strings.Contains(got, "line3") {
		t.Errorf("误伤其他行: %q", got)
	}
}

package ai

import (
	"strings"
)

// AI 以 SEARCH/REPLACE 块返回对模板的局部修改 (类 Aider/Cursor):
//
//	<<<<<<< SEARCH
//	原文片段 (逐字匹配当前模板)
//	=======
//	替换后的新内容
//	>>>>>>> REPLACE
//
// 可返回多个块。SEARCH 为空 = 把 REPLACE 追加到模板末尾 (新增区块)。
// 若改动过大难以 diff, AI 可用整模板兜底通道:
//
//	<<<FULL>>>
//	完整模板
//	<<<END>>>

const (
	markSearch  = "<<<<<<< SEARCH"
	markSep     = "======="
	markReplace = ">>>>>>> REPLACE"
	markFull    = "<<<FULL>>>"
	markFullEnd = "<<<END>>>"
)

// searchReplace 是一个待应用的替换块。
type searchReplace struct {
	search  string
	replace string
}

// parseDiff 解析 AI 输出。
// 返回: blocks (SEARCH/REPLACE 列表), full (整模板兜底内容, 非空则优先用), ok (是否解析出任何可用结果)。
func parseDiff(out string) (blocks []searchReplace, full string, ok bool) {
	out = strings.TrimSpace(out)

	// 整模板兜底通道
	if i := strings.Index(out, markFull); i >= 0 {
		rest := out[i+len(markFull):]
		if j := strings.Index(rest, markFullEnd); j >= 0 {
			return nil, strings.TrimSpace(rest[:j]), true
		}
		// 无结束标记: 取其后全部
		return nil, strings.TrimSpace(rest), true
	}

	// 逐个解析 SEARCH/REPLACE 块
	for {
		s := strings.Index(out, markSearch)
		if s < 0 {
			break
		}
		sep := strings.Index(out[s:], markSep)
		if sep < 0 {
			break
		}
		sep += s
		rep := strings.Index(out[sep:], markReplace)
		if rep < 0 {
			break
		}
		rep += sep

		search := trimBlock(out[s+len(markSearch) : sep])
		replace := trimBlock(out[sep+len(markSep) : rep])
		blocks = append(blocks, searchReplace{search: search, replace: replace})
		out = out[rep+len(markReplace):]
	}
	return blocks, "", len(blocks) > 0
}

// trimBlock 去掉块内容的首尾换行 (保留内部空白)。
func trimBlock(s string) string {
	return strings.Trim(s, "\r\n")
}

// applyDiff 把 SEARCH/REPLACE 块应用到 current, 返回结果与成功应用的块数。
//   - SEARCH 非空: 在 current 里查找并替换 (精确优先, 失败再忽略行首尾空白匹配)。
//   - SEARCH 为空: REPLACE 追加到末尾。
//   - 匹配不到的块跳过 (不计入 applied)。
func applyDiff(current string, blocks []searchReplace) (string, int) {
	result := current
	applied := 0
	for _, b := range blocks {
		if b.search == "" {
			// 新增: 追加到末尾
			result = strings.TrimRight(result, "\n") + "\n\n" + b.replace + "\n"
			applied++
			continue
		}
		if strings.Contains(result, b.search) {
			result = strings.Replace(result, b.search, b.replace, 1)
			applied++
			continue
		}
		// 容错: 忽略每行首尾空白再匹配一次
		if sp := fuzzyIndex(result, b.search); sp.start >= 0 {
			result = result[:sp.start] + b.replace + result[sp.end:]
			applied++
		}
	}
	return result, applied
}

type span struct{ start, end int }

// fuzzyIndex 在 text 中按"忽略每行首尾空白"的方式定位 search 的位置。
// 返回匹配到的原文区间 (含原始空白); 找不到返回 {-1,-1}。
func fuzzyIndex(text, search string) span {
	norm := func(s string) string {
		lines := strings.Split(s, "\n")
		for i := range lines {
			lines[i] = strings.TrimSpace(lines[i])
		}
		return strings.Join(lines, "\n")
	}
	target := norm(search)
	if target == "" {
		return span{-1, -1}
	}
	// 在 text 的每个换行边界处尝试: 取从该位置起、与 search 行数相同的片段, 规范化后比较。
	lineCount := strings.Count(search, "\n") + 1
	// 构造 text 的行起始偏移
	var offsets []int
	offsets = append(offsets, 0)
	for i, r := range text {
		if r == '\n' {
			offsets = append(offsets, i+1)
		}
	}
	for _, start := range offsets {
		// 找到第 lineCount 行的结束位置
		end := start
		nl := 0
		for end < len(text) && nl < lineCount {
			if text[end] == '\n' {
				nl++
				if nl == lineCount {
					break
				}
			}
			end++
		}
		if nl < lineCount-1 {
			continue // 剩余行数不足
		}
		if norm(text[start:end]) == target {
			return span{start, end}
		}
	}
	return span{-1, -1}
}

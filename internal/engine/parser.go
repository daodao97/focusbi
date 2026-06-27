package engine

import (
	"encoding/json"
	"regexp"
	"strings"
)

// rawBlock 是按 `;` 切分后、宏替换前的一个原始区块。
type rawBlock struct {
	annotations map[string]any            // -- @key=value
	colConfigs  map[string]map[string]any // alias -> 列配置
	colOrder    []string                  // 列别名顺序
	body        string                    // 去掉注解行后的主体 (SQL 或 markdown/raw 内容)
	kind        string                    // sql / markdown / raw
}

var (
	// 整行注解: -- @key=value  或  -- @key
	annotationRe = regexp.MustCompile(`^\s*--\s*@([\w.]+)(?:=(.*))?\s*$`)
	// 列配置:  ... AS alias  -- @{...json...}  或 ... AS alias -- @expr
	colConfigRe = regexp.MustCompile(`(?i)\bAS\s+["` + "`" + `']?([^"` + "`" + `'\r\n,]+?)["` + "`" + `']?\s*,?\s*--\s*@(.+?)\s*$`)
	// 标记区块起始: 行首 (多行模式 ^) 的 #!SCRIPT / #!MARKDOWN / #!RAW。
	// Go 的 RE2 不支持环视, 故用行首锚 + 代码层定位终止边界 (见 splitBlocks)。
	markerStartRe = regexp.MustCompile(`(?m)^[ \t]*#!(?:SCRIPT|MARKDOWN|RAW)\b`)
	// 标记区块结束: 行首的 #!END。
	markerEndRe = regexp.MustCompile(`(?m)^[ \t]*#!END\b`)
)

// splitBlocks 把内容切分为多个原始区块。
// 标记段 (#!SCRIPT/#!MARKDOWN/#!RAW) 没有 `;` 结尾, 若走分号切分会吞掉后面的 SQL;
// 故整段抠出, 不参与分号切分。每个标记段从其 #!XXX 起, 到最近的:
//   - 行首 #!END (含, 显式收尾, 推荐); 或
//   - 下一个标记段起始 (不含, 无 #!END 时的兼容退化); 或
//   - 文末。
//
// 标记段之间 (及之前/之后) 的普通文本仍按 `;` (行尾) 切分。原文顺序保持。
func splitBlocks(content string) []string {
	ranges := markerRanges(content)
	if len(ranges) == 0 {
		return splitBySemicolon(content)
	}

	var blocks []string
	last := 0
	for _, r := range ranges {
		// 标记段之前的普通文本, 按分号切; 标记段整段抠出, 不切。
		blocks = append(blocks, splitBySemicolon(content[last:r[0]])...)
		blocks = append(blocks, content[r[0]:r[1]])
		last = r[1]
	}
	// 尾部剩余普通文本
	blocks = append(blocks, splitBySemicolon(content[last:])...)
	return blocks
}

// markerRanges 返回所有标记段 (#!SCRIPT/#!MARKDOWN/#!RAW ... #!END) 在 content 中的
// 字节区间 [start,end)。每段从其 #!XXX 起, 到最近的: 行首 #!END (含); 或下一个标记段
// 起始 (不含, 无 #!END 的退化); 或文末。
// 既供 splitBlocks 切块, 也供 parseFilters 跳过区块内部 —— 脚本里的 JS template literal
// `${x}` 不应被当成报表过滤器解析。两处共用此函数, 避免边界判定漂移。
func markerRanges(content string) [][2]int {
	starts := markerStartRe.FindAllStringIndex(content, -1)
	if len(starts) == 0 {
		return nil
	}
	ranges := make([][2]int, 0, len(starts))
	for i, s := range starts {
		upper := len(content)
		if i+1 < len(starts) {
			upper = starts[i+1][0]
		}
		end := upper
		if loc := markerEndRe.FindStringIndex(content[s[1]:upper]); loc != nil {
			end = s[1] + loc[1]
		}
		ranges = append(ranges, [2]int{s[0], end})
	}
	return ranges
}

// splitBySemicolon 以分号结尾的行作为区块边界切分 (SQL/markdown 用)。空白片段被丢弃。
func splitBySemicolon(content string) []string {
	var blocks []string
	var cur []string
	flush := func() {
		if s := strings.Join(cur, "\n"); strings.TrimSpace(s) != "" {
			blocks = append(blocks, s)
		}
		cur = nil
	}
	for _, line := range strings.Split(content, "\n") {
		cur = append(cur, line)
		if strings.HasSuffix(strings.TrimSpace(line), ";") {
			flush()
		}
	}
	flush()
	return blocks
}

// parseBlock 解析单个区块: 抽取注解、列配置, 判定类型, 返回主体。
func parseBlock(raw string) *rawBlock {
	b := &rawBlock{
		annotations: map[string]any{},
		colConfigs:  map[string]map[string]any{},
	}

	// 标记区块 (#!SCRIPT/#!MARKDOWN/#!RAW): 整段原样保留, 不做注解/列配置的逐行扫描。
	// 脚本是避免 JS 被误解析; markdown/raw 是避免正文里的 `-- @x` 被当成块注解抽走。
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "#!SCRIPT") {
		// 标记行可带指令, 如 `#!SCRIPT @setup` (前置执行: 宏冻结前 setParam 改写 params)。
		// 抽出指令后正文保持纯 JS, 不污染脚本。
		markerLine, rest := trimmed, ""
		if nl := strings.IndexByte(trimmed, '\n'); nl >= 0 {
			markerLine, rest = trimmed[:nl], trimmed[nl:]
		}
		if strings.Contains(markerLine, "@setup") {
			b.annotations["setup"] = true
		}
		b.body = "#!SCRIPT" + rest
		b.kind = "script"
		return b
	}
	if strings.HasPrefix(trimmed, "#!MARKDOWN") {
		b.body = trimmed
		b.kind = "markdown"
		return b
	}
	if strings.HasPrefix(trimmed, "#!RAW") {
		b.body = trimmed
		b.kind = "raw"
		return b
	}

	var bodyLines []string
	for _, line := range strings.Split(raw, "\n") {
		if m := annotationRe.FindStringSubmatch(line); m != nil {
			key, val := m[1], strings.TrimSpace(m[2])
			b.annotations[key] = decodeAnnotation(val)
			continue
		}
		// 列配置行: 保留行 (去掉注释部分), 同时记录配置
		if m := colConfigRe.FindStringSubmatch(line); m != nil {
			alias := strings.TrimSpace(m[1])
			cfg := decodeColConfig(strings.TrimSpace(m[2]))
			if alias != "" {
				b.colConfigs[alias] = cfg
				b.colOrder = append(b.colOrder, alias)
			}
			// 去掉 -- @... 注释, 保留 SQL 片段
			clean := regexp.MustCompile(`\s*--\s*@.+$`).ReplaceAllString(line, "")
			bodyLines = append(bodyLines, clean)
			continue
		}
		bodyLines = append(bodyLines, line)
	}

	b.body = strings.TrimSpace(strings.Join(bodyLines, "\n"))
	b.kind = detectKind(b.body)
	return b
}

// decodeAnnotation 解码注解值: 尝试 JSON, 失败则返回字符串; 空值视为 true。
func decodeAnnotation(val string) any {
	if val == "" {
		return true
	}
	if val == "true" {
		return true
	}
	if val == "false" {
		return false
	}
	if strings.HasPrefix(val, "{") || strings.HasPrefix(val, "[") {
		var v any
		if err := json.Unmarshal([]byte(val), &v); err == nil {
			return v
		}
	}
	return val
}

// decodeColConfig 解码列配置: JSON 形式返回对象; 否则当作 def 表达式。
func decodeColConfig(val string) map[string]any {
	if strings.HasPrefix(val, "{") {
		var v map[string]any
		if err := json.Unmarshal([]byte(val), &v); err == nil {
			return v
		}
	}
	// 表达式形式 -- @{点击}/{请求}*100 当作 def
	return map[string]any{"def": val}
}

// detectKind 判定区块类型。
func detectKind(body string) string {
	upper := strings.ToUpper(strings.TrimSpace(body))
	switch {
	case strings.HasPrefix(body, "#!SCRIPT"):
		return "script"
	case strings.HasPrefix(body, "#!MARKDOWN"):
		return "markdown"
	case strings.HasPrefix(body, "#!RAW"):
		return "raw"
	case strings.HasPrefix(upper, "SELECT"), strings.HasPrefix(upper, "WITH"):
		return "sql"
	case body == "":
		return "empty"
	default:
		return "markdown"
	}
}

// stripMarker 去掉 #!SCRIPT/#!MARKDOWN/#!RAW 前缀, 并去掉末尾的 #!END 收尾标记 (若有)。
func stripMarker(body string) string {
	for _, marker := range []string{"#!SCRIPT", "#!MARKDOWN", "#!RAW"} {
		if strings.HasPrefix(body, marker) {
			body = body[len(marker):]
			// 去掉末尾的 #!END (显式收尾); 取最后一个, 避免正文里出现 #!END 字样时误截。
			if i := strings.LastIndex(body, "#!END"); i >= 0 {
				body = body[:i]
			}
			return strings.TrimSpace(body)
		}
	}
	return body
}

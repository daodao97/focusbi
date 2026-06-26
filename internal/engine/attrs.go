package engine

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/spf13/cast"
)

// 本文件移植自 dataddy 的行/单元格属性 (attrs) 机制:
//   - 单元格标签: dataddy field_* 插件返回 class/value, 写入 attrs[i][field]
//   - 行级样式:   dataddy plugin_row_tag 写入 attrs[i]['_']
// 这里以列配置 `tag` 与块注解 `row_tag` 两种声明式形式落地, 产出供前端渲染的结构。

// validTagTypes 是 element-plus el-tag 支持的语义类型。
var validTagTypes = map[string]bool{
	"success": true, "warning": true, "danger": true, "info": true, "primary": true,
}

// buildCellAttrs 依据列的 `tag` 配置, 为命中规则的单元格生成标签。
// tag 配置形如 "1:success,0:danger,default:info" (值:类型),
// 或带文本 "1:success:已完成,0:danger:失败" (值:类型:文本)。
// 键以单元格的"原始值"匹配, 因此应在 applyCellTransforms (enum 等) 之前调用。
// 返回 colName -> 行号(字符串) -> *CellAttr; 无任何标签时返回 nil。
func buildCellAttrs(cols []Column, rows []map[string]any) map[string]map[string]*CellAttr {
	var out map[string]map[string]*CellAttr
	for _, col := range cols {
		if col.Config == nil {
			continue
		}
		rules := parseTagConfig(col.Config["tag"])
		if rules == nil {
			continue
		}
		def, hasDef := rules["default"]
		for i, row := range rows {
			v, ok := row[col.Name]
			if !ok || v == nil {
				continue
			}
			attr, matched := rules[cast.ToString(v)]
			if !matched {
				if !hasDef {
					continue
				}
				attr = def
			}
			if out == nil {
				out = map[string]map[string]*CellAttr{}
			}
			if out[col.Name] == nil {
				out[col.Name] = map[string]*CellAttr{}
			}
			a := *attr // 复制, 避免多行共享同一指针
			out[col.Name][strconv.Itoa(i)] = &a
		}
	}
	return out
}

// applyPercentColumns 处理列的 `percent` 配置 (移植自 dataddy field_percent):
// 计算 值 / base * 100, 把单元格改写为 "X%", 并按阈值生成彩色标签写入 attrs。
// 配置形如 {"base":"total","succ":70,"warn":40,"dot":1}:
//   - base: 分母, 字符串表示取同行某列的值, 数值表示常量
//   - succ: 达标阈值 (>= 显示 success 绿色)
//   - warn: 预警阈值 (>= 显示 warning, < 显示 danger; 省略时未达标即 danger)
//   - dot:  小数位数 (默认 0)
//
// 须在 applyCellTransforms 之前调用 (读取原始数值)。attrs 可为 nil, 返回合并后的 map。
func applyPercentColumns(cols []Column, rows []map[string]any, attrs map[string]map[string]*CellAttr) map[string]map[string]*CellAttr {
	for _, col := range cols {
		if col.Config == nil {
			continue
		}
		cfg, ok := col.Config["percent"].(map[string]any)
		if !ok {
			continue
		}
		baseRaw := cfg["base"]
		baseCol, baseIsCol := baseRaw.(string)
		baseConst := cast.ToFloat64(baseRaw)
		succ, hasSucc := numConfig(cfg["succ"])
		warn, hasWarn := numConfig(cfg["warn"])
		dot := cast.ToInt(cfg["dot"])

		for i, row := range rows {
			v, ok := row[col.Name]
			if !ok || v == nil {
				continue
			}
			num, err := toFloat(v)
			if err != nil {
				continue
			}
			base := baseConst
			if baseIsCol {
				bv, err := toFloat(row[baseCol])
				if err != nil {
					continue
				}
				base = bv
			}
			if base == 0 {
				continue
			}
			pct := num / base * 100
			text := formatFloat(pct, dot) + "%"
			row[col.Name] = text

			typ := "info"
			switch {
			case hasSucc && pct >= succ:
				typ = "success"
			case hasWarn && pct >= warn:
				typ = "warning"
			case hasSucc || hasWarn:
				typ = "danger"
			}

			if attrs == nil {
				attrs = map[string]map[string]*CellAttr{}
			}
			if attrs[col.Name] == nil {
				attrs[col.Name] = map[string]*CellAttr{}
			}
			attrs[col.Name][strconv.Itoa(i)] = &CellAttr{Type: typ, Text: text}
		}
	}
	return attrs
}

// parseTagConfig 解析 tag 列配置为 值 -> *CellAttr 的规则表。
// 仅接受字符串形式; 非法/空配置返回 nil。
func parseTagConfig(v any) map[string]*CellAttr {
	s, ok := v.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return nil
	}
	out := map[string]*CellAttr{}
	for _, kv := range strings.Split(s, ",") {
		seg := strings.SplitN(kv, ":", 3)
		if len(seg) < 2 {
			continue
		}
		key := strings.TrimSpace(seg[0])
		typ := strings.TrimSpace(seg[1])
		if key == "" || !validTagTypes[typ] {
			continue
		}
		attr := &CellAttr{Type: typ}
		if len(seg) == 3 {
			attr.Text = strings.TrimSpace(seg[2])
		}
		out[key] = attr
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildRowAttrs 依据块注解 `row_tag` 为命中条件的行生成行级样式。
// row_tag 形如 {"when":"status==1","class":"row-success"}, 或多条规则的数组。
// when 支持 `field op value`, op ∈ == != > >= < <=; 省略 when 表示匹配所有行。
// 多条规则按顺序匹配, 命中后该行的 class 以空格追加。返回 行号(字符串) -> *RowAttr。
func buildRowAttrs(annotation any, rows []map[string]any) map[string]*RowAttr {
	rules := parseRowTagRules(annotation)
	if len(rules) == 0 {
		return nil
	}
	var out map[string]*RowAttr
	for i, row := range rows {
		for _, rule := range rules {
			if rule.class == "" || !rule.match(row) {
				continue
			}
			if out == nil {
				out = map[string]*RowAttr{}
			}
			key := strconv.Itoa(i)
			if ex := out[key]; ex != nil {
				ex.Class = strings.TrimSpace(ex.Class + " " + rule.class)
			} else {
				out[key] = &RowAttr{Class: rule.class}
			}
		}
	}
	return out
}

// rowTagRule 是一条解析后的行样式规则。
type rowTagRule struct {
	field string
	op    string
	value string
	class string
	all   bool // 无 when 条件, 匹配所有行
}

var rowTagOps = []string{"==", "!=", ">=", "<=", ">", "<"}

// parseRowTagRules 把注解 (对象或对象数组) 解析为规则列表。
func parseRowTagRules(annotation any) []rowTagRule {
	objs := normalizeRowTag(annotation)
	var rules []rowTagRule
	for _, obj := range objs {
		class := cast.ToString(obj["class"])
		if class == "" {
			continue
		}
		when := strings.TrimSpace(cast.ToString(obj["when"]))
		if when == "" {
			rules = append(rules, rowTagRule{class: class, all: true})
			continue
		}
		if f, op, val, ok := splitCondition(when); ok {
			rules = append(rules, rowTagRule{field: f, op: op, value: val, class: class})
		}
	}
	return rules
}

// normalizeRowTag 把注解统一为 []map[string]any。
// 注解经 decodeAnnotation 后, 对象为 map[string]any, 数组为 []any。
func normalizeRowTag(annotation any) []map[string]any {
	switch v := annotation.(type) {
	case map[string]any:
		return []map[string]any{v}
	case []any:
		out := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	case string:
		// 兜底: 注解未被识别为 JSON 时, 尝试再解析一次。
		var raw any
		if json.Unmarshal([]byte(v), &raw) == nil {
			return normalizeRowTag(raw)
		}
	}
	return nil
}

// splitCondition 拆分 "field op value" 为三段。
func splitCondition(expr string) (field, op, value string, ok bool) {
	for _, o := range rowTagOps {
		if idx := strings.Index(expr, o); idx > 0 {
			field = strings.TrimSpace(expr[:idx])
			value = strings.TrimSpace(expr[idx+len(o):])
			value = strings.Trim(value, `"'`)
			if field != "" {
				return field, o, value, true
			}
		}
	}
	return "", "", "", false
}

// match 判定某行是否满足规则条件。数值可比则按数值比较, 否则按字符串。
func (r rowTagRule) match(row map[string]any) bool {
	if r.all {
		return true
	}
	cell, ok := row[r.field]
	if !ok || cell == nil {
		return false
	}
	left := cast.ToString(cell)
	lf, lErr := toFloat(cell)
	rf, rErr := strconv.ParseFloat(r.value, 64)
	numeric := lErr == nil && rErr == nil

	switch r.op {
	case "==":
		return left == r.value
	case "!=":
		return left != r.value
	case ">":
		return numeric && lf > rf
	case ">=":
		return numeric && lf >= rf
	case "<":
		return numeric && lf < rf
	case "<=":
		return numeric && lf <= rf
	}
	return false
}

// applyColumnPipeline 对一个 table Block 执行列级处理链, 声明式 SQL 区块与脚本产出的
// table 区块共用:
//   - tag (单元格标签) / row_tag (行样式): 按原始值匹配, 须在单元格转换之前
//   - percent (条件百分比): 改写值并按阈值生成彩色标签
//   - 单元格转换: enum / ratio / round / date / time2str
//   - 合计 / 平均行 (wantSum / wantAvg)
//
// rowTag 为 row_tag 配置 (声明式来自注解, 脚本可来自 spec); 就地修改 block。
func applyColumnPipeline(block *Block, rowTag any, wantSum, wantAvg bool) {
	block.CellAttrs = buildCellAttrs(block.Columns, block.Rows)
	if rowTag != nil {
		block.RowAttrs = buildRowAttrs(rowTag, block.Rows)
	}
	block.CellAttrs = applyPercentColumns(block.Columns, block.Rows, block.CellAttrs)
	applyCellTransforms(block.Columns, block.Rows)
	if wantSum || wantAvg {
		block.Summary, block.Average = computeSummary(block.Columns, block.Rows, wantSum, wantAvg)
	}
}

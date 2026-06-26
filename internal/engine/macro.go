package engine

import (
	"regexp"
	"strings"
)

// macroRe 匹配 {name} / {?name} / {?!name} / {name[raw]} 形式的宏占位。
// 组: 1=条件(? 或 ?!), 2=名称(支持逗号组合), 3=修饰(中括号内)
var macroRe = regexp.MustCompile(`\{(\?!?)?([\w,]+)(?:\[([^\]]*)\])?\}`)

// applyMacros 把内容中的宏占位替换为实际值。
// 对于行尾的条件占位 `-- {?name}`: 若条件不满足则删除该列/行。
// 实现采用逐行处理以支持行级条件删除。
func applyMacros(content string, macros map[string]string) string {
	lines := strings.Split(content, "\n")
	var out []string

	for _, line := range lines {
		processed, drop := processLine(line, macros)
		if drop {
			continue
		}
		out = append(out, processed)
	}
	return strings.Join(out, "\n")
}

// processLine 处理单行的宏替换与行级条件删除。
// 返回处理后的行, 以及是否应整体删除该行。
func processLine(line string, macros map[string]string) (string, bool) {
	// 行尾注释形式的条件: `xxx, -- {?show_income}`
	trailing := regexp.MustCompile(`--\s*(\{[?!\w,\[\]]+\})\s*$`).FindStringSubmatch(line)
	if trailing != nil {
		cond := trailing[1]
		if m := macroRe.FindStringSubmatch(cond); m != nil && m[1] != "" {
			if shouldDrop(m[1], m[2], macros) {
				return "", true
			}
			// 条件满足: 去掉行尾的条件注释, 保留业务内容
			line = regexp.MustCompile(`\s*--\s*\{[?!\w,\[\]]+\}\s*$`).ReplaceAllString(line, "")
			return substitute(line, macros), false
		}
	}

	return substitute(line, macros), false
}

// shouldDrop 判断条件宏是否导致删除。
// `?name`  -> name 为空时删除
// `?!name` -> name 非空时删除
func shouldDrop(cond, name string, macros map[string]string) bool {
	empty := isEmpty(name, macros)
	switch cond {
	case "?":
		return empty
	case "?!":
		return !empty
	}
	return false
}

func isEmpty(name string, macros map[string]string) bool {
	for _, n := range strings.Split(name, ",") {
		v := strings.TrimSpace(macros[strings.TrimSpace(n)])
		if v != "" && v != "0" && strings.ToLower(v) != "false" {
			return false
		}
	}
	return true
}

// substitute 替换普通宏占位 {name} / {name[modifier|format]} / {a,b}。
func substitute(line string, macros map[string]string) string {
	return macroRe.ReplaceAllStringFunc(line, func(token string) string {
		m := macroRe.FindStringSubmatch(token)
		if m == nil {
			return token
		}
		cond, name, mod := m[1], m[2], m[3]
		if cond != "" {
			// 条件占位但不在行尾上下文 -> 直接移除占位本身
			return ""
		}
		// 逗号组合: 取第一个存在的值
		for _, n := range strings.Split(name, ",") {
			if v, ok := macros[strings.TrimSpace(n)]; ok {
				if mod != "" {
					return applyModifier(v, mod)
				}
				return v
			}
		}
		return ""
	})
}

package engine

import (
	"regexp"
	"strings"
	"time"

	"xproxy/internal/datasource"

	"github.com/spf13/cast"
)

// filterRe 匹配 ${name|label|default|type(params)} 形式的过滤器定义。
var filterRe = regexp.MustCompile(`\$\{([^}]*)\}`)

// enumOptRe 匹配 enum 参数 "1:成功,0:失败" 这样的候选项。
var enumOptRe = regexp.MustCompile(`^[^:]+:.+`)

// parseFilters 从 content 中提取所有过滤器定义, 并返回去掉定义行后的内容。
// 过滤器定义独占首部, 形如:
//
//	${date|日期|-7 days,today|date_range}
//	${status|状态|1|enum(1:成功,0:失败)}
func parseFilters(content string) ([]FilterDef, string) {
	var filters []FilterDef
	seen := map[string]bool{}

	matches := filterRe.FindAllStringSubmatch(content, -1)
	for _, m := range matches {
		body := strings.TrimSpace(m[1])
		if body == "" {
			continue
		}
		f := parseFilterBody(body)
		if f.Name == "" || seen[f.Name] {
			continue
		}
		seen[f.Name] = true
		filters = append(filters, f)
	}

	// 移除定义行: 把 ${...} 占位整体删掉 (含可能跟随的 ; 与换行)
	cleaned := filterRe.ReplaceAllString(content, "")
	cleaned = regexp.MustCompile(`(?m)^\s*;\s*$`).ReplaceAllString(cleaned, "")
	return filters, cleaned
}

// parseFilterBody 解析 "name|label|default|type(params)"。
func parseFilterBody(body string) FilterDef {
	parts := strings.SplitN(body, "|", 4)
	f := FilterDef{Type: "string"}
	if len(parts) > 0 {
		f.Name = strings.TrimSpace(parts[0])
	}
	if len(parts) > 1 {
		f.Label = strings.TrimSpace(parts[1])
	}
	if len(parts) > 2 {
		f.Default = strings.TrimSpace(parts[2])
	}
	if len(parts) > 3 {
		typePart := strings.TrimSpace(parts[3])
		// enum_sql[dsn](SELECT value, label FROM ...): 动态选项, 选项由 SQL 查询得到。
		if sql, dsn, multiple, ok := parseEnumSQL(typePart); ok {
			f.Type = "enum"
			f.optionSQL = sql
			f.optionDSN = dsn
			f.Multiple = multiple
		} else {
			f.Type, f.Format, f.Options, f.Multiple = parseFilterType(typePart)
		}
	}
	if f.Label == "" {
		f.Label = f.Name
	}
	return f
}

// enumSQLRe 匹配 enum_sql[.multiple][\[dsn\]](SQL); SQL 可含括号, 取最外层括号内全部。
// 捕获组: 1=.multiple 后缀 2=dsn 3=SQL
var enumSQLRe = regexp.MustCompile(`(?is)^enum_sql((?:\.\w+)*)(?:\[([^\]]*)\])?\s*\((.*)\)\s*$`)

// parseEnumSQL 解析 enum_sql 类型, 返回 (SQL, dsn, 是否多选, 是否匹配)。
func parseEnumSQL(s string) (sql, dsn string, multiple, ok bool) {
	m := enumSQLRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", "", false, false
	}
	multiple = strings.Contains(m[1], "multiple")
	return strings.TrimSpace(m[3]), strings.TrimSpace(m[2]), multiple, true
}

// resolveFilterOptions 为带 optionSQL 的过滤器 (enum_sql) 查库填充 Options。
// 查询取首两列作为 value / label (仅一列则 value=label)。查询出错则该过滤器选项留空, 不崩报表。
func (r *Runner) resolveFilterOptions(filters []FilterDef) {
	for i := range filters {
		f := &filters[i]
		if f.optionSQL == "" {
			continue
		}
		dsn := f.optionDSN
		if dsn == "" {
			dsn = r.defaultDSN
		}
		// 数据源授权: 无权则该过滤器选项留空 (不阻断报表, 与 enum_sql 查询失败一致)。
		if r.authz != nil && r.authz(dsn) != nil {
			continue
		}
		qr, err := datasource.Query(dsn, f.optionSQL)
		if err != nil || qr == nil {
			continue
		}
		f.Options = rowsToOptions(qr.Columns, qr.Rows)
	}
}

// rowsToOptions 把查询结果转成过滤器选项。
// 优先用名为 value/label 的列; 否则取列顺序的前两列 (单列则 value=label)。
func rowsToOptions(cols []string, rows []map[string]any) []EnumOpt {
	if len(cols) == 0 {
		return nil
	}
	valueCol, labelCol := cols[0], cols[0]
	if len(cols) >= 2 {
		labelCol = cols[1]
	}
	for _, c := range cols { // 显式命名优先
		switch strings.ToLower(c) {
		case "value":
			valueCol = c
		case "label":
			labelCol = c
		}
	}
	opts := make([]EnumOpt, 0, len(rows))
	for _, row := range rows {
		v := cast.ToString(row[valueCol])
		l := cast.ToString(row[labelCol])
		if l == "" {
			l = v
		}
		opts = append(opts, EnumOpt{Value: v, Label: l})
	}
	return opts
}

// parseFilterType 解析 "enum(1:成功,0:失败)" -> ("enum", "", options, false)。
// 支持尾部 [format] 指定日期格式, 如 "date_range[Y-m]" -> ("date_range", "Y-m", nil, false)。
// `.multiple` 后缀 (如 enum.multiple) 标记多选; 其余 dataddy 修饰后缀 (.macro/.raw 等) 被忽略。
func parseFilterType(s string) (typ, format string, opts []EnumOpt, multiple bool) {
	// 尾部 [format]: date_range[Y-m] / date[Y-m-d]
	if i := strings.LastIndex(s, "["); i >= 0 && strings.HasSuffix(s, "]") {
		format = strings.TrimSpace(s[i+1 : len(s)-1])
		s = strings.TrimSpace(s[:i])
	}

	var params string
	if i := strings.Index(s, "("); i >= 0 && strings.HasSuffix(s, ")") {
		params = s[i+1 : len(s)-1]
		s = s[:i]
	}

	// date.month / enum.multiple / string.macro.raw -> 第一段为基础类型, 后缀里识别 multiple
	base := s
	if i := strings.Index(s, "."); i >= 0 {
		base = s[:i]
		for _, suf := range strings.Split(s[i+1:], ".") {
			if strings.TrimSpace(suf) == "multiple" {
				multiple = true
			}
		}
	}
	base = strings.TrimSpace(base)

	if base == "enum" && params != "" {
		for _, kv := range strings.Split(params, ",") {
			kv = strings.TrimSpace(kv)
			if !enumOptRe.MatchString(kv) {
				continue
			}
			seg := strings.SplitN(kv, ":", 2)
			opts = append(opts, EnumOpt{Value: strings.TrimSpace(seg[0]), Label: strings.TrimSpace(seg[1])})
		}
	}

	switch base {
	case "date", "time", "number", "string", "enum", "bool", "date_range", "time_range":
		return base, format, opts, multiple
	default:
		return "string", format, opts, multiple
	}
}

// macroValues 依据过滤器定义与请求参数, 计算所有可用的宏值。
// date_range 类型会展开为 {from_<name>} 与 {to_<name>} 两个宏。
func macroValues(filters []FilterDef, params map[string]string) map[string]string {
	macros := map[string]string{}

	for _, f := range filters {
		val, ok := params[f.Name]
		if !ok || strings.TrimSpace(val) == "" {
			val = defaultValue(f)
		}

		switch {
		case f.Type == "date_range" || f.Type == "time_range":
			from, to := splitRange(val, f.Type)
			macros["from_"+f.Name] = from
			macros["to_"+f.Name] = to
			macros[f.Name] = val
		case f.Type == "enum" && f.Multiple:
			// 多选: {name} 展开为 SQL in-list ('a','b','c'), 作者写 WHERE x IN ({name})。
			// 同时给 {name_raw} = 原始逗号串, 供需要原值的场景。
			macros[f.Name] = sqlInList(val)
			macros[f.Name+"_raw"] = val
		default:
			macros[f.Name] = val
		}
	}
	return macros
}

// sqlInList 把逗号分隔的多选值转成 SQL in-list: "a,b" -> "'a','b'"。
// 单引号转义 (''), 防止经宏拼接产生注入。空值返回 '' (使 IN ('') 不命中而非语法错)。
func sqlInList(val string) string {
	parts := strings.Split(val, ",")
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		quoted = append(quoted, "'"+strings.ReplaceAll(p, "'", "''")+"'")
	}
	if len(quoted) == 0 {
		return "''"
	}
	return strings.Join(quoted, ",")
}

// defaultValue 计算过滤器在无请求参数时的默认值, 解析相对日期。
// 若过滤器带 Format (PHP 风格, 如 Y-m-01), 则按该格式输出 —— 注意用 formatDate 直接
// 拼接 (而非 Go time 布局), 这样 "Y-m-01" 中的 "01" 会作为字面量保留 (当月第一天)。
func defaultValue(f FilterDef) string {
	def := f.Default
	switch f.Type {
	case "date_range", "time_range":
		seg := strings.SplitN(def, ",", 2)
		fromExpr, toExpr := "today", "today"
		if len(seg) > 0 {
			fromExpr = strings.TrimSpace(seg[0])
		}
		if len(seg) > 1 {
			toExpr = strings.TrimSpace(seg[1])
		}
		return resolveDateExpr(f, fromExpr) + "," + resolveDateExpr(f, toExpr)
	case "date", "time":
		if def == "" {
			def = "today"
		}
		return resolveDateExpr(f, def)
	default:
		return def
	}
}

// resolveDateExpr 把相对日期表达式解析为时间, 再按过滤器格式输出字符串。
func resolveDateExpr(f FilterDef, expr string) string {
	t, ok := resolveRelativeTime(expr)
	if !ok {
		// 非相对表达式 (已是绝对值), 原样返回
		return expr
	}
	if fm := strings.TrimSpace(f.Format); fm != "" {
		return formatDate(t, fm) // 字面量安全, "01" 保持为 01
	}
	layout := "2006-01-02"
	if f.Type == "time" || f.Type == "time_range" {
		layout = "2006-01-02 15:04:05"
	}
	return t.Format(layout)
}

// resolveDefaults 为每个过滤器填充 Resolved (解析后的默认值), 供前端回填输入控件。
// 相对日期如 "-7 days,today" 会被解析为具体日期, enum/bool/string 等保持原默认值。
func resolveDefaults(filters []FilterDef) {
	for i := range filters {
		filters[i].Resolved = defaultValue(filters[i])
	}
}

func splitRange(val, typ string) (string, string) {
	seg := strings.SplitN(val, ",", 2)
	if len(seg) == 2 {
		return strings.TrimSpace(seg[0]), strings.TrimSpace(seg[1])
	}
	if len(seg) == 1 {
		return strings.TrimSpace(seg[0]), strings.TrimSpace(seg[0])
	}
	return "", ""
}

func today(layout string) string {
	return time.Now().Format(layout)
}

// resolveRelativeTime 解析简单的相对日期表达式为 time.Time。
// 识别: today/now / yesterday / this month / ±N day(s) / ±N week(s) / ±N month(s) / ±N year(s)。
// 第二返回值为 false 表示 expr 不是已知的相对表达式 (调用方应原样保留)。
func resolveRelativeTime(expr string) (time.Time, bool) {
	e := strings.TrimSpace(strings.ToLower(expr))
	now := time.Now()

	switch e {
	case "", "today", "now":
		return now, true
	case "yesterday":
		return now.AddDate(0, 0, -1), true
	case "tomorrow":
		return now.AddDate(0, 0, 1), true
	case "this month":
		return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()), true
	}

	if m := regexp.MustCompile(`^([+-]?\d+)\s*(day|days|week|weeks|month|months|year|years)$`).FindStringSubmatch(e); m != nil {
		n := atoi(m[1])
		switch m[2] {
		case "day", "days":
			return now.AddDate(0, 0, n), true
		case "week", "weeks":
			return now.AddDate(0, 0, n*7), true
		case "month", "months":
			return now.AddDate(0, n, 0), true
		case "year", "years":
			return now.AddDate(n, 0, 0), true
		}
	}

	return time.Time{}, false
}

func atoi(s string) int {
	neg := false
	n := 0
	for _, r := range s {
		switch {
		case r == '-':
			neg = true
		case r == '+':
		case r >= '0' && r <= '9':
			n = n*10 + int(r-'0')
		}
	}
	if neg {
		return -n
	}
	return n
}

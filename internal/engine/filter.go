package engine

import (
	"regexp"
	"strconv"
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

	// 标记段 (#!SCRIPT/#!MARKDOWN/#!RAW) 内部不解析过滤器: 脚本里的 JS template literal
	// `${table}` 不是报表过滤器, 不能被提取或删除。只扫描标记段之外的内容。
	skip := markerRanges(content)
	inSkip := func(pos int) bool {
		for _, r := range skip {
			if pos >= r[0] && pos < r[1] {
				return true
			}
		}
		return false
	}

	// 一次性扫描: 标记段外的 ${...} 既提取为过滤器, 又从 cleaned 中删除; 标记段内原样保留。
	var b strings.Builder
	prev := 0
	for _, loc := range filterRe.FindAllStringSubmatchIndex(content, -1) {
		if inSkip(loc[0]) {
			continue // 标记段内: 不提取也不删除
		}
		b.WriteString(content[prev:loc[0]]) // 保留占位前文本, 跳过 ${...} 本身
		prev = loc[1]

		body := strings.TrimSpace(content[loc[2]:loc[3]])
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
	b.WriteString(content[prev:])

	cleaned := regexp.MustCompile(`(?m)^\s*;\s*$`).ReplaceAllString(b.String(), "")
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
		// 只读契约: enum_sql 与声明式区块/脚本 query() 一样只允许只读查询, 非法则跳过 (选项留空)。
		if err := validateReadOnlySQL(f.optionSQL); err != nil {
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
//
// 安全: 宏值被字面拼进 SQL (作者按 SYNTAX §8 写 '{name}' 自己加引号), 故这里对所有
// 用户可控的字符串值做单引号转义 (' -> ”), 数值/日期型强制类型校验。攻击者传入的
// x' UNION SELECT ... 会被转义成字面量, 无法越出引号。需要原始未转义值的场景用 {name_raw}。
func macroValues(filters []FilterDef, params map[string]string) map[string]string {
	macros := map[string]string{}

	// 先收下所有 params: 非过滤器的派生值 (如 @setup 脚本 setParam 写入的 is_current)
	// 也能作为 {name} 宏使用。@setup 派生值来自可信脚本, 但请求 params 也会落这里,
	// 故未声明为过滤器的值同样转义, 防止注入未声明的裸参数名。下方过滤器循环覆盖同名项。
	for k, v := range params {
		macros[k] = sqlEscape(v)
		macros[k+"_raw"] = v
	}

	for _, f := range filters {
		val, ok := params[f.Name]
		if !ok || strings.TrimSpace(val) == "" {
			val = defaultValue(f)
		}
		macros[f.Name+"_raw"] = val

		switch {
		case f.Type == "date_range" || f.Type == "time_range":
			// 日期/时间强制格式校验: 非法值置空。作者可裸写 WHERE day >= {from_x},
			// 攻击者传 "2026-01-01 OR 1=1" 会被整体置空, 无法注入。
			from, to := splitRange(val, f.Type)
			macros["from_"+f.Name] = sqlDate(from)
			macros["to_"+f.Name] = sqlDate(to)
			macros[f.Name] = sqlDateRange(val) // 合并值也校验 (含逗号分隔)
		case f.Type == "date" || f.Type == "time":
			macros[f.Name] = sqlDate(val)
		case f.Type == "enum" && f.Multiple:
			// 多选: {name} 展开为 SQL in-list ('a','b','c'), 作者写 WHERE x IN ({name})。
			// 同时给 {name_raw} = 原始逗号串, 供需要原值的场景。
			macros[f.Name] = sqlInList(val)
			macros[f.Name+"_raw"] = val
		case f.Type == "number":
			// 数值型强制校验: 非合法数字则置空 (作者可用 -- {?name} 行条件跳过)。
			macros[f.Name] = sqlNumber(val)
		default:
			macros[f.Name] = sqlEscape(val)
		}
	}
	return macros
}

// sqlEscape 对宏值做 SQL 转义, 使其嵌入 '{name}' 后无法越出字面量。
// 作者按 SYNTAX 约定自行在 SQL 里写引号 (WHERE x = '{name}'), 本函数只保证引号内安全。
//
// 必须先转义反斜杠再转义单引号: MySQL 默认模式 (NO_BACKSLASH_ESCAPES 关闭) 下 \ 是转义符,
// 值 `a\' OR 1=1 -- ` 若只把 ' 翻倍会得到 'a\” ...', 其中 \' 被当作转义引号、后一个 ' 才闭合
// 字符串, 使 OR 1=1 逃逸。先 \ -> \\ 再 ' -> ” 可堵死此路径。PG/SQLite 标准模式下 \ 非转义符,
// 多一个 \\ 只是多一个字面反斜杠, 不影响正确性 (真需原值用 {name_raw})。
func sqlEscape(v string) string {
	v = strings.ReplaceAll(v, `\`, `\\`)
	return strings.ReplaceAll(v, "'", "''")
}

// sqlNumber 校验并规整数值型宏值: 合法整数/浮点原样返回, 否则返回空串。
// 空串配合作者的 -- {?name} 行条件可自然跳过该条件; 无行条件时 = 或 IN 处会因空值不命中。
func sqlNumber(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if _, err := strconv.ParseFloat(v, 64); err == nil {
		return v
	}
	return ""
}

// dateValueRe 匹配常见日期/时间字面量: 年必填, 月、日各自可选 (支持按年 2026 / 按月
// 2026-05 / 完整 2026-05-01 三种粒度, 见 SYNTAX date[Y]/date_range[Y-m]), 后可跟
// 可选的 时:分[:秒[.纳秒]] 与时区。锚定 ^...$, 故 "2026-05-01 OR 1=1" / "-- " / "1+1"
// 这类注入或注释片段整体不匹配。
var dateValueRe = regexp.MustCompile(`^\d{4}(?:[-/.]\d{1,2}){0,2}(?:[ T]\d{1,2}:\d{2}(?::\d{2}(?:\.\d{1,9})?)?)?(?:Z|[+-]\d{2}:?\d{2})?$`)

// sqlDate 校验日期/时间型宏值: 只接受上面 dateValueRe 认可的日期/时间字面量, 非法则置空。
// 不能仅做字符白名单, 否则 "-- " 这类注释片段或 "1+1" 表达式仍可出现在裸宏位置。
// 作者可裸写 WHERE day >= {day}; 非法输入置空后配合 -- {?day} 行条件可跳过, 无行条件则不命中。
func sqlDate(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if dateValueRe.MatchString(v) {
		return v
	}
	return ""
}

func sqlDateRange(v string) string {
	from, to := splitRange(v, "")
	from = sqlDate(from)
	to = sqlDate(to)
	if from == "" || to == "" {
		return ""
	}
	return from + "," + to
}

// sqlInList 把逗号分隔的多选值转成 SQL in-list: "a,b" -> "'a','b'"。
// 单引号转义 (”), 防止经宏拼接产生注入。空值返回 ” (使 IN (”) 不命中而非语法错)。
func sqlInList(val string) string {
	parts := strings.Split(val, ",")
	quoted := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		quoted = append(quoted, "'"+sqlEscape(p)+"'")
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

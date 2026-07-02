package engine

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// applyModifier 处理宏占位的 [...] 修饰部分, 对日期类型的值进行调整与格式化。
//
// 语法 (借鉴 dataddy):
//
//	{name[format]}            仅格式化, 如 {month[Y-m-01]} -> 2026-06-01
//	{name[modifier|format]}   先调整再格式化, 如 {d[first_day_of_month|Y-m-d]}
//	{name[modifier]}          仅调整, 用默认 Y-m-d 输出
//	{name[raw]}               原样返回 (兼容旧语义)
//
// 格式 token (PHP date 风格): Y=4位年 y=2位年 m=2位月 n=月 d=2位日 j=日
//
//	H=时 i=分 s=秒; 其余字符原样输出 (故 "01" 中的 0、1 为字面量)。
//
// modifier 可用逗号串联, 取值:
//
//	first_day_of_month / month_start, last_day_of_month / month_end,
//	first_day_of_year / year_start,  last_day_of_year / year_end,
//	+N day(s) / -N day(s) / +N week(s) / +N month(s) / +N year(s)
//
// 若 value 不是可识别的日期, 原样返回。
func applyModifier(value, mod string) string {
	mod = strings.TrimSpace(mod)
	if mod == "" || mod == "raw" {
		return value
	}

	var modifier, format string
	if i := strings.Index(mod, "|"); i >= 0 {
		modifier = strings.TrimSpace(mod[:i])
		format = strings.TrimSpace(mod[i+1:])
	} else if isDateModifier(mod) {
		modifier = mod
	} else {
		format = mod
	}

	t, ok := parseDateValue(value)
	if !ok {
		return value
	}

	if modifier != "" {
		for _, m := range strings.Split(modifier, ",") {
			t = applyDateModifier(t, strings.TrimSpace(m))
		}
	}
	if format == "" {
		format = "Y-m-d"
	}
	return formatDate(t, format)
}

var dateLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02",
	"2006/01/02",
	"2006-01",
}

// parseDateValue 尝试把字符串解析为时间。
func parseDateValue(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range dateLayouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

var relativeModRe = regexp.MustCompile(`^([+-]?\d+)\s*(day|days|week|weeks|month|months|year|years)$`)

// isDateModifier 判断一段文本是否为已知的日期调整指令 (而非格式串)。
func isDateModifier(s string) bool {
	switch normalizeMod(s) {
	case "first_day_of_month", "last_day_of_month", "first_day_of_year", "last_day_of_year":
		return true
	}
	return relativeModRe.MatchString(strings.ToLower(strings.TrimSpace(s)))
}

func normalizeMod(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "first_day_of_month", "month_start", "start_of_month":
		return "first_day_of_month"
	case "last_day_of_month", "month_end", "end_of_month":
		return "last_day_of_month"
	case "first_day_of_year", "year_start", "start_of_year":
		return "first_day_of_year"
	case "last_day_of_year", "year_end", "end_of_year":
		return "last_day_of_year"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// applyDateModifier 对时间应用单个调整指令。
func applyDateModifier(t time.Time, mod string) time.Time {
	switch normalizeMod(mod) {
	case "first_day_of_month":
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location())
	case "last_day_of_month":
		return time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, t.Location()).AddDate(0, 0, -1)
	case "first_day_of_year":
		return time.Date(t.Year(), 1, 1, 0, 0, 0, 0, t.Location())
	case "last_day_of_year":
		return time.Date(t.Year(), 12, 31, 0, 0, 0, 0, t.Location())
	}

	if m := relativeModRe.FindStringSubmatch(strings.ToLower(strings.TrimSpace(mod))); m != nil {
		n, _ := strconv.Atoi(m[1])
		switch m[2] {
		case "day", "days":
			return t.AddDate(0, 0, n)
		case "week", "weeks":
			return t.AddDate(0, 0, n*7)
		case "month", "months":
			return t.AddDate(0, n, 0)
		case "year", "years":
			return t.AddDate(n, 0, 0)
		}
	}
	return t
}

// formatDate 按 PHP date 风格 token 直接拼接输出 (避免 Go 参考时间布局冲突)。
func formatDate(t time.Time, format string) string {
	var sb strings.Builder
	for _, r := range format {
		switch r {
		case 'Y':
			sb.WriteString(strconv.Itoa(t.Year()))
		case 'y':
			sb.WriteString(fmt.Sprintf("%02d", t.Year()%100))
		case 'm':
			sb.WriteString(fmt.Sprintf("%02d", int(t.Month())))
		case 'n':
			sb.WriteString(strconv.Itoa(int(t.Month())))
		case 'd':
			sb.WriteString(fmt.Sprintf("%02d", t.Day()))
		case 'j':
			sb.WriteString(strconv.Itoa(t.Day()))
		case 'H':
			sb.WriteString(fmt.Sprintf("%02d", t.Hour()))
		case 'i':
			sb.WriteString(fmt.Sprintf("%02d", t.Minute()))
		case 's':
			sb.WriteString(fmt.Sprintf("%02d", t.Second()))
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

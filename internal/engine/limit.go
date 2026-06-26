package engine

import (
	"regexp"
	"strings"
)

// limitRe 检测 SQL 末尾是否已有 LIMIT 子句 (允许尾部分号/空白)。
var limitRe = regexp.MustCompile(`(?is)\blimit\s+\d+(\s*,\s*\d+)?\s*;?\s*$`)

// injectLimit 为没有 LIMIT 的 SELECT/WITH 查询补上 LIMIT n。
// n<=0 表示不限制; 已含 LIMIT、或非查询语句, 均原样返回。
func injectLimit(sql string, n int) string {
	if n <= 0 {
		return sql
	}
	trimmed := strings.TrimSpace(sql)
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return sql
	}
	if limitRe.MatchString(trimmed) {
		return sql
	}

	// 去掉尾部分号后追加 LIMIT
	body := strings.TrimRight(trimmed, "; \t\r\n")
	return body + "\nLIMIT " + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

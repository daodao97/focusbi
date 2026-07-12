package engine

import (
	"regexp"
	"strings"
)

const (
	// reportQueryHardLimit 是声明式 SQL 区块的系统硬上限。即使作者使用
	// @limit=0 或在 SQL 中写了更大的 LIMIT, 最终结果也不会超过该值。
	reportQueryHardLimit = 100000
	// enumSQLHardLimit 防止动态下拉一次加载过多选项。
	enumSQLHardLimit = 5000
	// scriptQueryHardLimit 防止脚本 query() 把超大结果集全部装入内存。
	scriptQueryHardLimit = 10000
)

// limitRe 检测 SQL 末尾是否已有 LIMIT 子句 (允许尾部分号/空白)。
var limitRe = regexp.MustCompile(`(?is)\blimit\s+(?:all|\d+)(?:\s*,\s*\d+|\s+offset\s+\d+)?\s*;?\s*$`)

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

// enforceHardLimit 用外层查询强制结果集硬上限。外层 LIMIT 不受原 SQL 自带的
// LIMIT/OFFSET 影响；调用前应已完成只读 SQL 校验。
func enforceHardLimit(sql string, n int) string {
	body := strings.TrimRight(strings.TrimSpace(sql), "; \t\r\n")
	return "SELECT * FROM (\n" + body + "\n) AS focusbi_limited\nLIMIT " + itoa(n)
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

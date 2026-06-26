package engine

import (
	"fmt"
	"strings"
	"unicode"
)

var forbiddenSQLTokens = map[string]bool{
	"insert": true, "update": true, "delete": true, "merge": true, "replace": true,
	"drop": true, "alter": true, "create": true, "truncate": true,
	"grant": true, "revoke": true, "call": true, "execute": true, "exec": true,
}

// validateReadOnlySQL enforces the report SQL contract: a block/query may only
// execute one SELECT/WITH statement. The tokenizer skips comments and quoted
// strings so semicolons or keywords inside text literals do not trigger false
// positives.
func validateReadOnlySQL(sql string) error {
	tokens, semis := sqlTokens(sql)
	if len(tokens) == 0 {
		return fmt.Errorf("SQL 不能为空")
	}
	first := strings.ToLower(tokens[0])
	if first != "select" && first != "with" {
		return fmt.Errorf("报表 SQL 仅支持只读 SELECT/WITH 查询")
	}
	if semis > 1 || (semis == 1 && !hasOnlyTrailingSemicolon(sql)) {
		return fmt.Errorf("报表 SQL 不支持多条语句")
	}
	for _, tok := range tokens {
		if forbiddenSQLTokens[strings.ToLower(tok)] {
			return fmt.Errorf("报表 SQL 仅支持只读查询, 不允许使用 %s", strings.ToUpper(tok))
		}
	}
	return nil
}

func sqlTokens(sql string) (tokens []string, semicolons int) {
	for i := 0; i < len(sql); {
		c := sql[i]
		switch {
		case isSQLSpace(c):
			i++
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			i += 2
			for i < len(sql) && sql[i] != '\n' && sql[i] != '\r' {
				i++
			}
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			i += 2
			for i+1 < len(sql) && !(sql[i] == '*' && sql[i+1] == '/') {
				i++
			}
			if i+1 < len(sql) {
				i += 2
			}
		case c == '\'':
			i = skipSQLQuoted(sql, i, '\'')
		case c == '"':
			i = skipSQLQuoted(sql, i, '"')
		case c == '`':
			i = skipSQLQuoted(sql, i, '`')
		case c == '[':
			i++
			for i < len(sql) && sql[i] != ']' {
				i++
			}
			if i < len(sql) {
				i++
			}
		case c == ';':
			semicolons++
			i++
		case isSQLIdentStart(c):
			start := i
			i++
			for i < len(sql) && isSQLIdentPart(sql[i]) {
				i++
			}
			tokens = append(tokens, sql[start:i])
		default:
			i++
		}
	}
	return tokens, semicolons
}

func skipSQLQuoted(sql string, i int, quote byte) int {
	i++
	for i < len(sql) {
		if sql[i] == '\\' && i+1 < len(sql) {
			i += 2
			continue
		}
		if sql[i] == quote {
			if i+1 < len(sql) && sql[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return i
}

func hasOnlyTrailingSemicolon(sql string) bool {
	for i := len(sql) - 1; i >= 0; i-- {
		switch sql[i] {
		case ' ', '\t', '\n', '\r':
			continue
		case ';':
			return true
		default:
			return false
		}
	}
	return false
}

func isSQLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f'
}

func isSQLIdentStart(c byte) bool {
	return c == '_' || unicode.IsLetter(rune(c))
}

func isSQLIdentPart(c byte) bool {
	return c == '_' || c == '$' || unicode.IsLetter(rune(c)) || unicode.IsDigit(rune(c))
}

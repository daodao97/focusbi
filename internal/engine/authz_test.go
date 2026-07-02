package engine

import (
	"fmt"
	"strings"
	"testing"
)

// denyExcept 返回一个只允许指定 dsn 名的授权闭包。
func denyExcept(allowed ...string) func(string) error {
	set := map[string]bool{}
	for _, a := range allowed {
		set[a] = true
	}
	return func(dsn string) error {
		if set[dsn] {
			return nil
		}
		return fmt.Errorf("无权使用数据源 %q", dsn)
	}
}

func TestAuthzBlocksUnauthorizedDSN(t *testing.T) {
	setupSQLiteDefault(t)
	// 默认 dsn=default 被授权, 但块用 @dsn=secret 覆盖 -> 应被拦 (报错块, 不查库)。
	content := "" +
		"-- @dsn=secret\n" +
		"SELECT day, pv FROM pv;\n"
	res, err := NewRunner("default").WithAuthz(denyExcept("default")).Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 1 {
		t.Fatalf("应有 1 个区块, got %d", len(res.Blocks))
	}
	if !strings.Contains(res.Blocks[0].Error, "secret") {
		t.Fatalf("应报无权使用 secret, got error=%q", res.Blocks[0].Error)
	}
}

func TestAuthzAllowsAuthorizedDSN(t *testing.T) {
	setupSQLiteDefault(t)
	content := "SELECT day, pv FROM pv ORDER BY day;\n"
	res, err := NewRunner("default").WithAuthz(denyExcept("default")).Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Blocks[0].Error != "" {
		t.Fatalf("default 已授权, 不应报错: %q", res.Blocks[0].Error)
	}
	if len(res.Blocks[0].Rows) != 2 {
		t.Fatalf("应返回 2 行, got %d", len(res.Blocks[0].Rows))
	}
}

func TestAuthzNilKeepsLegacyBehavior(t *testing.T) {
	setupSQLiteDefault(t)
	// authz=nil: 调用方明确选择预授权执行时, Runner 本身不再做数据源校验。
	content := "SELECT day, pv FROM pv;\n"
	res, err := NewRunner("default").Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Blocks[0].Error != "" {
		t.Fatalf("authz=nil 不应校验, got error=%q", res.Blocks[0].Error)
	}
}

func TestAuthzBlocksScriptQuery(t *testing.T) {
	setupSQLiteDefault(t)
	// 脚本里 query 指定未授权 dsn -> 抛错被脚本错误处理捕获为 Error 区块。
	content := "#!SCRIPT\n" +
		"const rows = query('SELECT day, pv FROM pv', [], 'secret');\n" +
		"result.table('x', rows);\n" +
		"#!END\n"
	res, err := NewRunner("default").WithAuthz(denyExcept("default")).Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// 至少有一个区块带 error 且提及 secret。
	var found bool
	for _, b := range res.Blocks {
		if strings.Contains(b.Error, "secret") {
			found = true
		}
	}
	if !found {
		t.Fatalf("脚本越权 query 应产出 secret 错误, blocks=%+v", res.Blocks)
	}
}

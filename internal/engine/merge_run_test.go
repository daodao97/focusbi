package engine

import (
	"testing"

	"xproxy/conf"
	"xproxy/internal/datasource"

	"github.com/daodao97/xgo/xdb"
)

// setupSQLiteDefault 把 default 数据源指向一个共享内存 SQLite, 并建好测试表。
// 返回后即可让 engine.Run 执行真实 SQL (走 datasource.Query)。
func setupSQLiteDefault(t *testing.T) {
	t.Helper()
	conf.ConfInstance = &conf.Conf{
		Database: []xdb.Config{{
			Name:   "default",
			Driver: "sqlite",
			DSN:    "file:enginemerge?mode=memory&cache=shared",
		}},
	}
	// 建表 + 造数据 (用一条返回结果的查询确保连接建立)。
	stmts := []string{
		`DROP TABLE IF EXISTS pv`,
		`DROP TABLE IF EXISTS orders`,
		`CREATE TABLE pv(day TEXT, pv INTEGER)`,
		`CREATE TABLE orders(day TEXT, orders INTEGER)`,
		`INSERT INTO pv VALUES('2026-06-23',100),('2026-06-24',300)`,
		`INSERT INTO orders VALUES('2026-06-23',5),('2026-06-24',9)`,
	}
	for _, s := range stmts {
		if _, err := datasource.Query("default", s); err != nil {
			t.Fatalf("setup SQL %q: %v", s, err)
		}
	}
}

func TestRunJoinMergesBlocks(t *testing.T) {
	setupSQLiteDefault(t)
	// 两块 SQL, 第二块 @join=day -> 合并成一个宽表 (day, pv, orders)。
	content := "" +
		"SELECT day, pv FROM pv ORDER BY day;\n" +
		"-- @join=day\n" +
		"SELECT day, orders FROM orders ORDER BY day;\n"

	res, err := NewRunner("default").Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 1 {
		t.Fatalf("@join 应合并为 1 个区块, got %d: %+v", len(res.Blocks), res.Blocks)
	}
	b := res.Blocks[0]
	if b.Error != "" {
		t.Fatalf("block error: %s", b.Error)
	}
	if !hasColumn(b, "pv") || !hasColumn(b, "orders") {
		t.Fatalf("合并块应含 pv 与 orders 列, got %+v", b.Columns)
	}
	if len(b.Rows) != 2 {
		t.Fatalf("应有 2 行, got %d", len(b.Rows))
	}
}

func TestRunUnionMergesBlocks(t *testing.T) {
	setupSQLiteDefault(t)
	// 两块 SQL, 第二块 @union -> 纵向并入 (4 行)。列名一致 (day,n)。
	content := "" +
		"SELECT day AS day, pv AS n FROM pv;\n" +
		"-- @union\n" +
		"SELECT day AS day, orders AS n FROM orders;\n"

	res, err := NewRunner("default").Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 1 {
		t.Fatalf("@union 应合并为 1 个区块, got %d", len(res.Blocks))
	}
	if len(res.Blocks[0].Rows) != 4 {
		t.Fatalf("union 应有 4 行, got %d", len(res.Blocks[0].Rows))
	}
}

func TestRunMergeThenPlainBlock(t *testing.T) {
	setupSQLiteDefault(t)
	// join 组之后接一个普通块 -> 共 2 个区块 (合并块 + 普通块)。
	content := "" +
		"SELECT day, pv FROM pv;\n" +
		"-- @join=day\n" +
		"SELECT day, orders FROM orders;\n" +
		"SELECT day, pv FROM pv;\n"

	res, err := NewRunner("default").Run(content, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Blocks) != 2 {
		t.Fatalf("应为 2 个区块 (合并块 + 普通块), got %d", len(res.Blocks))
	}
}

func hasColumn(b Block, name string) bool {
	for _, c := range b.Columns {
		if c.Name == name {
			return true
		}
	}
	return false
}

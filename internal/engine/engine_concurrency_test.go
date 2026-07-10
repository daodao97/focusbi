package engine

import (
	"encoding/json"
	"testing"

	"xproxy/conf"
	"xproxy/internal/datasource"
	"xproxy/internal/runtimecfg"

	"github.com/daodao97/xgo/xdb"
)

// setupSQLiteConcurrency 把 default 源指向共享内存 SQLite, 建多张表供并发等价性测试。
func setupSQLiteConcurrency(t *testing.T) {
	t.Helper()
	conf.ConfInstance = &conf.Conf{
		Database: []xdb.Config{{
			Name:   "default",
			Driver: "sqlite",
			DSN:    "file:engineconc?mode=memory&cache=shared",
		}},
	}
	stmts := []string{
		`DROP TABLE IF EXISTS pv`,
		`DROP TABLE IF EXISTS orders`,
		`DROP TABLE IF EXISTS daily`,
		`CREATE TABLE pv(day TEXT, pv INTEGER)`,
		`CREATE TABLE orders(day TEXT, orders INTEGER)`,
		`CREATE TABLE daily(stat_date TEXT, consume INTEGER)`,
		`INSERT INTO pv VALUES('2026-06-23',100),('2026-06-24',300)`,
		`INSERT INTO orders VALUES('2026-06-23',5),('2026-06-24',9)`,
		`INSERT INTO daily VALUES('2026-06-23',100),('2026-06-24',300)`,
	}
	for _, s := range stmts {
		if _, err := datasource.Query("default", s); err != nil {
			t.Fatalf("setup SQL %q: %v", s, err)
		}
	}
}

// 一个混排模板: 普通 SQL、@join 合并组、@union、markdown、波动检测、脚本引用前置块。
// 覆盖所有顺序/依赖约束, 用于断言并发与串行产出完全一致。
const mixedTemplate = `
#!MARKDOWN
# 概览
#!END

-- @id=pv_trend
SELECT day, pv FROM pv ORDER BY day;

-- @id=overview
SELECT day, pv FROM pv ORDER BY day;
-- @join=day
SELECT day, orders FROM orders ORDER BY day;

-- @id=channel_union
SELECT day AS day, pv AS n FROM pv;
-- @union
SELECT day AS day, orders AS n FROM orders;

-- @id=fluctuation
-- @data_fluctuations={"field":"consume","threshold_percent":50}
SELECT stat_date, consume FROM daily ORDER BY stat_date DESC;

#!SCRIPT
const rows = dataset('pv_trend')
result.table({ id: 'from_script', title: '脚本复用',
  columns: [{name:'day',header:'day'},{name:'pv',header:'pv'}], rows })
#!END
`

// 把结果序列化为稳定 JSON, 便于逐字节比较 (块序/消息序/内容全覆盖)。
func resultJSON(t *testing.T, r *Result) string {
	t.Helper()
	blocks := make([]Block, len(r.Blocks))
	copy(blocks, r.Blocks)
	for i := range blocks {
		blocks[i].Timing = nil
	}
	b, err := json.Marshal(struct {
		Blocks   []Block  `json:"blocks"`
		Messages []string `json:"messages"`
		Filters  []FilterDef
	}{blocks, r.Messages, r.Filters})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func runWithQueryConcurrency(t *testing.T, n int, content string) (*Result, error) {
	t.Helper()
	old := conf.Get().Engine.QueryConcurrency
	conf.Get().Engine.QueryConcurrency = n
	runtimecfg.Invalidate()
	defer func() {
		conf.Get().Engine.QueryConcurrency = old
		runtimecfg.Invalidate()
	}()
	return NewRunner("default").Run(content, nil)
}

// 并发 (8) 与串行 (1) 跑同一模板, 产出必须完全相同。
func TestRunConcurrencyEquivalence(t *testing.T) {
	setupSQLiteConcurrency(t)

	serial, err := runWithQueryConcurrency(t, 1, mixedTemplate)
	if err != nil {
		t.Fatalf("serial Run: %v", err)
	}
	concurrent, err := runWithQueryConcurrency(t, 8, mixedTemplate)
	if err != nil {
		t.Fatalf("concurrent Run: %v", err)
	}

	if got, want := resultJSON(t, concurrent), resultJSON(t, serial); got != want {
		t.Errorf("并发与串行产出不一致:\n串行  = %s\n并发  = %s", want, got)
	}

	// 顺序敏感性 sanity: 第一个块应是 markdown 概览, 脚本块在最后。
	if len(serial.Blocks) == 0 || serial.Blocks[0].Type != "markdown" {
		t.Fatalf("首块应为 markdown, got %+v", serial.Blocks)
	}
	if serial.Timing == nil || serial.Timing.ParsedBlocks == 0 {
		t.Fatalf("应返回报表计时信息, got %+v", serial.Timing)
	}
	if serial.Blocks[0].Timing == nil {
		t.Fatalf("应返回区块计时信息")
	}
	last := serial.Blocks[len(serial.Blocks)-1]
	if last.ID != "from_script" {
		t.Errorf("末块应为脚本产出 from_script, got id=%q", last.ID)
	}
}

// 并发数大于块数时不出错, 且与串行一致 (worker 池上限被收敛到 job 数)。
func TestRunConcurrencyExceedsBlocks(t *testing.T) {
	setupSQLiteConcurrency(t)
	content := "SELECT day, pv FROM pv ORDER BY day;"

	serial, err := runWithQueryConcurrency(t, 1, content)
	if err != nil {
		t.Fatalf("serial: %v", err)
	}
	concurrent, err := runWithQueryConcurrency(t, 64, content)
	if err != nil {
		t.Fatalf("concurrent: %v", err)
	}
	if resultJSON(t, concurrent) != resultJSON(t, serial) {
		t.Errorf("并发数>块数时产出应与串行一致")
	}
}

// 默认 Runner 走 conf 默认并发, 与显式串行产出一致。
func TestRunDefaultConcurrencyEquivalence(t *testing.T) {
	setupSQLiteConcurrency(t)
	def, err := NewRunner("default").Run(mixedTemplate, nil)
	if err != nil {
		t.Fatalf("default Run: %v", err)
	}
	serial, err := runWithQueryConcurrency(t, 1, mixedTemplate)
	if err != nil {
		t.Fatalf("serial Run: %v", err)
	}
	if resultJSON(t, def) != resultJSON(t, serial) {
		t.Errorf("默认并发与串行产出不一致")
	}
}

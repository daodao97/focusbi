package mcpserver

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"xproxy/conf"
	"xproxy/dao"
	"xproxy/internal/auth"
	"xproxy/internal/datasource"

	"github.com/daodao97/xgo/xdb"
)

// setupTestDB 用内存 SQLite 初始化 dao + datasource 的 default 数据源, 并建必要表。
func setupTestDB(t *testing.T) {
	t.Helper()
	conf.ConfInstance = &conf.Conf{
		Database: []xdb.Config{{
			Name:   "default",
			Driver: "sqlite",
			DSN:    "file:mcptest?mode=memory&cache=shared",
		}},
	}
	if err := xdb.Inits(conf.Get().Database); err != nil {
		t.Fatalf("xdb init: %v", err)
	}
	// 建表 (sqlite 方言, 仅测试所需字段)。
	stmts := []string{
		`DROP TABLE IF EXISTS report`,
		`CREATE TABLE report(id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, parent_id INTEGER DEFAULT 0,
			type TEXT DEFAULT 'report', sort INTEGER DEFAULT 0, dsn TEXT DEFAULT 'default',
			content TEXT DEFAULT '', dev_content TEXT DEFAULT '', settings TEXT DEFAULT '',
			remark TEXT DEFAULT '', is_public INTEGER DEFAULT 0, share_token TEXT DEFAULT '',
			created_at DATETIME, updated_at DATETIME)`,
		`DROP TABLE IF EXISTS dsn`,
		`CREATE TABLE dsn(id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT, driver TEXT, dsn TEXT,
			remark TEXT, ssh_enabled INTEGER DEFAULT 0, ssh_host TEXT, ssh_port INTEGER DEFAULT 22,
			ssh_user TEXT, ssh_auth TEXT, ssh_password TEXT, ssh_key TEXT, ssh_key_passphrase TEXT,
			created_at DATETIME, updated_at DATETIME)`,
		`DROP TABLE IF EXISTS biz`,
		`CREATE TABLE biz(day TEXT, amount INTEGER)`,
		`INSERT INTO biz VALUES('2026-06-24', 100), ('2026-06-25', 200)`,
	}
	for _, s := range stmts {
		if _, err := datasource.Query("default", s); err != nil {
			t.Fatalf("setup sql %q: %v", s, err)
		}
	}
	dao.Report = xdb.New("report")
	dao.Dsn = xdb.New("dsn")
}

// ctxWithPerm 构造一个带指定权限的调用上下文 (绕过 HTTP 鉴权, 直接测工具门禁)。
func ctxWithPerm(resources map[string]string) context.Context {
	perm := auth.NewPermissionFromResources(resources)
	pr := &principal{user: &dao.UserRecord{Id: 1, Nick: "tester"}, perm: perm}
	return withPrincipal(context.Background(), pr)
}

func TestUnauthenticatedRejected(t *testing.T) {
	// 无 principal 的裸 context: 应被拒。
	if _, _, err := getSyntaxDoc(context.Background(), nil, emptyIn{}); err == nil {
		t.Fatal("无认证应被拒绝")
	}
}

func TestCreateReportRequiresWrite(t *testing.T) {
	setupTestDB(t)
	// 只读权限 -> 创建被拒。
	roCtx := ctxWithPerm(map[string]string{"report": "Rr"})
	if _, _, err := createReportTool(roCtx, nil, createReportIn{Name: "x"}); err == nil {
		t.Fatal("仅读权限不应能创建报表")
	}
	// 有 report.manage 写权限 -> 成功。
	rwCtx := ctxWithPerm(map[string]string{"report.manage": "rw"})
	_, out, err := createReportTool(rwCtx, nil, createReportIn{Name: "销售日报", DevContent: "SELECT 1;"})
	if err != nil {
		t.Fatalf("有写权限创建失败: %v", err)
	}
	if out.ID <= 0 {
		t.Fatalf("应返回新报表 id, got %d", out.ID)
	}
}

func TestListReportsFilteredByPermission(t *testing.T) {
	setupTestDB(t)
	rwCtx := ctxWithPerm(map[string]string{"report.manage": "rw"})
	if _, _, err := createReportTool(rwCtx, nil, createReportIn{Name: "r1"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 无任何 report 读权限的用户 -> 列表为空 (不泄漏)。
	noCtx := ctxWithPerm(map[string]string{"dsn": "r"})
	_, out, err := listReportsTool(noCtx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(out.Reports) != 0 {
		t.Fatalf("无读权限应看不到报表, got %d", len(out.Reports))
	}
	// 全局读权限 -> 能看到。
	roCtx := ctxWithPerm(map[string]string{"report": "Rr"})
	_, out2, _ := listReportsTool(roCtx, nil, emptyIn{})
	if len(out2.Reports) == 0 {
		t.Fatal("有读权限应能看到报表")
	}
}

func TestReportURLFromSiteConfig(t *testing.T) {
	setupTestDB(t)
	conf.Get().Site.URL = "https://bi.example.com/" // 尾部斜杠应被 SiteBaseURL 去掉
	rwCtx := ctxWithPerm(map[string]string{"report.manage": "rw", "report": "Rr"})

	// 报表 -> 控制台查看链接 + 编辑链接
	_, created, err := createReportTool(rwCtx, nil, createReportIn{Name: "r1"})
	if err != nil {
		t.Fatalf("create report: %v", err)
	}
	wantView := "https://bi.example.com/#/reports/" + strconv.Itoa(created.ID)
	if created.URL != wantView {
		t.Errorf("create url = %q, want %q", created.URL, wantView)
	}
	_, got, err := getReportTool(rwCtx, nil, getReportIn{ID: created.ID})
	if err != nil {
		t.Fatalf("get report: %v", err)
	}
	if got.URL != wantView {
		t.Errorf("get url = %q, want %q", got.URL, wantView)
	}
	if got.EditURL != wantView+"/edit" {
		t.Errorf("get edit_url = %q, want %q", got.EditURL, wantView+"/edit")
	}

	// folder -> 无链接 (打不开)
	_, folder, err := createReportTool(rwCtx, nil, createReportIn{Name: "f1", Type: "folder"})
	if err != nil {
		t.Fatalf("create folder: %v", err)
	}
	if folder.URL != "" {
		t.Errorf("folder url = %q, want empty", folder.URL)
	}

	// 站点地址未配置 -> 无链接
	conf.Get().Site.URL = ""
	_, got2, _ := getReportTool(rwCtx, nil, getReportIn{ID: created.ID})
	if got2.URL != "" || got2.EditURL != "" {
		t.Errorf("no site url should yield empty links, got url=%q edit=%q", got2.URL, got2.EditURL)
	}
}

func TestQueryRawSelectOnly(t *testing.T) {
	setupTestDB(t)
	ctx := ctxWithPerm(map[string]string{"dsn": "r"})

	// 非 SELECT 被拒。
	for _, sql := range []string{
		"DELETE FROM biz",
		"UPDATE biz SET amount=0",
		"DROP TABLE biz",
		"SELECT 1; DROP TABLE biz", // 多语句
	} {
		if _, _, err := queryRawTool(ctx, nil, queryRawIn{DSN: "default", SQL: sql}); err == nil {
			t.Errorf("应拒绝非只读/多语句: %q", sql)
		}
	}

	// 正常 SELECT 成功。
	_, out, err := queryRawTool(ctx, nil, queryRawIn{DSN: "default", SQL: "SELECT day, amount FROM biz ORDER BY day"})
	if err != nil {
		t.Fatalf("SELECT 应成功: %v", err)
	}
	if len(out.Rows) != 2 {
		t.Fatalf("应返回 2 行, got %d", len(out.Rows))
	}
}

func TestQueryRawRequiresDsnRead(t *testing.T) {
	setupTestDB(t)
	// 无 dsn 权限 -> 拒绝。
	ctx := ctxWithPerm(map[string]string{"report.manage": "rw"})
	if _, _, err := queryRawTool(ctx, nil, queryRawIn{DSN: "default", SQL: "SELECT 1"}); err == nil {
		t.Fatal("无 dsn 读权限应被拒绝")
	}
}

func TestPreviewRequiresManage(t *testing.T) {
	setupTestDB(t)
	// 仅读 -> 拒绝。
	if _, _, err := previewTemplateTool(ctxWithPerm(map[string]string{"report": "Rr"}), nil,
		previewIn{DSN: "default", Content: "SELECT day, amount FROM biz;"}); err == nil {
		t.Fatal("preview 需 report.manage 写权限")
	}
	// 有写 -> 成功并返回区块。
	_, out, err := previewTemplateTool(ctxWithPerm(map[string]string{"report.manage": "rw"}), nil,
		previewIn{DSN: "default", Content: "SELECT day, amount FROM biz;"})
	if err != nil {
		t.Fatalf("preview 失败: %v", err)
	}
	if len(out.Blocks) == 0 {
		t.Fatal("preview 应返回至少一个区块")
	}
}

func TestResolveUserIDSetsExpiration(t *testing.T) {
	setupTestDB(t) // 提供 conf (JWT secret 回退默认值)
	tok, err := auth.IssueToken(7, "alice", false)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	uid, exp, err := resolveUserID(tok)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if uid != 7 {
		t.Errorf("uid = %d, want 7", uid)
	}
	// SDK 要求 Expiration 为将来的非零时间, 否则 401 "token missing expiration"。
	if exp.IsZero() {
		t.Fatal("过期时间不应为零值")
	}
	if !exp.After(time.Now()) {
		t.Fatal("过期时间应在将来")
	}
}

func TestSyntaxDocReturned(t *testing.T) {
	ctx := ctxWithPerm(map[string]string{})
	_, out, err := getSyntaxDoc(ctx, nil, emptyIn{})
	if err != nil {
		t.Fatalf("get_syntax_doc: %v", err)
	}
	if !strings.Contains(out.Markdown, "报表模板") {
		t.Fatal("应返回语法文档内容")
	}
}

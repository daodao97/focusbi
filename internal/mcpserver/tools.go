package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"xproxy/conf"
	"xproxy/dao"
	"xproxy/docs"
	"xproxy/internal/auth"
	"xproxy/internal/datasource"
	"xproxy/internal/engine"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// 权限资源常量 (与 api/setup.go 对齐)。
const (
	resReportManage = "report.manage"
	resDsn          = "dsn"
)

// queryRawMaxRows 限制 query_raw 返回行数, 避免 AI 拉爆上下文。
const queryRawMaxRows = 200

// ---- 工具输入/输出结构 (字段 json tag + jsonschema 描述, SDK 自动生成 schema) ----

type emptyIn struct{}

type docOut struct {
	Markdown string `json:"markdown" jsonschema:"报表模板语法的完整权威说明 (Markdown)"`
}

type listReportsOut struct {
	Reports []reportBrief `json:"reports"`
}

type reportBrief struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type" jsonschema:"report 或 folder"`
	ParentID int    `json:"parent_id"`
	DSN      string `json:"dsn"`
	URL      string `json:"url,omitempty" jsonschema:"报表查看链接 (可直接打开); 站点地址未配置或 folder 时为空"`
}

type getReportIn struct {
	ID int `json:"id" jsonschema:"报表 id"`
}

type getReportOut struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	ParentID   int    `json:"parent_id"`
	DSN        string `json:"dsn"`
	DevContent string `json:"dev_content" jsonschema:"开发版草稿模板 (编辑对象)"`
	Content    string `json:"content" jsonschema:"已发布版模板 (查看者所见)"`
	Settings   string `json:"settings"`
	IsPublic   bool   `json:"is_public"`
	URL        string `json:"url,omitempty" jsonschema:"报表查看链接 (控制台, 可直接打开); 站点地址未配置或 folder 时为空"`
	EditURL    string `json:"edit_url,omitempty" jsonschema:"报表编辑链接 (控制台); 站点地址未配置或 folder 时为空"`
}

type listDatasourcesOut struct {
	Datasources []dsnBrief `json:"datasources"`
}

type dsnBrief struct {
	Name   string `json:"name"`
	Driver string `json:"driver"`
	Remark string `json:"remark"`
}

type dsnNameIn struct {
	DSN string `json:"dsn" jsonschema:"数据源名称"`
}

type listDatabasesOut struct {
	Databases []string `json:"databases"`
}

type listTablesIn struct {
	DSN string `json:"dsn" jsonschema:"数据源名称"`
	DB  string `json:"db" jsonschema:"数据库/schema 名 (留空用默认库)"`
}

type listTablesOut struct {
	Tables []string `json:"tables"`
}

type describeTableIn struct {
	DSN   string `json:"dsn"`
	DB    string `json:"db" jsonschema:"数据库/schema 名 (留空用默认库)"`
	Table string `json:"table"`
}

type describeTableOut struct {
	Columns []datasource.Column `json:"columns"`
}

type queryRawIn struct {
	DSN string `json:"dsn"`
	SQL string `json:"sql" jsonschema:"只读 SELECT 查询; 自动补 LIMIT, 仅供探数据"`
}

type queryRawOut struct {
	Columns   []string         `json:"columns"`
	Rows      []map[string]any `json:"rows"`
	Truncated bool             `json:"truncated" jsonschema:"结果是否因超过上限被截断"`
}

type previewIn struct {
	DSN     string            `json:"dsn" jsonschema:"数据源名称 (留空用 default)"`
	Content string            `json:"content" jsonschema:"待试跑的报表模板"`
	Params  map[string]string `json:"params,omitempty" jsonschema:"过滤器取值 (可选)"`
}

type previewOut struct {
	Filters []engine.FilterDef `json:"filters"`
	Blocks  []engine.Block     `json:"blocks" jsonschema:"各区块结果; 区块的 error 字段非空表示该块 SQL/脚本出错"`
}

type createReportIn struct {
	Name       string `json:"name"`
	ParentID   int    `json:"parent_id" jsonschema:"父文件夹 id, 0 为根"`
	Type       string `json:"type" jsonschema:"report 或 folder, 留空默认 report"`
	DSN        string `json:"dsn" jsonschema:"数据源名称, 留空用 default"`
	DevContent string `json:"dev_content" jsonschema:"初始模板 (开发版草稿)"`
}

type idOut struct {
	ID  int    `json:"id"`
	URL string `json:"url,omitempty" jsonschema:"新建报表的控制台查看链接 (folder 或站点地址未配置时为空)"`
}

type updateReportIn struct {
	ID         int     `json:"id"`
	Name       *string `json:"name,omitempty"`
	DSN        *string `json:"dsn,omitempty"`
	DevContent *string `json:"dev_content,omitempty" jsonschema:"更新开发版草稿模板"`
	Settings   *string `json:"settings,omitempty"`
}

type okOut struct {
	OK bool `json:"ok"`
}

type publishIn struct {
	ID int `json:"id"`
}

// ---- 工具注册 ----

// registerTools 把所有报表开发工具注册到 server。
func registerTools(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "get_syntax_doc",
		Description: "获取报表模板语法的完整权威说明; 编写/修改模板前应先读它。"},
		getSyntaxDoc)

	mcp.AddTool(s, &mcp.Tool{Name: "list_reports",
		Description: "列出当前用户有读权限的报表与文件夹。"},
		listReportsTool)

	mcp.AddTool(s, &mcp.Tool{Name: "get_report",
		Description: "读取单个报表的元信息与模板 (开发版/发布版)。"},
		getReportTool)

	mcp.AddTool(s, &mcp.Tool{Name: "list_datasources",
		Description: "列出可用数据源 (仅名称/驱动/备注, 不含连接串与密钥)。需 dsn 读权限。"},
		listDatasourcesTool)

	mcp.AddTool(s, &mcp.Tool{Name: "list_databases",
		Description: "列出某数据源下的数据库/schema。需 dsn 读权限。"},
		listDatabasesTool)

	mcp.AddTool(s, &mcp.Tool{Name: "list_tables",
		Description: "列出某数据源某库下的表。需 dsn 读权限。"},
		listTablesTool)

	mcp.AddTool(s, &mcp.Tool{Name: "describe_table",
		Description: "查看表的列定义 (名称/类型/注释)。需 dsn 读权限。"},
		describeTableTool)

	mcp.AddTool(s, &mcp.Tool{Name: "query_raw",
		Description: "对数据源执行只读 SELECT 查询探数据 (自动补 LIMIT, 结果截断)。需 dsn 读权限。"},
		queryRawTool)

	mcp.AddTool(s, &mcp.Tool{Name: "preview_template",
		Description: "试跑一段报表模板并返回结构化结果 (含每个区块的错误), 不落库。用于校验模板。需 report.manage 写权限。"},
		previewTemplateTool)

	mcp.AddTool(s, &mcp.Tool{Name: "create_report",
		Description: "创建一个报表或文件夹。需 report.manage 写权限。"},
		createReportTool)

	mcp.AddTool(s, &mcp.Tool{Name: "update_report",
		Description: "更新报表的开发版草稿/名称/数据源/设置 (只动草稿, 不影响已发布版)。需 report.manage 写权限。"},
		updateReportTool)

	mcp.AddTool(s, &mcp.Tool{Name: "publish_report",
		Description: "把开发版草稿发布为正式版 (查看者与定时任务据此); 同时记录一个版本快照。需 report.manage 写权限。"},
		publishReportTool)
}

// ---- handler 实现 ----

func getSyntaxDoc(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, docOut, error) {
	if _, err := principalFromContext(ctx); err != nil {
		return nil, docOut{}, err
	}
	return nil, docOut{Markdown: docs.SyntaxMarkdown}, nil
}

func listReportsTool(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, listReportsOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, listReportsOut{}, err
	}
	parents, err := auth.LoadReportParents()
	if err != nil {
		return nil, listReportsOut{}, err
	}
	list, err := dao.ListReports()
	if err != nil {
		return nil, listReportsOut{}, err
	}
	out := listReportsOut{}
	for _, r := range list {
		if pr.perm.ReportReadable(r.Id, parents, "r") {
			out.Reports = append(out.Reports, reportBrief{
				ID: r.Id, Name: r.Name, Type: r.Type, ParentID: r.ParentID, DSN: r.DSN,
				URL: reportURL(r),
			})
		}
	}
	return nil, out, nil
}

func getReportTool(ctx context.Context, _ *mcp.CallToolRequest, in getReportIn) (*mcp.CallToolResult, getReportOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, getReportOut{}, err
	}
	parents, err := auth.LoadReportParents()
	if err != nil {
		return nil, getReportOut{}, err
	}
	if !pr.perm.ReportReadable(in.ID, parents, "r") {
		return nil, getReportOut{}, fmt.Errorf("无权读取该报表")
	}
	r, err := dao.GetReportByID(in.ID)
	if err != nil {
		return nil, getReportOut{}, err
	}
	return nil, getReportOut{
		ID: r.Id, Name: r.Name, Type: r.Type, ParentID: r.ParentID, DSN: r.DSN,
		DevContent: r.DevContent, Content: r.Content, Settings: r.Settings, IsPublic: r.IsPublic,
		URL: reportURL(r), EditURL: reportEditURL(r),
	}, nil
}

func listDatasourcesTool(ctx context.Context, _ *mcp.CallToolRequest, _ emptyIn) (*mcp.CallToolResult, listDatasourcesOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, listDatasourcesOut{}, err
	}
	list, err := dao.ListDsn()
	if err != nil {
		return nil, listDatasourcesOut{}, err
	}
	// 按权限过滤: 只回调用者可读的数据源。
	nameToID := make(map[string]int, len(list))
	for _, d := range list {
		nameToID[d.Name] = d.Id
	}
	out := listDatasourcesOut{}
	for _, d := range list {
		if pr.perm.DsnReadable(d.Name, nameToID) {
			out.Datasources = append(out.Datasources, dsnBrief{Name: d.Name, Driver: d.Driver, Remark: d.Remark})
		}
	}
	return nil, out, nil
}

func listDatabasesTool(ctx context.Context, _ *mcp.CallToolRequest, in dsnNameIn) (*mcp.CallToolResult, listDatabasesOut, error) {
	if err := requireDsnReadByName(ctx, in.DSN); err != nil {
		return nil, listDatabasesOut{}, err
	}
	dbs, err := datasource.ListDatabases(in.DSN)
	if err != nil {
		return nil, listDatabasesOut{}, err
	}
	return nil, listDatabasesOut{Databases: dbs}, nil
}

func listTablesTool(ctx context.Context, _ *mcp.CallToolRequest, in listTablesIn) (*mcp.CallToolResult, listTablesOut, error) {
	if err := requireDsnReadByName(ctx, in.DSN); err != nil {
		return nil, listTablesOut{}, err
	}
	tables, err := datasource.ListTables(in.DSN, in.DB)
	if err != nil {
		return nil, listTablesOut{}, err
	}
	return nil, listTablesOut{Tables: tables}, nil
}

func describeTableTool(ctx context.Context, _ *mcp.CallToolRequest, in describeTableIn) (*mcp.CallToolResult, describeTableOut, error) {
	if err := requireDsnReadByName(ctx, in.DSN); err != nil {
		return nil, describeTableOut{}, err
	}
	cols, err := datasource.TableColumns(in.DSN, in.DB, in.Table)
	if err != nil {
		return nil, describeTableOut{}, err
	}
	return nil, describeTableOut{Columns: cols}, nil
}

// selectOnlyRe 粗校验语句以 SELECT 或 WITH 开头 (去掉前导注释/空白后)。
var selectOnlyRe = regexp.MustCompile(`(?is)^\s*(with|select)\b`)

func queryRawTool(ctx context.Context, _ *mcp.CallToolRequest, in queryRawIn) (*mcp.CallToolResult, queryRawOut, error) {
	if err := requireDsnReadByName(ctx, in.DSN); err != nil {
		return nil, queryRawOut{}, err
	}
	sql := strings.TrimSpace(in.SQL)
	if !selectOnlyRe.MatchString(sql) {
		return nil, queryRawOut{}, fmt.Errorf("query_raw 仅支持只读 SELECT/WITH 查询")
	}
	// 拒绝多语句, 防夹带写操作。
	if strings.Contains(strings.TrimRight(sql, ";"), ";") {
		return nil, queryRawOut{}, fmt.Errorf("query_raw 不支持多条语句")
	}
	res, err := datasource.Query(in.DSN, sql)
	if err != nil {
		return nil, queryRawOut{}, err
	}
	out := queryRawOut{Columns: res.Columns, Rows: res.Rows}
	if len(out.Rows) > queryRawMaxRows {
		out.Rows = out.Rows[:queryRawMaxRows]
		out.Truncated = true
	}
	return nil, out, nil
}

func previewTemplateTool(ctx context.Context, _ *mcp.CallToolRequest, in previewIn) (*mcp.CallToolResult, previewOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, previewOut{}, err
	}
	if !pr.perm.Check(resReportManage, "rw") {
		return nil, previewOut{}, fmt.Errorf("无权试跑模板 (需 report.manage 写权限)")
	}
	nameToID, err := auth.LoadDsnNameToID()
	if err != nil {
		return nil, previewOut{}, err
	}
	// 按调用者权限校验模板触达的每个数据源。
	result, err := engine.NewRunner(in.DSN).WithNoCache(true).
		WithAuthz(pr.perm.DsnAuthz(nameToID)).Run(in.Content, in.Params)
	if err != nil {
		return nil, previewOut{}, err
	}
	return nil, previewOut{Filters: result.Filters, Blocks: result.Blocks}, nil
}

func createReportTool(ctx context.Context, _ *mcp.CallToolRequest, in createReportIn) (*mcp.CallToolResult, idOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, idOut{}, err
	}
	if !pr.perm.Check(resReportManage, "rw") {
		return nil, idOut{}, fmt.Errorf("无权创建报表 (需 report.manage 写权限)")
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, idOut{}, fmt.Errorf("报表名不能为空")
	}
	typ := strings.TrimSpace(in.Type)
	if typ == "" {
		typ = "report"
	}
	dsn := strings.TrimSpace(in.DSN)
	if dsn == "" {
		dsn = "default"
	}
	id, err := dao.CreateReport(&dao.ReportRecord{
		Name: in.Name, ParentID: in.ParentID, Type: typ, DSN: dsn, DevContent: in.DevContent,
	})
	if err != nil {
		return nil, idOut{}, err
	}
	return nil, idOut{ID: int(id), URL: reportURL(&dao.ReportRecord{Id: int(id), Type: typ})}, nil
}

func updateReportTool(ctx context.Context, _ *mcp.CallToolRequest, in updateReportIn) (*mcp.CallToolResult, okOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, okOut{}, err
	}
	if !pr.perm.Check(resReportManage, "rw") {
		return nil, okOut{}, fmt.Errorf("无权修改报表 (需 report.manage 写权限)")
	}
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = strings.TrimSpace(*in.Name)
	}
	if in.DSN != nil {
		updates["dsn"] = strings.TrimSpace(*in.DSN)
	}
	if in.DevContent != nil {
		updates["dev_content"] = *in.DevContent
	}
	if in.Settings != nil {
		updates["settings"] = *in.Settings
	}
	if len(updates) == 0 {
		return nil, okOut{}, fmt.Errorf("没有要更新的字段")
	}
	if err := dao.UpdateReportByID(in.ID, updates); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

func publishReportTool(ctx context.Context, _ *mcp.CallToolRequest, in publishIn) (*mcp.CallToolResult, okOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, okOut{}, err
	}
	if !pr.perm.Check(resReportManage, "rw") {
		return nil, okOut{}, fmt.Errorf("无权发布报表 (需 report.manage 写权限)")
	}
	if err := dao.PublishReport(in.ID); err != nil {
		return nil, okOut{}, err
	}
	// 记录版本快照 (与 REST publishReport 行为一致); 失败不阻断发布。
	if r, e := dao.GetReportByID(in.ID); e == nil {
		_ = dao.AddReportVersion(in.ID, r.Content, pr.user.Id, pr.user.Nick)
	}
	return nil, okOut{OK: true}, nil
}

// reportURL 构造报表在控制台的查看链接, 方便在 AI 工具里直接点开。
// MCP 调用者是已登录的开发者 (有后台权限), 故统一指向控制台查看页 (/#/reports/<id>),
// 而非公开分享页。folder 或站点地址未配置时返回空。
func reportURL(r *dao.ReportRecord) string {
	base := conf.Get().SiteBaseURL()
	if base == "" || r.Type == "folder" {
		return ""
	}
	return fmt.Sprintf("%s/#/reports/%d", base, r.Id)
}

// reportEditURL 构造报表的控制台编辑链接 (AI 改完模板可直接点开继续编辑/预览)。
func reportEditURL(r *dao.ReportRecord) string {
	if u := reportURL(r); u != "" {
		return u + "/edit"
	}
	return ""
}

// requireDsnReadByName 校验调用者对某数据源 (按名) 有读权限 (dsn.<id> 或全局 dsn)。
func requireDsnReadByName(ctx context.Context, name string) error {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return err
	}
	nameToID, err := auth.LoadDsnNameToID()
	if err != nil {
		return err
	}
	if !pr.perm.DsnReadable(name, nameToID) {
		return fmt.Errorf("无权使用数据源 %q", name)
	}
	return nil
}

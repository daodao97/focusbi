package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"xproxy/conf"
	"xproxy/dao"
	"xproxy/docs"
	"xproxy/internal/auth"
	"xproxy/internal/datasource"
	"xproxy/internal/engine"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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
	DSN          string            `json:"dsn" jsonschema:"数据源名称 (留空用 default)"`
	Content      string            `json:"content" jsonschema:"待试跑的报表模板"`
	Params       map[string]string `json:"params,omitempty" jsonschema:"过滤器取值 (可选)"`
	ValidateOnly bool              `json:"validate_only,omitempty" jsonschema:"只做解析+语法校验, 不连库执行 (快速迭代模板结构时用)"`
}

// blockError 是 preview 里出错区块的分类诊断, 帮 AI 定位是哪类错误、怎么改。
type blockError struct {
	BlockID string `json:"block_id,omitempty"`
	Kind    string `json:"kind" jsonschema:"错误类别: sql / template / chart / script / unknown"`
	Message string `json:"message" jsonschema:"原始错误信息"`
	Hint    string `json:"hint,omitempty" jsonschema:"针对该类错误的修正建议"`
}

type previewOut struct {
	Filters []engine.FilterDef `json:"filters"`
	Blocks  []engine.Block     `json:"blocks" jsonschema:"各区块结果; 区块的 error 字段非空表示该块出错"`
	Errors  []blockError       `json:"errors,omitempty" jsonschema:"出错区块的分类诊断与修正建议 (为空表示全部成功)"`
}

type createReportIn struct {
	Name       string `json:"name"`
	ParentID   int    `json:"parent_id" jsonschema:"父文件夹 id, 0 为根"`
	Type       string `json:"type" jsonschema:"report 或 folder, 留空默认 report"`
	DSN        string `json:"dsn" jsonschema:"数据源名称, 留空用 default"`
	DevContent string `json:"dev_content" jsonschema:"初始模板 (开发版草稿)"`
}

type idOut struct {
	ID         int    `json:"id"`
	URL        string `json:"url,omitempty" jsonschema:"新建报表的控制台查看链接 (folder 或站点地址未配置时为空)"`
	NextAction string `json:"next_action,omitempty" jsonschema:"下一步建议; 报表内容保存为草稿后需调用 publish_report 才会在查看页生效"`
}

type updateReportIn struct {
	ID         int     `json:"id"`
	Name       *string `json:"name,omitempty"`
	DSN        *string `json:"dsn,omitempty"`
	DevContent *string `json:"dev_content,omitempty" jsonschema:"更新开发版草稿模板"`
	Settings   *string `json:"settings,omitempty"`
}

type okOut struct {
	OK         bool   `json:"ok"`
	NextAction string `json:"next_action,omitempty" jsonschema:"下一步建议; 草稿修改后需调用 publish_report 才会在查看页生效"`
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
		Description: "查看表的列定义 (名称/类型/注释) 及每列若干去重样例值 (判断枚举取值/日期粒度/口径)。需 dsn 读权限。"},
		describeTableTool)

	mcp.AddTool(s, &mcp.Tool{Name: "query_raw",
		Description: "对数据源执行只读 SELECT 查询探数据 (自动补 LIMIT, 结果截断)。需 dsn 读权限。"},
		queryRawTool)

	mcp.AddTool(s, &mcp.Tool{Name: "preview_template",
		Description: "试跑一段报表模板并返回结构化结果, 不落库。出错区块在 errors 里带分类与修正建议。传 validate_only=true 只做语法校验不连库 (快速迭代结构)。需报表写权限。"},
		previewTemplateTool)

	mcp.AddTool(s, &mcp.Tool{Name: "create_report",
		Description: "创建一个报表或文件夹。报表内容只写入开发版草稿; 若用户要在查看页看到结果, 预览通过后必须继续调用 publish_report。需对目标父级有写权限; 根目录需全局 report 写权限。"},
		createReportTool)

	mcp.AddTool(s, &mcp.Tool{Name: "update_report",
		Description: "更新报表的开发版草稿/名称/数据源/设置 (只动草稿, 不影响已发布版); 若用户要在查看页看到修改, 预览通过后必须继续调用 publish_report。需对该报表有写权限。"},
		updateReportTool)

	mcp.AddTool(s, &mcp.Tool{Name: "publish_report",
		Description: "把开发版草稿发布为正式版 (查看页、查看者与定时任务据此); 同时记录一个版本快照。create_report/update_report 后要让查看页生效就调用本工具。需对该报表有写权限。"},
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
	cols, err := datasource.TableColumnsWithSamples(in.DSN, in.DB, in.Table)
	if err != nil {
		return nil, describeTableOut{}, err
	}
	return nil, describeTableOut{Columns: cols}, nil
}

func queryRawTool(ctx context.Context, _ *mcp.CallToolRequest, in queryRawIn) (*mcp.CallToolResult, queryRawOut, error) {
	if err := requireDsnReadByName(ctx, in.DSN); err != nil {
		return nil, queryRawOut{}, err
	}
	// 多取一行用于判断是否截断; 校验和补 LIMIT 复用报表引擎的统一安全策略。
	sql, err := engine.PrepareReadOnlySQL(strings.TrimSpace(in.SQL), queryRawMaxRows+1)
	if err != nil {
		return nil, queryRawOut{}, err
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
	if !pr.perm.CanWriteAnyReport() {
		return nil, previewOut{}, fmt.Errorf("无权试跑模板 (需报表写权限)")
	}

	// validate_only: 只解析 + 只读校验, 不连库、不校验数据源权限 (无库交互)。
	if in.ValidateOnly {
		filters, issues := engine.Validate(in.Content)
		out := previewOut{Filters: filters}
		for _, is := range issues {
			out.Errors = append(out.Errors, blockError{
				BlockID: is.BlockID, Kind: "sql", Message: is.Message,
				Hint: classifyHint("sql", is.Message),
			})
		}
		return nil, out, nil
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
	out := previewOut{Filters: result.Filters, Blocks: result.Blocks}
	for _, b := range result.Blocks {
		if b.Error == "" {
			continue
		}
		kind := classifyBlockError(b.Type, b.Error)
		out.Errors = append(out.Errors, blockError{
			BlockID: b.ID, Kind: kind, Message: b.Error, Hint: classifyHint(kind, b.Error),
		})
	}
	return nil, out, nil
}

// classifyBlockError 依据区块类型与错误文本粗分类, 供 AI 快速定位。
func classifyBlockError(blockType, msg string) string {
	if blockType == "markdown" || blockType == "raw" {
		return "template"
	}
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "chart") || strings.Contains(low, "图表"):
		return "chart"
	case strings.Contains(low, "script") || strings.Contains(low, "脚本") || strings.Contains(low, "goja"):
		return "script"
	case strings.Contains(low, "sql") || strings.Contains(low, "syntax") ||
		strings.Contains(low, "column") || strings.Contains(low, "table") ||
		strings.Contains(low, "unknown") || strings.Contains(low, "near "):
		return "sql"
	default:
		return "unknown"
	}
}

// classifyHint 给出针对错误类别的一句修正建议。
func classifyHint(kind, msg string) string {
	low := strings.ToLower(msg)
	switch kind {
	case "sql":
		switch {
		case strings.Contains(low, "unknown column"):
			return "列名不存在: 用 describe_table 核对列名, 或检查是否漏写表别名。"
		case strings.Contains(low, "unknown") && strings.Contains(low, "table"):
			return "表不存在: 用 list_tables 核对表名与所属库。"
		case strings.Contains(low, "只读") || strings.Contains(low, "read-only") || strings.Contains(low, "forbidden"):
			return "报表模板只允许只读 SELECT; 移除写操作或多语句。"
		case strings.Contains(low, "syntax") || strings.Contains(low, "near "):
			return "SQL 语法错误: 检查宏 {name} 是否已正确闭合、是否缺少引号或逗号。"
		default:
			return "检查 SQL 是否针对该数据源方言合法; 可先用 query_raw 单独验证。"
		}
	case "template":
		return "模板语法问题: 用 get_syntax_doc 核对注解 (-- @key=value) 与区块标记写法。"
	case "chart":
		return "图表配置问题: 核对 @chart 引用的列名确实出现在该区块 SELECT 结果中。"
	case "script":
		return "脚本执行出错: 检查 #!SCRIPT 块的 JS 语法与 query()/where() 用法。"
	default:
		return ""
	}
}

func createReportTool(ctx context.Context, _ *mcp.CallToolRequest, in createReportIn) (*mcp.CallToolResult, idOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, idOut{}, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return nil, idOut{}, fmt.Errorf("报表名不能为空")
	}
	// 指定父级: 须存在、是文件夹且用户可写 (与 REST createReport 一致)。
	// 根目录 (无父级): 需全局 report:w, 单报表写权限不能扩展到根级全局空间。
	if in.ParentID != 0 {
		if err := requireWritableParent(pr, in.ParentID); err != nil {
			return nil, idOut{}, err
		}
	} else if !pr.perm.Check("report", "w") {
		return nil, idOut{}, fmt.Errorf("无权在根目录创建报表 (需全局报表写权限)")
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
	out := idOut{ID: int(id), URL: reportURL(&dao.ReportRecord{Id: int(id), Type: typ})}
	if typ != "folder" {
		out.NextAction = "内容已保存为开发版草稿; 请先用 preview_template 验证, 然后调用 publish_report 发布, 否则查看页仍看不到新内容。"
	}
	return nil, out, nil
}

func updateReportTool(ctx context.Context, _ *mcp.CallToolRequest, in updateReportIn) (*mcp.CallToolResult, okOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, okOut{}, err
	}
	if err := requireReportWrite(pr, in.ID); err != nil {
		return nil, okOut{}, err
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
	return nil, okOut{
		OK:         true,
		NextAction: "已更新开发版草稿; 请先用 preview_template 验证, 然后调用 publish_report 发布, 否则查看页仍显示旧的已发布版。",
	}, nil
}

func publishReportTool(ctx context.Context, _ *mcp.CallToolRequest, in publishIn) (*mcp.CallToolResult, okOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, okOut{}, err
	}
	if err := requireReportWrite(pr, in.ID); err != nil {
		return nil, okOut{}, err
	}
	r, err := dao.GetReportByID(in.ID)
	if err != nil {
		return nil, okOut{}, err
	}
	if !r.IsFolder() {
		if err := validateContentDSNAccess(pr, r.DSN, r.DevContent); err != nil {
			return nil, okOut{}, err
		}
		r.Settings = dao.SettingsWithApprovedDSNs(r.Settings, engine.CollectDSNs(r.DSN, r.DevContent))
		if err := dao.PublishReportContentWithSettings(in.ID, r.DevContent, r.Settings); err != nil {
			return nil, okOut{}, err
		}
	} else if err := dao.PublishReport(in.ID); err != nil {
		return nil, okOut{}, err
	}
	// 记录版本快照 (与 REST publishReport 行为一致); 失败不阻断发布。
	_ = dao.AddReportVersion(in.ID, r.DevContent, pr.user.Id, pr.user.Nick)
	return nil, okOut{OK: true, NextAction: "已发布; 查看页、查看者和定时任务现在会使用最新发布版。"}, nil
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

// requireReportWrite 校验调用者对某报表有写权限 (report.<id>:w 或祖先文件夹递归 Rw)。
func requireReportWrite(pr *principal, id int) error {
	parents, err := auth.LoadReportParents()
	if err != nil {
		return err
	}
	if !pr.perm.ReportReadable(id, parents, "w") {
		return fmt.Errorf("无权修改该报表")
	}
	return nil
}

// requireWritableParent 校验目标父级合法且可写 (与 REST requireWritableParent 对称):
// parentID==0 (根) 时调用方另行校验全局 report:w; 否则父级须存在、是文件夹、且用户可写。
// 防止把报表建到普通报表下、或借过期的 report.<id>:w 建到已不存在的父级下。
func requireWritableParent(pr *principal, parentID int) error {
	if parentID == 0 {
		return nil
	}
	list, err := dao.ListReports()
	if err != nil {
		return err
	}
	var parent *dao.ReportRecord
	parents := make(map[int]int, len(list))
	for _, x := range list {
		parents[x.Id] = x.ParentID
		if x.Id == parentID {
			parent = x
		}
	}
	if parent == nil || !parent.IsFolder() {
		return fmt.Errorf("父级不存在或不是文件夹")
	}
	if !pr.perm.ReportReadable(parentID, parents, "w") {
		return fmt.Errorf("无权在该目录下创建报表")
	}
	return nil
}

func validateContentDSNAccess(pr *principal, dsn, content string) error {
	nameToID, err := auth.LoadDsnNameToID()
	if err != nil {
		return err
	}
	authz := pr.perm.DsnAuthz(nameToID)
	denied := map[string]bool{}
	for _, name := range engine.CollectDSNs(dsn, content) {
		if err := authz(name); err != nil {
			denied[name] = true
		}
	}
	if len(denied) == 0 {
		return nil
	}
	names := make([]string, 0, len(denied))
	for name := range denied {
		names = append(names, name)
	}
	sort.Strings(names)
	return fmt.Errorf("报表内容引用了无权使用的数据源: %s", strings.Join(names, ", "))
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

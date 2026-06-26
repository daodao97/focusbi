package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"xproxy/dao"
	"xproxy/internal/ai"
	"xproxy/internal/auth"
	"xproxy/internal/datasource"
	"xproxy/internal/engine"

	"github.com/daodao97/xgo/xdb"
	"github.com/gin-gonic/gin"
)

// 资源串约定:
//
//	report           全部报表 (列表/查看默认资源)
//	report.{id}      单个报表
//	report.manage    报表的建/改/删 (rw)
//	dsn              数据源 (读: 查看/取结构; 写: 增删改)
const (
	resReport       = "report"
	resReportManage = "report.manage"
	resDsn          = "dsn"
)

// Setup 注册报表系统的 REST 接口与前端页面。
func Setup(e *gin.Engine) {
	registerUI(e)
	registerMCP(e)

	api := e.Group("/api")

	// ---- 公开接口 (无需登录) ----
	api.POST("/auth/register", register) // 首位注册即 admin; 表非空后拒绝
	api.POST("/auth/login", login)
	api.GET("/auth/bootstrap", authBootstrap) // 是否还没有任何用户 (前端决定显示注册还是登录)

	// 公开分享: 凭不可枚举的 share_token 访问, 无需登录 (仅 is_public 报表)
	api.GET("/public/report/:token", publicGetReport)
	api.POST("/public/report/:token/run", publicRunReport)

	// ---- 需要登录 ----
	authed := api.Group("", auth.Middleware())
	{
		authed.GET("/auth/me", me)
		authed.POST("/auth/logout", logout)

		// API Token (供 MCP 等程序化访问); 仅管理本人令牌, 明文只在创建时返回一次
		authed.GET("/token", listAPITokens)
		authed.POST("/token", createAPIToken)
		authed.DELETE("/token/:id", deleteAPIToken)

		// 数据源: 读需 dsn:r, 写需 dsn:rw
		// 列数据源: handler 内按权限过滤 (含仅 dsn.<id>:r 的用户)
		authed.GET("/dsn", listDsn)
		authed.POST("/dsn", auth.Require(resDsn, "rw"), createDsn)
		authed.PUT("/dsn/:id", auth.Require(resDsn, "rw"), updateDsn)
		authed.DELETE("/dsn/:id", auth.Require(resDsn, "rw"), deleteDsn)
		authed.POST("/dsn/test", auth.Require(resDsn, "rw"), testDsn)
		// schema 探查: 在 handler 内按 dsn.<id> 细粒度校验 (中间件拿不到 :name)
		authed.GET("/dsn/:name/databases", listDatabases)
		authed.GET("/dsn/:name/tables", listTables)
		authed.GET("/dsn/:name/columns", listColumns)

		// 报表: 列表按权限过滤; 单个报表按 report.{id} 校验; 增删改需 report.manage:rw
		authed.GET("/report", listReports)
		authed.GET("/report/:id", getReport)
		authed.POST("/report", auth.Require(resReportManage, "rw"), createReport)
		authed.PUT("/report/:id", auth.Require(resReportManage, "rw"), updateReport)
		authed.DELETE("/report/:id", auth.Require(resReportManage, "rw"), deleteReport)
		authed.POST("/report/:id/publish", auth.Require(resReportManage, "rw"), publishReport)
		// 版本历史 / 回滚 (需 report.manage:rw)
		authed.GET("/report/:id/version", auth.Require(resReportManage, "rw"), listReportVersions)
		authed.GET("/report/:id/version/:vid", auth.Require(resReportManage, "rw"), getReportVersion)
		authed.POST("/report/:id/version/:vid/rollback", auth.Require(resReportManage, "rw"), rollbackReport)
		authed.POST("/report/:id/run", runReport)
		authed.POST("/report/preview", auth.Require(resReportManage, "rw"), previewReport)
		authed.POST("/report/ai", auth.Require(resReportManage, "rw"), aiModify)
		authed.POST("/report/ai/stream", auth.Require(resReportManage, "rw"), aiModifyStream)
		// 分享开关: 需 report.manage:rw
		authed.POST("/report/:id/share", auth.Require(resReportManage, "rw"), setReportShare)
		// 拖拽排序/移动: 批量更新 parent_id + sort
		authed.POST("/report/reorder", auth.Require(resReportManage, "rw"), reorderReports)

		// 报表定时订阅 (飞书/企微推送): 需 report.manage:rw
		// 全局订阅管理页列表 (静态段, 须排在 /report/:id 相关路由前)
		authed.GET("/report/subscriptions", auth.Require(resReportManage, "rw"), listAllSubscriptions)
		authed.GET("/report/:id/subscription", auth.Require(resReportManage, "rw"), listSubscriptions)
		authed.POST("/report/:id/subscription", auth.Require(resReportManage, "rw"), createSubscription)
		authed.GET("/report/:id/subscription/:sid", auth.Require(resReportManage, "rw"), getSubscription)
		authed.PUT("/report/:id/subscription/:sid", auth.Require(resReportManage, "rw"), updateSubscription)
		authed.DELETE("/report/:id/subscription/:sid", auth.Require(resReportManage, "rw"), deleteSubscription)
		authed.POST("/report/:id/subscription/:sid/test", auth.Require(resReportManage, "rw"), testSubscription)

		// 用户 / 角色管理: 仅管理员
		admin := authed.Group("", auth.RequireAdmin())
		{
			admin.GET("/user", listUsersAPI)
			admin.POST("/user", createUserAPI)
			admin.PUT("/user/:id", updateUserAPI)
			admin.DELETE("/user/:id", deleteUserAPI)

			admin.GET("/role", listRolesAPI)
			admin.POST("/role", createRoleAPI)
			admin.PUT("/role/:id", updateRoleAPI)
			admin.DELETE("/role/:id", deleteRoleAPI)
		}
	}
}

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

func fail(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"code": 1, "msg": msg})
}

func paramID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "无效的 id")
		return 0, false
	}
	return id, true
}

// ---- DSN ----

func listDsn(c *gin.Context) {
	list, err := dao.ListDsn()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 按权限过滤: 只回调用者可读的数据源 (否则名字泄漏)。全局 dsn:r 会全部命中。
	p := auth.PermOf(c)
	nameToID := make(map[string]int, len(list))
	for _, d := range list {
		nameToID[d.Name] = d.Id
	}
	filtered := make([]*dao.DsnRecord, 0, len(list))
	for _, d := range list {
		if p.DsnReadable(d.Name, nameToID) {
			filtered = append(filtered, d)
		}
	}
	ok(c, filtered)
}

func createDsn(c *gin.Context) {
	var r dao.DsnRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	id, err := dao.CreateDsn(&r)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

func updateDsn(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	var r dao.DsnRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// 取旧记录以便失效旧的连接/隧道缓存 (名称可能变更)
	if old, err := dao.GetDsnByID(id); err == nil {
		datasource.Invalidate(old.Name)
	}
	if err := dao.UpdateDsnByID(id, r.Record()); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	datasource.Invalidate(r.Name)
	ok(c, gin.H{"id": id})
}

func deleteDsn(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if old, err := dao.GetDsnByID(id); err == nil {
		datasource.Invalidate(old.Name)
	}
	if err := dao.DeleteDsnByID(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

func testDsn(c *gin.Context) {
	var r dao.DsnRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := datasource.PingRecord(&r); err != nil {
		fail(c, http.StatusBadRequest, "连接失败: "+err.Error())
		return
	}
	ok(c, gin.H{"connected": true})
}

// listDatabases 返回某数据源下可见的数据库 / schema。
func listDatabases(c *gin.Context) {
	name := c.Param("name")
	if !requireDsnReadByName(c, name) {
		return
	}
	dbs, err := datasource.ListDatabases(name)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, dbs)
}

// listTables 返回某数据源指定库下的所有表名 (?db=xxx, 可空)。
func listTables(c *gin.Context) {
	name := c.Param("name")
	if !requireDsnReadByName(c, name) {
		return
	}
	tables, err := datasource.ListTables(name, c.Query("db"))
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, tables)
}

// listColumns 返回某库某表的字段定义 (?db=xxx&table=xxx)。
func listColumns(c *gin.Context) {
	name := c.Param("name")
	if !requireDsnReadByName(c, name) {
		return
	}
	table := c.Query("table")
	if table == "" {
		fail(c, http.StatusBadRequest, "缺少 table 参数")
		return
	}
	cols, err := datasource.TableColumns(name, c.Query("db"), table)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, cols)
}

// dsnAuthzOf 构造当前请求的数据源授权闭包 (按调用者权限 + 当前 name->id 映射)。
// 出错时返回一个一律拒绝的闭包, 保证 fail-closed。
func dsnAuthzOf(c *gin.Context) func(string) error {
	p := auth.PermOf(c)
	nameToID, err := auth.LoadDsnNameToID()
	if err != nil {
		return func(string) error { return err }
	}
	return p.DsnAuthz(nameToID)
}

// requireDsnReadByName 校验当前用户对某数据源 (按名) 有读权限; 无则写 403 并返回 false。
func requireDsnReadByName(c *gin.Context, name string) bool {
	if err := dsnAuthzOf(c)(name); err != nil {
		fail(c, http.StatusForbidden, err.Error())
		return false
	}
	return true
}

// ---- Report ----

// reportReadable 复用 auth 包的祖先链判权 (api 与 mcpserver 共用同一逻辑)。
func reportReadable(p *auth.Permission, id int, parents map[int]int, mode string) bool {
	return p.ReportReadable(id, parents, mode)
}

// loadParentMap 取 id->parent_id 映射 + 报表全量列表 (用于祖先链判权与树过滤)。
func loadParentMap() (map[int]int, []*dao.ReportRecord, error) {
	list, err := dao.ListReports()
	if err != nil {
		return nil, nil, err
	}
	parents := make(map[int]int, len(list))
	for _, r := range list {
		parents[r.Id] = r.ParentID
	}
	return parents, list, nil
}

// canReadReport 判断当前用户能否读某报表 (含祖先文件夹递归授权)。
func canReadReport(c *gin.Context, id int) bool {
	parents, _, err := loadParentMap()
	if err != nil {
		return false
	}
	return reportReadable(auth.PermOf(c), id, parents, "r")
}

func listReports(c *gin.Context) {
	parents, list, err := loadParentMap()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	p := auth.PermOf(c)
	// 先标记每个可直接读的节点
	readable := make(map[int]bool, len(list))
	for _, r := range list {
		if reportReadable(p, r.Id, parents, "r") {
			readable[r.Id] = true
		}
	}
	// 文件夹: 若其任一后代可读, 则文件夹也需返回 (否则树断链)
	for _, r := range list {
		if r.IsFolder() && !readable[r.Id] && folderHasReadableChild(r.Id, list, readable) {
			readable[r.Id] = true
		}
	}
	filtered := make([]*dao.ReportRecord, 0, len(list))
	for _, r := range list {
		if readable[r.Id] {
			filtered = append(filtered, r)
		}
	}
	ok(c, filtered)
}

// folderHasReadableChild 判断某文件夹是否含可读后代 (递归)。
func folderHasReadableChild(folderID int, list []*dao.ReportRecord, readable map[int]bool) bool {
	for _, r := range list {
		if r.ParentID != folderID {
			continue
		}
		if readable[r.Id] {
			return true
		}
		if r.IsFolder() && folderHasReadableChild(r.Id, list, readable) {
			return true
		}
	}
	return false
}

func getReport(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !canReadReport(c, id) {
		fail(c, http.StatusForbidden, "无权访问该报表")
		return
	}
	r, err := dao.GetReportByID(id)
	if err != nil {
		if err == xdb.ErrNotFound {
			fail(c, http.StatusNotFound, "报表不存在")
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, r)
}

func createReport(c *gin.Context) {
	var r dao.ReportRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	id, err := dao.CreateReport(&r)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

func updateReport(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	var r dao.ReportRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := dao.UpdateReportByID(id, r.Record()); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// publishReport 发布: 把开发版草稿同步到发布版 (对查看者生效), 并记一条版本快照。
func publishReport(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if err := dao.PublishReport(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 记录发布版本 (失败不阻断发布本身, 仅影响历史)
	if r, err := dao.GetReportByID(id); err == nil {
		uid, nick := currentUser(c)
		_ = dao.AddReportVersion(id, r.Content, uid, nick)
	}
	ok(c, gin.H{"id": id})
}

// currentUser 取当前登录用户的 id 与昵称 (用于记录操作人)。
func currentUser(c *gin.Context) (int, string) {
	u := auth.UserOf(c)
	if u == nil {
		return 0, ""
	}
	nick := u.Nick
	if nick == "" {
		nick = u.Username
	}
	return u.Id, nick
}

// listReportVersions 返回某报表的发布版本历史 (不含内容)。
func listReportVersions(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	list, err := dao.ListReportVersions(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, list)
}

// getReportVersion 返回单条版本 (含内容), 用于查看/回滚预览。
func getReportVersion(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	vid, ok2 := paramVID(c)
	if !ok2 {
		return
	}
	v, err := dao.GetReportVersion(vid)
	if err != nil || v.ReportID != id {
		fail(c, http.StatusNotFound, "版本不存在")
		return
	}
	ok(c, v)
}

// rollbackReport 回滚: 把某历史版本内容写入开发版草稿 (需再发布才上线)。
func rollbackReport(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	vid, ok2 := paramVID(c)
	if !ok2 {
		return
	}
	v, err := dao.GetReportVersion(vid)
	if err != nil || v.ReportID != id {
		fail(c, http.StatusNotFound, "版本不存在")
		return
	}
	if err := dao.UpdateReportByID(id, xdb.Record{"dev_content": v.Content}); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id, "version_id": vid})
}

// paramVID 解析路径参数 :vid (版本 id)。
func paramVID(c *gin.Context) (int, bool) {
	vid, err := strconv.Atoi(c.Param("vid"))
	if err != nil || vid <= 0 {
		fail(c, http.StatusBadRequest, "无效的版本 id")
		return 0, false
	}
	return vid, true
}

func deleteReport(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	// 文件夹非空时禁止删除 (需先清空或移走子节点)
	if n, err := dao.CountReportChildren(id); err == nil && n > 0 {
		fail(c, http.StatusBadRequest, "文件夹非空, 请先移走或删除其中的报表")
		return
	}
	if err := dao.DeleteReportByID(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// reorderReq 是一批受拖拽影响的节点的新位置。
type reorderReq struct {
	Items []struct {
		ID       int `json:"id"`
		ParentID int `json:"parent_id"`
		Sort     int `json:"sort"`
	} `json:"items"`
}

// reorderReports 批量更新节点的 parent_id 与 sort (拖拽后持久化)。
// 仅改这两个字段, 不影响 content 等。校验目标父级必须是文件夹 (或根 0)。
func reorderReports(c *gin.Context) {
	var req reorderReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Items) == 0 {
		ok(c, gin.H{"updated": 0})
		return
	}

	// 预取全部报表, 校验父级合法性 (父级必须存在且为 folder; 0 表示根)
	all, err := dao.ListReports()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	typeOf := make(map[int]string, len(all))
	parentOf := make(map[int]int, len(all))
	for _, r := range all {
		typeOf[r.Id] = r.Type
		parentOf[r.Id] = r.ParentID
	}

	// isDescendant 判断 ancestor 是否为 node 的祖先 (用更新后的 parentOf, 防环)。
	isDescendant := func(node, ancestor int, p map[int]int) bool {
		seen := map[int]bool{}
		for cur := p[node]; cur != 0 && !seen[cur]; cur = p[cur] {
			seen[cur] = true
			if cur == ancestor {
				return true
			}
		}
		return false
	}

	for _, it := range req.Items {
		if it.ParentID != 0 {
			t, ok := typeOf[it.ParentID]
			if !ok || t != "folder" {
				fail(c, http.StatusBadRequest, "只能移动到文件夹下")
				return
			}
			if it.ParentID == it.ID {
				fail(c, http.StatusBadRequest, "不能移动到自身")
				return
			}
			// 不能把文件夹移动到它自己的后代下 (成环)
			if isDescendant(it.ParentID, it.ID, parentOf) {
				fail(c, http.StatusBadRequest, "不能移动到自己的子目录下")
				return
			}
		}
		parentOf[it.ID] = it.ParentID // 反映本次变更, 供后续项的环检测
		if err := dao.MoveReport(it.ID, it.ParentID, it.Sort); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	}
	ok(c, gin.H{"updated": len(req.Items)})
}

// ---- 执行 ----

type runRequest struct {
	Params map[string]string `json:"params"`
}

func runReport(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !canReadReport(c, id) {
		fail(c, http.StatusForbidden, "无权访问该报表")
		return
	}
	r, err := dao.GetReportByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, "报表不存在")
		return
	}
	var req runRequest
	_ = c.ShouldBindJSON(&req)

	result, err := engine.NewRunner(r.DSN).
		WithNoCache(noCacheParam(req.Params)).
		WithAuthz(dsnAuthzOf(c)). // 按调用者权限校验报表触达的每个数据源
		Run(r.Content, req.Params)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	settings := r.ParseSettings()
	result.AutoRefresh = settings.AutoRefresh       // 报表级自动刷新
	result.PrependContent = settings.PrependContent // 页面顶部 HTML
	ok(c, result)
}

// noCacheParam 判断请求是否要求旁路查询缓存 (前端"刷新"按钮传 _nocache=1)。
func noCacheParam(params map[string]string) bool {
	v := params["_nocache"]
	return v != "" && v != "0" && v != "false"
}

type previewRequest struct {
	DSN     string            `json:"dsn"`
	Content string            `json:"content"`
	Params  map[string]string `json:"params"`
}

// previewReport 直接对传入的模板内容执行 (用于编辑/AI 修改后的实时预览)。
func previewReport(c *gin.Context) {
	var req previewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// 预览始终取实时数据, 旁路缓存; 同样按调用者权限校验数据源。
	result, err := engine.NewRunner(req.DSN).WithNoCache(true).WithAuthz(dsnAuthzOf(c)).Run(req.Content, req.Params)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, result)
}

// ---- AI ----

type aiRequest struct {
	Content     string    `json:"content"`
	Instruction string    `json:"instruction"`
	History     []ai.Turn `json:"history"`
	Schema      string    `json:"schema"` // 可选: 数据源/表结构上下文, 帮助 AI 生成正确 SQL
}

func aiModify(c *gin.Context) {
	var req aiRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Instruction == "" {
		fail(c, http.StatusBadRequest, "instruction 不能为空")
		return
	}
	content, err := ai.ModifyTemplate(c.Request.Context(), req.History, req.Content, req.Instruction, req.Schema)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"content": content})
}

func aiModifyStream(c *gin.Context) {
	var req aiRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if req.Instruction == "" {
		fail(c, http.StatusBadRequest, "instruction 不能为空")
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	send := func(event string, data any) error {
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", event, b); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}

	_ = send("status", gin.H{"text": "正在生成修改建议..."})
	proposal, err := ai.ProposeTemplate(c.Request.Context(), req.History, req.Content, req.Instruction, req.Schema, func(delta string) error {
		if delta == "" {
			return nil
		}
		return send("delta", gin.H{"text": delta})
	})
	if err != nil {
		_ = send("error", gin.H{"message": err.Error()})
		return
	}
	_ = send("tool_call", gin.H{"used": proposal.ToolCall})
	_ = send("proposal", proposal)
}

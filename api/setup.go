package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"xproxy/dao"
	"xproxy/internal/ai"
	"xproxy/internal/auth"
	"xproxy/internal/datasource"
	"xproxy/internal/engine"

	"github.com/daodao97/xgo/xapp"
	"github.com/daodao97/xgo/xdb"
	"github.com/daodao97/xgo/xlog"
	"github.com/gin-gonic/gin"
)

// 资源串约定:
//
//	report           全部报表 (r 读 / w 写, 通常带 R 递归覆盖所有报表)
//	report.{id}      单个报表或文件夹 (文件夹带 R 递归覆盖后代)
//	dsn              数据源 (读: 查看/取结构; 写: 增删改)
//
// 报表写权限是单一维度: 有 report*:w 即可写对应范围的报表, 无需再单列"管理"开关。
const resDsn = "dsn"

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

		// 报表权限单一维度: report / report.{id} 的 r/w (含文件夹 R 递归)。
		// - 读: 列表按权限过滤; 单报表校验 report.{id}:r。
		// - 写: 带 id 的接口在 handler 内按 report.{id}:w 判权; 无 id 的写入口
		//   先用 RequireReportWriter 校验"是否报表开发者", 根级创建/移动再要求 report:w。
		writer := auth.RequireReportWriter()
		authed.GET("/report", listReports)
		authed.GET("/report/:id", getReport)
		authed.POST("/report", writer, createReport)
		authed.PUT("/report/:id", writer, updateReport)
		authed.DELETE("/report/:id", writer, deleteReport)
		authed.POST("/report/:id/publish", writer, publishReport)
		// 版本历史 / 回滚: 读版本需 report.{id}:r, 回滚需 report.{id}:w (handler 内判)
		authed.GET("/report/:id/version", listReportVersions)
		authed.GET("/report/:id/version/:vid", getReportVersion)
		authed.POST("/report/:id/version/:vid/rollback", writer, rollbackReport)
		authed.POST("/report/:id/run", runReport)
		authed.POST("/report/preview", writer, previewReport)
		authed.POST("/report/ai", writer, aiModify)
		authed.POST("/report/ai/stream", writer, aiModifyStream)
		// 分享开关 / 侧边菜单可见性: handler 内按 report.{id}:w 判权
		authed.POST("/report/:id/share", writer, setReportShare)
		authed.POST("/report/:id/visible", writer, setReportVisible)
		// 拖拽排序/移动: handler 内对每个被移动节点 + 目标父级按 report.{id}:w 判权
		authed.POST("/report/reorder", writer, reorderReports)

		// 报表定时任务 (飞书/企微推送): 带 id 的在 handler 内按 report.{id}:w 判权。
		// 全局任务管理页列表 (静态段, 须排在 /report/:id 相关路由前) 需报表写权限。
		authed.GET("/report/schedules", writer, listAllSchedules)
		authed.GET("/report/:id/schedule", listSchedules)
		authed.POST("/report/:id/schedule", createSchedule)
		authed.GET("/report/:id/schedule/:sid", getSchedule)
		authed.PUT("/report/:id/schedule/:sid", updateSchedule)
		authed.DELETE("/report/:id/schedule/:sid", deleteSchedule)
		authed.POST("/report/:id/schedule/:sid/test", testSchedule)

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

// devEnv 判定是否 dev 环境; 用函数变量便于测试覆写。默认读 xapp.IsDev(), 它以
// xapp.Args.AppEnv 为准 —— 同时覆盖 --app-env 标志与 APP_ENV 环境变量 (与 cmd/main.go 一致),
// 避免只认环境变量导致 `--app-env dev` 下 5xx 被误泛化。
var devEnv = xapp.IsDev

// fail 统一错误出口。4xx (客户端/业务错误, 如"报表不存在"/参数非法) 的 msg 面向用户、可控,
// 原样返回; 5xx (内部错误, msg 常为 DB/连接原文) 会泄露主机/库名/连接细节, 故生产环境记日志
// 后返回泛化文案, 仅 dev 透出原文。
func fail(c *gin.Context, status int, msg string) {
	if status >= http.StatusInternalServerError && !devEnv() {
		xlog.Error("api internal error",
			xlog.String("path", c.FullPath()),
			xlog.Int("status", status),
			xlog.String("detail", msg))
		msg = "服务器内部错误, 请稍后重试"
	}
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
	// 取旧记录: 失效旧连接/隧道缓存 (名称可能变更) + 补回前端未改动的脱敏凭据 (**** 占位)。
	if old, err := dao.GetDsnByID(id); err == nil {
		datasource.Invalidate(old.Name)
		r.MergeSecretsFrom(old)
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
	// 编辑既有数据源时 (带 id), 前端表单里的凭据是脱敏占位; 用库中原值补回后再连。
	if r.Id != 0 {
		if old, err := dao.GetDsnByID(r.Id); err == nil {
			r.MergeSecretsFrom(old)
		}
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

func authorizeReportContentDSNs(c *gin.Context, dsn, content string) ([]string, bool) {
	authz := dsnAuthzOf(c)
	dsns := engine.CollectDSNs(dsn, content)
	denied := make([]string, 0)
	for _, name := range dsns {
		if err := authz(name); err != nil {
			denied = append(denied, name)
		}
	}
	if len(denied) == 0 {
		return dsns, true
	}
	sort.Strings(denied)
	fail(c, http.StatusForbidden, "报表内容引用了无权使用的数据源: "+strings.Join(denied, ", "))
	return nil, false
}

func approveReportDSNs(reportID int, settings string, dsns []string) error {
	return dao.UpdateReportByID(reportID, xdb.Record{"settings": dao.SettingsWithApprovedDSNs(settings, dsns)})
}

// ---- Report ----

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

func canReport(c *gin.Context, id int, mode string) bool {
	parents, _, err := loadParentMap()
	if err != nil {
		return false
	}
	return auth.PermOf(c).ReportReadable(id, parents, mode)
}

func requireReportPerm(c *gin.Context, id int, mode, msg string) bool {
	if !canReport(c, id, mode) {
		fail(c, http.StatusForbidden, msg)
		return false
	}
	return true
}

func requireReportWrite(c *gin.Context, id int) bool {
	return requireReportPerm(c, id, "w", "无权修改该报表")
}
func requireReportRead(c *gin.Context, id int) bool {
	return requireReportPerm(c, id, "r", "无权访问该报表")
}

func requireRootReportWrite(c *gin.Context) bool {
	if p := auth.PermOf(c); p != nil && p.Check("report", "w") {
		return true
	}
	fail(c, http.StatusForbidden, "无权在根目录下操作报表")
	return false
}

// requireWritableParent 校验目标父级 parentID 合法且用户可写 (create/update/move 共用)。
// parentID==0 表示根目录, 需全局 report:w; 单报表写权限不能扩展到根级全局空间。
// 无则写对应错误并返回 false。
func requireWritableParent(c *gin.Context, parentID int) bool {
	if parentID == 0 {
		return requireRootReportWrite(c)
	}
	parents, list, err := loadParentMap()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return false
	}
	var parent *dao.ReportRecord
	for _, x := range list {
		if x.Id == parentID {
			parent = x
			break
		}
	}
	if parent == nil || !parent.IsFolder() {
		fail(c, http.StatusBadRequest, "父级不存在或不是文件夹")
		return false
	}
	if !auth.PermOf(c).ReportReadable(parentID, parents, "w") {
		fail(c, http.StatusForbidden, "无权在该目录下操作报表")
		return false
	}
	return true
}

type reportViewRecord struct {
	*dao.ReportRecord
	CanRead  bool `json:"can_read,omitempty"`
	CanWrite bool `json:"can_write,omitempty"`
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
	writable := make(map[int]bool, len(list))
	for _, r := range list {
		if p.ReportReadable(r.Id, parents, "r") {
			readable[r.Id] = true
		}
		if p.ReportReadable(r.Id, parents, "w") {
			writable[r.Id] = true
		}
	}
	// 文件夹: 若其任一后代可读, 则文件夹也需返回 (否则树断链)
	for _, r := range list {
		if r.IsFolder() && !readable[r.Id] && folderHasReadableChild(r.Id, list, readable) {
			readable[r.Id] = true
		}
	}
	filtered := make([]reportViewRecord, 0, len(list))
	for _, r := range list {
		if readable[r.Id] {
			filtered = append(filtered, reportViewRecord{ReportRecord: r, CanRead: readable[r.Id], CanWrite: writable[r.Id]})
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
	if !requireReportRead(c, id) {
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
	ok(c, reportViewRecord{ReportRecord: r, CanRead: true, CanWrite: canReport(c, id, "w")})
}

func createReport(c *gin.Context) {
	var r dao.ReportRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// 指定父级时: 父级须为存在的文件夹且用户对其有写权限 (防在无权目录下建报表)。
	if !requireWritableParent(c, r.ParentID) {
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
	if !requireReportWrite(c, id) {
		return
	}
	var r dao.ReportRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// Record() 会写 parent_id, 即更新也能移动报表; 若本次改变了父级, 需与 create/reorder 一致
	// 校验新父级合法且可写, 否则可借 PUT 把报表移进无权目录或挂到非文件夹节点下。
	// 仅在父级真正变更时校验: 否则对 report.<id>:w (父文件夹无权) 的用户会误伤正常编辑。
	if old, err := dao.GetReportByID(id); err == nil && old.ParentID != r.ParentID {
		if !requireWritableParent(c, r.ParentID) {
			return
		}
	}
	if err := dao.UpdateReportByID(id, r.Record()); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id})
}

// setReportVisible 开启/关闭某报表在侧边菜单的可见性。
func setReportVisible(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	var req struct {
		Visible bool `json:"visible"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	if err := dao.SetReportVisible(id, req.Visible); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": id, "visible": req.Visible})
}

// publishReport 发布: 把开发版草稿同步到发布版 (对查看者生效), 并记一条版本快照。
func publishReport(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	r, err := dao.GetReportByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, "报表不存在")
		return
	}
	if !r.IsFolder() {
		dsns, ok := authorizeReportContentDSNs(c, r.DSN, r.DevContent)
		if !ok {
			return
		}
		r.Settings = dao.SettingsWithApprovedDSNs(r.Settings, dsns)
		if err := dao.PublishReportContentWithSettings(id, r.DevContent, r.Settings); err != nil {
			fail(c, http.StatusInternalServerError, err.Error())
			return
		}
	} else if err := dao.PublishReport(id); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 记录发布版本 (失败不阻断发布本身, 仅影响历史)
	uid, nick := currentUser(c)
	_ = dao.AddReportVersion(id, r.DevContent, uid, nick)
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
	if !requireReportRead(c, id) {
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
	if !requireReportRead(c, id) {
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
	if !requireReportWrite(c, id) {
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
	if !requireReportWrite(c, id) {
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
	origParents := make(map[int]int, len(all))
	for _, r := range all {
		typeOf[r.Id] = r.Type
		parentOf[r.Id] = r.ParentID
		origParents[r.Id] = r.ParentID
	}

	// 每个被移动的节点都要有写权限 (按其当前位置的祖先链判定), 且目标父级也要可写
	// (移动到根目录需全局 report:w), 否则报表开发者可把无权报表移进/移出自己的目录。
	p := auth.PermOf(c)
	for _, it := range req.Items {
		if !p.ReportReadable(it.ID, origParents, "w") {
			fail(c, http.StatusForbidden, "无权移动该报表")
			return
		}
		if it.ParentID == 0 {
			if !requireRootReportWrite(c) {
				return
			}
		} else if !p.ReportReadable(it.ParentID, origParents, "w") {
			fail(c, http.StatusForbidden, "无权移动到该目标目录")
			return
		}
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
	if !requireReportRead(c, id) {
		return
	}
	r, err := dao.GetReportByID(id)
	if err != nil {
		fail(c, http.StatusNotFound, "报表不存在")
		return
	}
	var req runRequest
	_ = c.ShouldBindJSON(&req)

	noCache := noCacheParam(req.Params)
	result, err := engine.NewRunner(r.DSN).
		WithNoCache(noCache).
		WithAuthz(dsnAuthzOf(c)). // 按调用者权限校验报表触达的每个数据源
		WithTrace(reportRunTrace(c, id, r.Name, noCache)).
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

func reportRunTrace(c *gin.Context, reportID int, reportName string, noCache bool) engine.RunTraceFunc {
	uid, nick := currentUser(c)
	path := c.FullPath()
	if path == "" {
		path = c.Request.URL.Path
	}
	requestID := c.GetHeader("X-Request-ID")
	return func(ev engine.RunTraceEvent) {
		switch ev.Phase {
		case "report":
			xlog.Info("report run timing",
				xlog.String("event", "report_run"),
				xlog.String("path", path),
				xlog.String("request_id", requestID),
				xlog.Int("report_id", reportID),
				xlog.String("report_name", reportName),
				xlog.Int("user_id", uid),
				xlog.String("user", nick),
				xlog.Any("no_cache", noCache),
				xlog.String("duration", ev.Duration.String()),
				xlog.Int("duration_ms", int(ev.Duration.Milliseconds())),
				xlog.Int("parsed_blocks", ev.ParsedBlocks),
				xlog.Int("output_blocks", ev.OutputBlocks),
				xlog.String("error", ev.Error),
			)
		case "block_parse":
			xlog.Info("report block parse timing",
				xlog.String("event", "report_block_parse"),
				xlog.String("path", path),
				xlog.String("request_id", requestID),
				xlog.Int("report_id", reportID),
				xlog.String("report_name", reportName),
				xlog.Int("user_id", uid),
				xlog.String("user", nick),
				xlog.Any("no_cache", noCache),
				xlog.Int("block_index", ev.BlockIndex),
				xlog.String("block_kind", ev.BlockKind),
				xlog.String("block_id", ev.BlockID),
				xlog.String("block_title", ev.BlockTitle),
				xlog.String("duration", ev.Duration.String()),
				xlog.Int("duration_ms", int(ev.Duration.Milliseconds())),
				xlog.Int("body_len", ev.SQLLen),
			)
		case "block_exec":
			xlog.Info("report block exec timing",
				xlog.String("event", "report_block_exec"),
				xlog.String("path", path),
				xlog.String("request_id", requestID),
				xlog.Int("report_id", reportID),
				xlog.String("report_name", reportName),
				xlog.Int("user_id", uid),
				xlog.String("user", nick),
				xlog.Any("no_cache", noCache),
				xlog.Int("block_index", ev.BlockIndex),
				xlog.String("block_kind", ev.BlockKind),
				xlog.String("block_id", ev.BlockID),
				xlog.String("block_title", ev.BlockTitle),
				xlog.String("dsn", ev.DSN),
				xlog.String("duration", ev.Duration.String()),
				xlog.Int("duration_ms", int(ev.Duration.Milliseconds())),
				xlog.Int("rows", ev.Rows),
				xlog.Int("columns", ev.Columns),
				xlog.Int("sql_len", ev.SQLLen),
				xlog.Int("produced_blocks", ev.ProducedBlocks),
				xlog.String("error", ev.Error),
			)
		}
	}
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

package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"xproxy/dao"
	"xproxy/internal/auth"
	"xproxy/internal/schedule"

	"github.com/gin-gonic/gin"
)

// paramSID 解析路径参数 :sid (定时任务 id)。
func paramSID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("sid"))
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "无效的定时任务 id")
		return 0, false
	}
	return id, true
}

// maskWebhook 脱敏 webhook (仅保留尾部), 列表回传时使用, 避免泄露完整地址。
func maskWebhook(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	r := []rune(s)
	if len(r) <= 8 {
		return "****"
	}
	return "****" + string(r[len(r)-6:])
}

// scheduleView 是定时任务对外视图 (webhook 脱敏)。
type scheduleView struct {
	*dao.ScheduleRecord
	Webhook    string `json:"webhook"`               // 覆盖为脱敏值
	ReportName string `json:"report_name,omitempty"` // 关联报表名 (管理页用)
}

func validateScheduleReportAccess(c *gin.Context, reportID int) bool {
	report, err := dao.GetReportByID(reportID)
	if err != nil {
		fail(c, http.StatusNotFound, "报表不存在")
		return false
	}
	if report.IsFolder() {
		fail(c, http.StatusBadRequest, "文件夹不能配置定时任务")
		return false
	}
	dsns, ok := authorizeReportContentDSNs(c, report.DSN, report.Content)
	if !ok {
		return false
	}
	if err := approveReportDSNs(reportID, report.Settings, dsns); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return false
	}
	return true
}

// listAllSchedules 列出定时任务管理页数据。RequireReportWriter 守卫只保证"是报表开发者",
// 但本接口跨所有报表, 故按 report.{id}:w 逐条过滤: 只回用户有权写的报表的任务,
// 否则仅持某一报表写权限者也能看到全站其他报表的调度信息 (报表名/参数/状态/脱敏 webhook)。
func listAllSchedules(c *gin.Context) {
	subs, err := dao.ListAllSchedules()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	parents, list, err := loadParentMap()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	names := make(map[int]string, len(list))
	for _, r := range list {
		names[r.Id] = r.Name
	}
	p := auth.PermOf(c)
	out := make([]scheduleView, 0, len(subs))
	for _, s := range subs {
		if !p.ReportReadable(s.ReportID, parents, "w") {
			continue // 无权写该报表 -> 不泄露其调度信息
		}
		out = append(out, scheduleView{
			ScheduleRecord: s,
			Webhook:        maskWebhook(s.Webhook),
			ReportName:     names[s.ReportID],
		})
	}
	ok(c, out)
}

func listSchedules(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	subs, err := dao.ListSchedulesByReport(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]scheduleView, 0, len(subs))
	for _, s := range subs {
		out = append(out, scheduleView{ScheduleRecord: s, Webhook: maskWebhook(s.Webhook)})
	}
	ok(c, out)
}

// getSchedule 返回单条定时任务的完整信息 (含明文 webhook), 供编辑回填。
// handler 内已校验 report.{id}:w, 调用者本就能修改该 webhook, 故返回明文。
func getSchedule(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	sub, err := dao.GetScheduleByID(sid)
	if err != nil || sub.ReportID != id {
		fail(c, http.StatusNotFound, "定时任务不存在")
		return
	}
	ok(c, sub)
}

func createSchedule(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	var r dao.ScheduleRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	r.ReportID = id
	if !validateScheduleReportAccess(c, id) {
		return
	}
	// 仅 webhook 动作才需要校验 webhook; none (只跑不推) 允许空 webhook。
	if r.Action != dao.ActionNone {
		if err := schedule.ValidateWebhook(r.Webhook); err != nil {
			fail(c, http.StatusBadRequest, err.Error())
			return
		}
	}
	newID, err := dao.CreateSchedule(&r)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": newID})
}

func updateSchedule(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	var r dao.ScheduleRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// 校验定时任务归属该报表
	old, err := dao.GetScheduleByID(sid)
	if err != nil || old.ReportID != id {
		fail(c, http.StatusNotFound, "定时任务不存在")
		return
	}
	if !validateScheduleReportAccess(c, id) {
		return
	}
	updates := r.Record()
	// none (只跑不推): 不校验 webhook。否则: 未提交新 webhook (前端拿到的是脱敏值) 时
	// 保留原 webhook 不覆盖; 提交了新值才校验。
	if r.Action == dao.ActionNone {
		delete(updates, "webhook")
	} else if strings.TrimSpace(r.Webhook) == "" || strings.HasPrefix(r.Webhook, "****") {
		delete(updates, "webhook")
	} else if err := schedule.ValidateWebhook(r.Webhook); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	delete(updates, "report_id") // 不允许改归属
	if err := dao.UpdateScheduleByID(sid, updates); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": sid})
}

func deleteSchedule(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	old, err := dao.GetScheduleByID(sid)
	if err != nil || old.ReportID != id {
		fail(c, http.StatusNotFound, "定时任务不存在")
		return
	}
	if err := dao.DeleteScheduleByID(sid); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": sid})
}

// testSchedule 立即执行一次定时任务 (推送到其 webhook), 用于配置后验证。
func testSchedule(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	if !requireReportWrite(c, id) {
		return
	}
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	sub, err := dao.GetScheduleByID(sid)
	if err != nil || sub.ReportID != id {
		fail(c, http.StatusNotFound, "定时任务不存在")
		return
	}
	if !validateScheduleReportAccess(c, id) {
		return
	}
	if err := schedule.Execute(sub); err != nil {
		// 条件未命中不是错误: 返回 200 + 说明, 让前端给提示而非报错。
		if errors.Is(err, schedule.ErrNotTriggered) {
			ok(c, gin.H{"ok": false, "triggered": false, "message": err.Error()})
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "triggered": true})
}

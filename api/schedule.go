package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"xproxy/dao"
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

// listAllSchedules 列出全站所有定时任务 (管理页, 需 report.manage:rw)。
func listAllSchedules(c *gin.Context) {
	subs, err := dao.ListAllSchedules()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 一次性取报表名映射, 避免逐条查库
	names := map[int]string{}
	if reports, err := dao.ListReports(); err == nil {
		for _, r := range reports {
			names[r.Id] = r.Name
		}
	}
	out := make([]scheduleView, 0, len(subs))
	for _, s := range subs {
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
// 调用者已经过 report.manage:rw 守卫, 本就能修改该 webhook, 故返回明文。
func getSchedule(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
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
	var r dao.ScheduleRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	r.ReportID = id
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
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	sub, err := dao.GetScheduleByID(sid)
	if err != nil || sub.ReportID != id {
		fail(c, http.StatusNotFound, "定时任务不存在")
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

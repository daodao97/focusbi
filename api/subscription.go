package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"xproxy/dao"
	"xproxy/internal/subscription"

	"github.com/gin-gonic/gin"
)

// paramSID 解析路径参数 :sid (订阅 id)。
func paramSID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("sid"))
	if err != nil || id <= 0 {
		fail(c, http.StatusBadRequest, "无效的订阅 id")
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

// subView 是订阅对外视图 (webhook 脱敏)。
type subView struct {
	*dao.SubscriptionRecord
	Webhook    string `json:"webhook"`               // 覆盖为脱敏值
	ReportName string `json:"report_name,omitempty"` // 关联报表名 (管理页用)
}

// listAllSubscriptions 列出全站所有订阅 (管理页, 需 report.manage:rw)。
func listAllSubscriptions(c *gin.Context) {
	subs, err := dao.ListAllSubscriptions()
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
	out := make([]subView, 0, len(subs))
	for _, s := range subs {
		out = append(out, subView{
			SubscriptionRecord: s,
			Webhook:            maskWebhook(s.Webhook),
			ReportName:         names[s.ReportID],
		})
	}
	ok(c, out)
}

func listSubscriptions(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	subs, err := dao.ListSubscriptionsByReport(id)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]subView, 0, len(subs))
	for _, s := range subs {
		out = append(out, subView{SubscriptionRecord: s, Webhook: maskWebhook(s.Webhook)})
	}
	ok(c, out)
}

// getSubscription 返回单条订阅的完整信息 (含明文 webhook), 供编辑回填。
// 调用者已经过 report.manage:rw 守卫, 本就能修改该 webhook, 故返回明文。
func getSubscription(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	sub, err := dao.GetSubscriptionByID(sid)
	if err != nil || sub.ReportID != id {
		fail(c, http.StatusNotFound, "订阅不存在")
		return
	}
	ok(c, sub)
}

func createSubscription(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	var r dao.SubscriptionRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	r.ReportID = id
	if err := subscription.ValidateWebhook(r.Webhook); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	newID, err := dao.CreateSubscription(&r)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": newID})
}

func updateSubscription(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	var r dao.SubscriptionRecord
	if err := c.ShouldBindJSON(&r); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	// 校验订阅归属该报表
	old, err := dao.GetSubscriptionByID(sid)
	if err != nil || old.ReportID != id {
		fail(c, http.StatusNotFound, "订阅不存在")
		return
	}
	updates := r.Record()
	// 未提交新 webhook (前端拿到的是脱敏值) 时, 保留原 webhook 不覆盖; 提交了新值才校验。
	if strings.TrimSpace(r.Webhook) == "" || strings.HasPrefix(r.Webhook, "****") {
		delete(updates, "webhook")
	} else if err := subscription.ValidateWebhook(r.Webhook); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	delete(updates, "report_id") // 不允许改归属
	if err := dao.UpdateSubscriptionByID(sid, updates); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": sid})
}

func deleteSubscription(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	old, err := dao.GetSubscriptionByID(sid)
	if err != nil || old.ReportID != id {
		fail(c, http.StatusNotFound, "订阅不存在")
		return
	}
	if err := dao.DeleteSubscriptionByID(sid); err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"id": sid})
}

// testSubscription 立即执行一次订阅 (推送到其 webhook), 用于配置后验证。
func testSubscription(c *gin.Context) {
	id, valid := paramID(c)
	if !valid {
		return
	}
	sid, valid := paramSID(c)
	if !valid {
		return
	}
	sub, err := dao.GetSubscriptionByID(sid)
	if err != nil || sub.ReportID != id {
		fail(c, http.StatusNotFound, "订阅不存在")
		return
	}
	if err := subscription.Execute(sub); err != nil {
		// 条件未命中不是错误: 返回 200 + 说明, 让前端给提示而非报错。
		if errors.Is(err, subscription.ErrNotTriggered) {
			ok(c, gin.H{"ok": false, "triggered": false, "message": err.Error()})
			return
		}
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"ok": true, "triggered": true})
}

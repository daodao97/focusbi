package schedule

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"xproxy/conf"
	"xproxy/dao"
	"xproxy/internal/engine"
)

// ErrNotTriggered 表示带阈值条件的定时任务本次未命中条件, 不推送 (非失败)。
var ErrNotTriggered = errors.New("条件未命中, 未推送")

// ErrSilenced 表示条件命中但处于告警静默期, 跳过推送 (非失败)。
var ErrSilenced = errors.New("命中但在静默期内, 未推送")

// Execute 跑一次定时任务: 取报表 → 执行 → (判定条件) → 渲染 → 推送。
// 返回 nil 表示已推送; 返回 ErrNotTriggered 表示带条件定时任务未命中 (正常跳过)。
func Execute(sub *dao.ScheduleRecord) error {
	if sub == nil {
		return fmt.Errorf("定时任务为空")
	}
	report, err := dao.GetReportByID(sub.ReportID)
	if err != nil {
		return fmt.Errorf("报表不存在: %w", err)
	}

	settings := report.ParseSettings()
	if len(settings.ApprovedDSNs) == 0 {
		return fmt.Errorf("报表未完成数据源预授权, 请重新发布或保存定时任务")
	}
	result, err := engine.NewRunner(report.DSN).
		WithNoCache(true).
		WithAuthz(engine.AllowlistAuthz(settings.ApprovedDSNs)).
		Run(report.Content, sub.Params)
	if err != nil {
		return fmt.Errorf("执行报表失败: %w", err)
	}

	// action=none: 只跑报表 (刷缓存/预热), 不推送。跑成功即完成。
	if sub.Action == dao.ActionNone {
		return nil
	}

	name := report.Name
	if sub.Name != "" {
		name = sub.Name
	}

	// 阈值告警: 配了条件则只有命中才推送; 命中时正文加告警前缀。
	// 静默期: 命中但距上次告警不足 silence_minutes 则跳过, 防持续命中造成告警风暴。
	var alarmPrefix string
	if sub.Condition != nil {
		hit, detail := evalCondition(sub.Condition, result)
		if !hit {
			return fmt.Errorf("%w (%s)", ErrNotTriggered, detail)
		}
		if silenced(sub.Condition.SilenceMinutes, sub.LastAlarmAt, nowFunc()) {
			return fmt.Errorf("%w (%s)", ErrSilenced, detail)
		}
		alarmPrefix = "⚠️ 告警: " + detail + "\n"
	}

	// 报表内嵌波动检测 (@data_fluctuations) 产出的消息, 作为告警前缀并入正文。
	if len(result.Messages) > 0 {
		alarmPrefix += "⚠️ 波动: " + strings.Join(result.Messages, "; ") + "\n"
	}

	text := alarmPrefix + RenderText(name, result, viewURL(report, sub))
	if err := push(sub.Channel, sub.Webhook, text); err != nil {
		return err
	}
	// 告警推送成功后记录时间, 作为下次静默期判断的起点。失败不影响本次结果。
	if sub.Condition != nil && sub.Condition.SilenceMinutes > 0 {
		_ = dao.TouchScheduleAlarm(sub.Id, nowFunc())
	}
	return nil
}

// silenced 判断是否处于告警静默期: 距上次告警不足 minutes 分钟则跳过。
func silenced(minutes int, lastAlarmAt *time.Time, now time.Time) bool {
	return minutes > 0 && lastAlarmAt != nil && now.Sub(*lastAlarmAt) < time.Duration(minutes)*time.Minute
}

// viewURL 构造报表查看链接 (站点地址未配置则返回空, 消息不带链接):
//   - 报表已公开分享 -> 公开查看页 view.html#/<token>
//   - 否则 -> 控制台报表页 (需登录)
func viewURL(report *dao.ReportRecord, sub *dao.ScheduleRecord) string {
	base := conf.Get().SiteBaseURL()
	if base == "" {
		return ""
	}
	if report.IsPublic && report.ShareToken != "" {
		u := base + "/view.html#/" + report.ShareToken
		if q := queryString(sub.Params); q != "" {
			u += "?" + q
		}
		return u
	}
	return fmt.Sprintf("%s/#/reports/%d", base, report.Id)
}

// queryString 把固定参数拼成 url query (简单编码, 仅用于展示链接)。
func queryString(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for k, v := range params {
		if strings.HasPrefix(k, "_") { // 跳过内部参数 (如 _nocache)
			continue
		}
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, "&")
}

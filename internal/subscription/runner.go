package subscription

import (
	"errors"
	"fmt"
	"strings"

	"xproxy/conf"
	"xproxy/dao"
	"xproxy/internal/engine"
)

// ErrNotTriggered 表示带阈值条件的订阅本次未命中条件, 不推送 (非失败)。
var ErrNotTriggered = errors.New("条件未命中, 未推送")

// Execute 跑一次订阅: 取报表 → 执行 → (判定条件) → 渲染 → 推送。
// 返回 nil 表示已推送; 返回 ErrNotTriggered 表示带条件订阅未命中 (正常跳过)。
func Execute(sub *dao.SubscriptionRecord) error {
	if sub == nil {
		return fmt.Errorf("订阅为空")
	}
	report, err := dao.GetReportByID(sub.ReportID)
	if err != nil {
		return fmt.Errorf("报表不存在: %w", err)
	}

	result, err := engine.NewRunner(report.DSN).WithNoCache(true).Run(report.Content, sub.Params)
	if err != nil {
		return fmt.Errorf("执行报表失败: %w", err)
	}

	name := report.Name
	if sub.Name != "" {
		name = sub.Name
	}

	// 阈值告警: 配了条件则只有命中才推送; 命中时正文加告警前缀。
	var alarmPrefix string
	if sub.Condition != nil {
		hit, detail := evalCondition(sub.Condition, result)
		if !hit {
			return fmt.Errorf("%w (%s)", ErrNotTriggered, detail)
		}
		alarmPrefix = "⚠️ 告警: " + detail + "\n"
	}

	// 报表内嵌波动检测 (@data_fluctuations) 产出的消息, 作为告警前缀并入正文。
	if len(result.Messages) > 0 {
		alarmPrefix += "⚠️ 波动: " + strings.Join(result.Messages, "; ") + "\n"
	}

	text := alarmPrefix + RenderText(name, result, viewURL(report, sub))
	return push(sub.Channel, sub.Webhook, text)
}

// viewURL 构造报表查看链接 (站点地址未配置则返回空, 消息不带链接):
//   - 报表已公开分享 -> 公开查看页 view.html#/<token>
//   - 否则 -> 控制台报表页 (需登录)
func viewURL(report *dao.ReportRecord, sub *dao.SubscriptionRecord) string {
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
	return fmt.Sprintf("%s/index.html#/reports/%d", base, report.Id)
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

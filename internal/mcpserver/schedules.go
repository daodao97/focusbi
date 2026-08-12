package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xproxy/dao"
	"xproxy/internal/auth"
	"xproxy/internal/engine"
	"xproxy/internal/schedule"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- 定时任务 MCP 输入/输出 ----

type listSchedulesIn struct {
	ReportID int `json:"report_id,omitempty" jsonschema:"报表 id; 留空列出当前用户有权管理的全部任务"`
}

type listSchedulesOut struct {
	Schedules []*dao.ScheduleRecord `json:"schedules" jsonschema:"定时任务列表; webhook 已脱敏"`
}

type scheduleIDIn struct {
	ID int `json:"id" jsonschema:"定时任务 id"`
}

type getScheduleOut struct {
	Schedule *dao.ScheduleRecord `json:"schedule"`
}

type createScheduleIn struct {
	ReportID  int                    `json:"report_id" jsonschema:"报表 id"`
	Name      string                 `json:"name,omitempty" jsonschema:"任务名称"`
	Cron      string                 `json:"cron" jsonschema:"标准 5 段 cron: 分 时 日 月 周"`
	Action    string                 `json:"action,omitempty" jsonschema:"webhook=推送群机器人; none=只跑不推; 默认 webhook"`
	Channel   string                 `json:"channel,omitempty" jsonschema:"lark 或 wework; 默认 lark"`
	Webhook   string                 `json:"webhook,omitempty" jsonschema:"群机器人 Webhook 完整地址; action=webhook 时必填"`
	Params    map[string]string      `json:"params,omitempty" jsonschema:"预置执行参数, 包含过滤器或任意 URL query 参数"`
	Condition *dao.ScheduleCondition `json:"condition,omitempty" jsonschema:"阈值告警条件; 留空表示每次定时执行都推送"`
	Enabled   *bool                  `json:"enabled,omitempty" jsonschema:"是否启用; 默认 true"`
}

type updateScheduleIn struct {
	ID             int                    `json:"id" jsonschema:"定时任务 id"`
	Name           *string                `json:"name,omitempty"`
	Cron           *string                `json:"cron,omitempty" jsonschema:"标准 5 段 cron: 分 时 日 月 周"`
	Action         *string                `json:"action,omitempty" jsonschema:"webhook 或 none"`
	Channel        *string                `json:"channel,omitempty" jsonschema:"lark 或 wework"`
	Webhook        *string                `json:"webhook,omitempty" jsonschema:"新的群机器人 Webhook 完整地址"`
	Params         *map[string]string     `json:"params,omitempty" jsonschema:"替换全部预置执行参数"`
	Condition      *dao.ScheduleCondition `json:"condition,omitempty" jsonschema:"新的阈值告警条件"`
	ClearCondition bool                   `json:"clear_condition,omitempty" jsonschema:"设为 true 清除阈值条件, 改为定时必推"`
	Enabled        *bool                  `json:"enabled,omitempty"`
}

type testScheduleOut struct {
	OK        bool   `json:"ok"`
	Triggered bool   `json:"triggered" jsonschema:"是否实际执行了推送; action=none 执行成功时也为 true"`
	Message   string `json:"message,omitempty"`
}

func listSchedulesTool(ctx context.Context, _ *mcp.CallToolRequest, in listSchedulesIn) (*mcp.CallToolResult, listSchedulesOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, listSchedulesOut{}, err
	}
	parents, err := auth.LoadReportParents()
	if err != nil {
		return nil, listSchedulesOut{}, err
	}
	var list []*dao.ScheduleRecord
	if in.ReportID > 0 {
		if !pr.perm.ReportReadable(in.ReportID, parents, "w") {
			return nil, listSchedulesOut{}, fmt.Errorf("无权管理该报表的定时任务")
		}
		list, err = dao.ListSchedulesByReport(in.ReportID)
	} else {
		list, err = dao.ListAllSchedules()
	}
	if err != nil {
		return nil, listSchedulesOut{}, err
	}
	out := listSchedulesOut{Schedules: make([]*dao.ScheduleRecord, 0, len(list))}
	for _, item := range list {
		if !pr.perm.ReportReadable(item.ReportID, parents, "w") {
			continue
		}
		copy := *item
		copy.Webhook = maskScheduleWebhook(copy.Webhook)
		out.Schedules = append(out.Schedules, &copy)
	}
	return nil, out, nil
}

func getScheduleTool(ctx context.Context, _ *mcp.CallToolRequest, in scheduleIDIn) (*mcp.CallToolResult, getScheduleOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, getScheduleOut{}, err
	}
	item, err := scheduleForWrite(pr, in.ID)
	if err != nil {
		return nil, getScheduleOut{}, err
	}
	return nil, getScheduleOut{Schedule: item}, nil
}

func createScheduleTool(ctx context.Context, _ *mcp.CallToolRequest, in createScheduleIn) (*mcp.CallToolResult, idOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, idOut{}, err
	}
	if _, err := approveScheduleReport(pr, in.ReportID); err != nil {
		return nil, idOut{}, err
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	item := &dao.ScheduleRecord{
		ReportID: in.ReportID, Name: in.Name, Cron: in.Cron, Action: in.Action,
		Channel: in.Channel, Webhook: in.Webhook, Params: in.Params,
		Condition: in.Condition, Enabled: enabled,
	}
	if err := validateScheduleConfig(item); err != nil {
		return nil, idOut{}, err
	}
	id, err := dao.CreateSchedule(item)
	if err != nil {
		return nil, idOut{}, err
	}
	return nil, idOut{ID: int(id)}, nil
}

func updateScheduleTool(ctx context.Context, _ *mcp.CallToolRequest, in updateScheduleIn) (*mcp.CallToolResult, okOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, okOut{}, err
	}
	item, err := scheduleForWrite(pr, in.ID)
	if err != nil {
		return nil, okOut{}, err
	}
	if _, err := approveScheduleReport(pr, item.ReportID); err != nil {
		return nil, okOut{}, err
	}
	changed := false
	if in.Name != nil {
		item.Name, changed = *in.Name, true
	}
	if in.Cron != nil {
		item.Cron, changed = *in.Cron, true
	}
	if in.Action != nil {
		item.Action, changed = *in.Action, true
	}
	if in.Channel != nil {
		item.Channel, changed = *in.Channel, true
	}
	if in.Webhook != nil {
		item.Webhook, changed = *in.Webhook, true
	}
	if in.Params != nil {
		item.Params, changed = *in.Params, true
	}
	if in.Condition != nil {
		item.Condition, changed = in.Condition, true
	}
	if in.ClearCondition {
		item.Condition, changed = nil, true
	}
	if in.Enabled != nil {
		item.Enabled, changed = *in.Enabled, true
	}
	if !changed {
		return nil, okOut{}, fmt.Errorf("没有要更新的字段")
	}
	if err := validateScheduleConfig(item); err != nil {
		return nil, okOut{}, err
	}
	updates := item.Record()
	delete(updates, "report_id")
	if err := dao.UpdateScheduleByID(item.Id, updates); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

func deleteScheduleTool(ctx context.Context, _ *mcp.CallToolRequest, in scheduleIDIn) (*mcp.CallToolResult, okOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, okOut{}, err
	}
	if _, err := scheduleForWrite(pr, in.ID); err != nil {
		return nil, okOut{}, err
	}
	if err := dao.DeleteScheduleByID(in.ID); err != nil {
		return nil, okOut{}, err
	}
	return nil, okOut{OK: true}, nil
}

func testScheduleTool(ctx context.Context, _ *mcp.CallToolRequest, in scheduleIDIn) (*mcp.CallToolResult, testScheduleOut, error) {
	pr, err := principalFromContext(ctx)
	if err != nil {
		return nil, testScheduleOut{}, err
	}
	item, err := scheduleForWrite(pr, in.ID)
	if err != nil {
		return nil, testScheduleOut{}, err
	}
	if _, err := approveScheduleReport(pr, item.ReportID); err != nil {
		return nil, testScheduleOut{}, err
	}
	if err := schedule.Execute(item); err != nil {
		if errors.Is(err, schedule.ErrNotTriggered) || errors.Is(err, schedule.ErrSilenced) {
			return nil, testScheduleOut{OK: false, Triggered: false, Message: err.Error()}, nil
		}
		return nil, testScheduleOut{}, err
	}
	return nil, testScheduleOut{OK: true, Triggered: true, Message: "定时任务执行成功"}, nil
}

// scheduleForWrite 加载任务并校验调用者对其所属报表有写权限。
func scheduleForWrite(pr *principal, id int) (*dao.ScheduleRecord, error) {
	if id <= 0 {
		return nil, fmt.Errorf("无效的定时任务 id")
	}
	item, err := dao.GetScheduleByID(id)
	if err != nil {
		return nil, err
	}
	if err := requireReportWrite(pr, item.ReportID); err != nil {
		return nil, fmt.Errorf("无权管理该定时任务")
	}
	return item, nil
}

// approveScheduleReport 与 REST 保存/测试任务一致: 校验报表、数据源权限并写入执行预授权。
func approveScheduleReport(pr *principal, reportID int) (*dao.ReportRecord, error) {
	if reportID <= 0 {
		return nil, fmt.Errorf("无效的报表 id")
	}
	if err := requireReportWrite(pr, reportID); err != nil {
		return nil, err
	}
	report, err := dao.GetReportByID(reportID)
	if err != nil {
		return nil, fmt.Errorf("报表不存在: %w", err)
	}
	if report.IsFolder() {
		return nil, fmt.Errorf("文件夹不能配置定时任务")
	}
	if err := validateContentDSNAccess(pr, report.DSN, report.Content); err != nil {
		return nil, err
	}
	dsns := engine.CollectDSNs(report.DSN, report.Content)
	report.Settings = dao.SettingsWithApprovedDSNs(report.Settings, dsns)
	if err := dao.UpdateReportByID(reportID, map[string]any{"settings": report.Settings}); err != nil {
		return nil, err
	}
	return report, nil
}

func validateScheduleConfig(item *dao.ScheduleRecord) error {
	if strings.TrimSpace(item.Cron) == "" {
		return fmt.Errorf("cron 不能为空")
	}
	action := strings.TrimSpace(item.Action)
	if action == "" {
		action = dao.ActionWebhook
	}
	if action != dao.ActionNone && action != dao.ActionWebhook {
		return fmt.Errorf("action 只能是 webhook 或 none")
	}
	item.Action = action
	if action == dao.ActionWebhook {
		if err := schedule.ValidateWebhook(item.Webhook); err != nil {
			return err
		}
	}
	channel := strings.TrimSpace(item.Channel)
	if channel == "" {
		channel = "lark"
	}
	if channel != "lark" && channel != "wework" {
		return fmt.Errorf("channel 只能是 lark 或 wework")
	}
	item.Channel = channel
	return nil
}

func maskScheduleWebhook(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= 8 {
		return "****"
	}
	return "****" + string(runes[len(runes)-6:])
}

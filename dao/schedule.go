package dao

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
)

var Schedule xdb.Model

// 动作常量: 定时任务到点执行后做什么。
const (
	ActionNone    = "none"    // 只跑报表, 不推送 (刷缓存 / 预热)
	ActionWebhook = "webhook" // 跑完推送到群机器人
)

// ScheduleRecord 是报表定时任务: 到点跑报表并把结果推送到群机器人。
type ScheduleRecord struct {
	Id          int                `json:"id"`
	ReportID    int                `json:"report_id"`
	Name        string             `json:"name"`
	Cron        string             `json:"cron"`                // 标准 5 段 cron (不含秒)
	Action      string             `json:"action"`              // none 只跑不推 / webhook 推群机器人
	Channel     string             `json:"channel"`             // lark / wework
	Webhook     string             `json:"webhook"`             // 群机器人 webhook 完整地址
	Params      map[string]string  `json:"params"`              // 固定过滤参数
	Condition   *ScheduleCondition `json:"condition,omitempty"` // 触发条件; nil=定时推送
	Enabled     bool               `json:"enabled"`
	LastRunAt   *time.Time         `json:"last_run_at,omitempty"`
	LastAlarmAt *time.Time         `json:"last_alarm_at,omitempty"` // 上次告警推送时间 (静默期判断)
	LastStatus  string             `json:"last_status"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
}

// ScheduleCondition 是阈值告警的触发条件: 对目标列按聚合方式取值, 与阈值比较, 命中才推送。
type ScheduleCondition struct {
	Block  string `json:"block,omitempty"` // 目标区块 Block.ID; 空=首个表格区块
	Column string `json:"column"`          // 目标列 Column.Name
	Agg    string `json:"agg"`             // any / first / sum / max / min / count
	Op     string `json:"op"`              // = != > >= < <=
	Value  string `json:"value"`           // 比较阈值
	// SilenceMinutes 是告警静默期 (分钟): 推送一次后, 静默期内再命中不重复推送。
	// 0 = 不静默 (每次命中都推, 兼容旧行为)。
	SilenceMinutes int `json:"silence_minutes,omitempty"`
}

func (s *ScheduleRecord) Record() xdb.Record {
	channel := strings.TrimSpace(s.Channel)
	if channel == "" {
		channel = "lark"
	}
	action := strings.TrimSpace(s.Action)
	if action == "" {
		action = ActionWebhook
	}
	params := "{}"
	if len(s.Params) > 0 {
		if b, err := json.Marshal(s.Params); err == nil {
			params = string(b)
		}
	}
	condition := "" // 空串 = 无条件 (定时推送)
	if s.Condition != nil && strings.TrimSpace(s.Condition.Column) != "" {
		if b, err := json.Marshal(s.Condition); err == nil {
			condition = string(b)
		}
	}
	return xdb.Record{
		"report_id":    s.ReportID,
		"name":         strings.TrimSpace(s.Name),
		"cron":         strings.TrimSpace(s.Cron),
		"action":       action,
		"channel":      channel,
		"webhook":      strings.TrimSpace(s.Webhook),
		"params":       params,
		"trigger_cond": condition,
		"enabled":      boolToInt(s.Enabled),
	}
}

func (s *ScheduleRecord) FromRecord(record xdb.Record) {
	s.Id = record.GetInt("id")
	s.ReportID = record.GetInt("report_id")
	s.Name = record.GetString("name")
	s.Cron = record.GetString("cron")
	s.Action = record.GetString("action")
	s.Channel = record.GetString("channel")
	s.Webhook = record.GetString("webhook")
	if raw := record.GetString("params"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &s.Params)
	}
	if raw := strings.TrimSpace(record.GetString("trigger_cond")); raw != "" {
		var cond ScheduleCondition
		if json.Unmarshal([]byte(raw), &cond) == nil && cond.Column != "" {
			s.Condition = &cond
		}
	}
	s.Enabled = record.GetInt("enabled") != 0
	s.LastRunAt = record.GetTime("last_run_at")
	s.LastAlarmAt = record.GetTime("last_alarm_at")
	s.LastStatus = record.GetString("last_status")
	if t := record.GetTime("created_at"); t != nil {
		s.CreatedAt = *t
	}
	if t := record.GetTime("updated_at"); t != nil {
		s.UpdatedAt = *t
	}
}

func CreateSchedule(s *ScheduleRecord) (int64, error) {
	if Schedule == nil {
		return 0, fmt.Errorf("schedule model not initialized")
	}
	if s.ReportID == 0 {
		return 0, fmt.Errorf("report_id is empty")
	}
	if strings.TrimSpace(s.Cron) == "" {
		return 0, fmt.Errorf("cron is empty")
	}
	return Schedule.Insert(s.Record())
}

func GetScheduleByID(id int) (*ScheduleRecord, error) {
	if Schedule == nil {
		return nil, fmt.Errorf("schedule model not initialized")
	}
	record, err := Schedule.First(xdb.WhereEq("id", id))
	if err != nil {
		return nil, err
	}
	r := &ScheduleRecord{}
	r.FromRecord(record)
	return r, nil
}

// ListSchedulesByReport 列出某报表的全部定时任务。
func ListSchedulesByReport(reportID int) ([]*ScheduleRecord, error) {
	if Schedule == nil {
		return nil, fmt.Errorf("schedule model not initialized")
	}
	records, err := Schedule.Selects(xdb.WhereEq("report_id", reportID), xdb.OrderByDesc("id"))
	if err != nil {
		return nil, err
	}
	return mapSchedules(records), nil
}

// ListAllSchedules 列出全站所有定时任务 (管理页用)。
func ListAllSchedules() ([]*ScheduleRecord, error) {
	if Schedule == nil {
		return nil, fmt.Errorf("schedule model not initialized")
	}
	records, err := Schedule.Selects(xdb.OrderByDesc("id"))
	if err != nil {
		return nil, err
	}
	return mapSchedules(records), nil
}

// ListEnabledSchedules 列出所有启用的定时任务 (调度扫描用)。
func ListEnabledSchedules() ([]*ScheduleRecord, error) {
	if Schedule == nil {
		return nil, fmt.Errorf("schedule model not initialized")
	}
	records, err := Schedule.Selects(xdb.WhereEq("enabled", 1))
	if err != nil {
		return nil, err
	}
	return mapSchedules(records), nil
}

func mapSchedules(records []xdb.Record) []*ScheduleRecord {
	out := make([]*ScheduleRecord, 0, len(records))
	for _, record := range records {
		r := &ScheduleRecord{}
		r.FromRecord(record)
		out = append(out, r)
	}
	return out
}

func UpdateScheduleByID(id int, updates xdb.Record) error {
	if Schedule == nil {
		return fmt.Errorf("schedule model not initialized")
	}
	if len(updates) == 0 {
		return fmt.Errorf("updates is empty")
	}
	_, err := Schedule.Update(updates, xdb.WhereEq("id", id))
	return err
}

func DeleteScheduleByID(id int) error {
	if Schedule == nil {
		return fmt.Errorf("schedule model not initialized")
	}
	_, err := Schedule.Delete(xdb.WhereEq("id", id))
	return err
}

// ClaimScheduleRun 原子抢占某分钟的执行权 (多实例去重):
// 仅当该任务 last_run_at 为空或早于本分钟时, 才把 last_run_at 置为本分钟并返回 true。
// 借数据库的条件更新保证只有一个实例抢到 (Update 的 ok = 受影响行数>0)。
func ClaimScheduleRun(id int, minute time.Time) (bool, error) {
	if Schedule == nil {
		return false, fmt.Errorf("schedule model not initialized")
	}
	m := minute.Format("2006-01-02 15:04:05")
	ok, err := Schedule.Update(
		xdb.Record{"last_run_at": m, "last_status": "running"},
		xdb.WhereEq("id", id),
		xdb.WhereGroup(
			xdb.WhereIsNil("last_run_at"),
			xdb.WhereOrLt("last_run_at", m),
		),
	)
	return ok, err
}

// TouchScheduleAlarm 记录一次告警推送时间 (静默期起点)。
func TouchScheduleAlarm(id int, at time.Time) error {
	if Schedule == nil {
		return fmt.Errorf("schedule model not initialized")
	}
	_, err := Schedule.Update(
		xdb.Record{"last_alarm_at": at.Format("2006-01-02 15:04:05")},
		xdb.WhereEq("id", id),
	)
	return err
}

// FinishScheduleRun 记录一次执行的最终结果 (不改 last_run_at, 抢占时已设)。
func FinishScheduleRun(id int, status string) error {
	if Schedule == nil {
		return fmt.Errorf("schedule model not initialized")
	}
	if len(status) > 255 {
		status = status[:255]
	}
	_, err := Schedule.Update(xdb.Record{"last_status": status}, xdb.WhereEq("id", id))
	return err
}

package dao

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
)

var Subscription xdb.Model

// SubscriptionRecord 是报表定时订阅: 到点跑报表并把结果推送到群机器人。
type SubscriptionRecord struct {
	Id         int               `json:"id"`
	ReportID   int               `json:"report_id"`
	Name       string            `json:"name"`
	Cron       string            `json:"cron"`    // 标准 5 段 cron (不含秒)
	Channel    string            `json:"channel"` // lark / wework
	Webhook    string            `json:"webhook"` // 群机器人 webhook 完整地址
	Params     map[string]string `json:"params"`              // 固定过滤参数
	Condition  *SubCondition     `json:"condition,omitempty"` // 触发条件; nil=定时推送
	Enabled    bool              `json:"enabled"`
	LastRunAt  *time.Time        `json:"last_run_at,omitempty"`
	LastStatus string            `json:"last_status"`
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// SubCondition 是阈值告警的触发条件: 对目标列按聚合方式取值, 与阈值比较, 命中才推送。
type SubCondition struct {
	Block  string `json:"block,omitempty"` // 目标区块 Block.ID; 空=首个表格区块
	Column string `json:"column"`          // 目标列 Column.Name
	Agg    string `json:"agg"`             // any / first / sum / max / min / count
	Op     string `json:"op"`              // = != > >= < <=
	Value  string `json:"value"`           // 比较阈值
}

func (s *SubscriptionRecord) Record() xdb.Record {
	channel := strings.TrimSpace(s.Channel)
	if channel == "" {
		channel = "lark"
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
		"report_id": s.ReportID,
		"name":      strings.TrimSpace(s.Name),
		"cron":      strings.TrimSpace(s.Cron),
		"channel":   channel,
		"webhook":   strings.TrimSpace(s.Webhook),
		"params":    params,
		"condition": condition,
		"enabled":   boolToInt(s.Enabled),
	}
}

func (s *SubscriptionRecord) FromRecord(record xdb.Record) {
	s.Id = record.GetInt("id")
	s.ReportID = record.GetInt("report_id")
	s.Name = record.GetString("name")
	s.Cron = record.GetString("cron")
	s.Channel = record.GetString("channel")
	s.Webhook = record.GetString("webhook")
	if raw := record.GetString("params"); raw != "" {
		_ = json.Unmarshal([]byte(raw), &s.Params)
	}
	if raw := strings.TrimSpace(record.GetString("condition")); raw != "" {
		var cond SubCondition
		if json.Unmarshal([]byte(raw), &cond) == nil && cond.Column != "" {
			s.Condition = &cond
		}
	}
	s.Enabled = record.GetInt("enabled") != 0
	s.LastRunAt = record.GetTime("last_run_at")
	s.LastStatus = record.GetString("last_status")
	if t := record.GetTime("created_at"); t != nil {
		s.CreatedAt = *t
	}
	if t := record.GetTime("updated_at"); t != nil {
		s.UpdatedAt = *t
	}
}

func CreateSubscription(s *SubscriptionRecord) (int64, error) {
	if Subscription == nil {
		return 0, fmt.Errorf("subscription model not initialized")
	}
	if s.ReportID == 0 {
		return 0, fmt.Errorf("report_id is empty")
	}
	if strings.TrimSpace(s.Cron) == "" {
		return 0, fmt.Errorf("cron is empty")
	}
	return Subscription.Insert(s.Record())
}

func GetSubscriptionByID(id int) (*SubscriptionRecord, error) {
	if Subscription == nil {
		return nil, fmt.Errorf("subscription model not initialized")
	}
	record, err := Subscription.First(xdb.WhereEq("id", id))
	if err != nil {
		return nil, err
	}
	r := &SubscriptionRecord{}
	r.FromRecord(record)
	return r, nil
}

// ListSubscriptionsByReport 列出某报表的全部订阅。
func ListSubscriptionsByReport(reportID int) ([]*SubscriptionRecord, error) {
	if Subscription == nil {
		return nil, fmt.Errorf("subscription model not initialized")
	}
	records, err := Subscription.Selects(xdb.WhereEq("report_id", reportID), xdb.OrderByDesc("id"))
	if err != nil {
		return nil, err
	}
	return mapSubscriptions(records), nil
}

// ListAllSubscriptions 列出全站所有订阅 (管理页用)。
func ListAllSubscriptions() ([]*SubscriptionRecord, error) {
	if Subscription == nil {
		return nil, fmt.Errorf("subscription model not initialized")
	}
	records, err := Subscription.Selects(xdb.OrderByDesc("id"))
	if err != nil {
		return nil, err
	}
	return mapSubscriptions(records), nil
}

// ListEnabledSubscriptions 列出所有启用的订阅 (调度扫描用)。
func ListEnabledSubscriptions() ([]*SubscriptionRecord, error) {
	if Subscription == nil {
		return nil, fmt.Errorf("subscription model not initialized")
	}
	records, err := Subscription.Selects(xdb.WhereEq("enabled", 1))
	if err != nil {
		return nil, err
	}
	return mapSubscriptions(records), nil
}

func mapSubscriptions(records []xdb.Record) []*SubscriptionRecord {
	out := make([]*SubscriptionRecord, 0, len(records))
	for _, record := range records {
		r := &SubscriptionRecord{}
		r.FromRecord(record)
		out = append(out, r)
	}
	return out
}

func UpdateSubscriptionByID(id int, updates xdb.Record) error {
	if Subscription == nil {
		return fmt.Errorf("subscription model not initialized")
	}
	if len(updates) == 0 {
		return fmt.Errorf("updates is empty")
	}
	_, err := Subscription.Update(updates, xdb.WhereEq("id", id))
	return err
}

func DeleteSubscriptionByID(id int) error {
	if Subscription == nil {
		return fmt.Errorf("subscription model not initialized")
	}
	_, err := Subscription.Delete(xdb.WhereEq("id", id))
	return err
}

// ClaimSubscriptionRun 原子抢占某分钟的执行权 (多实例去重):
// 仅当该订阅 last_run_at 为空或早于本分钟时, 才把 last_run_at 置为本分钟并返回 true。
// 借数据库的条件更新保证只有一个实例抢到 (Update 的 ok = 受影响行数>0)。
func ClaimSubscriptionRun(id int, minute time.Time) (bool, error) {
	if Subscription == nil {
		return false, fmt.Errorf("subscription model not initialized")
	}
	m := minute.Format("2006-01-02 15:04:05")
	ok, err := Subscription.Update(
		xdb.Record{"last_run_at": m, "last_status": "running"},
		xdb.WhereEq("id", id),
		xdb.WhereGroup(
			xdb.WhereIsNil("last_run_at"),
			xdb.WhereOrLt("last_run_at", m),
		),
	)
	return ok, err
}

// FinishSubscriptionRun 记录一次执行的最终结果 (不改 last_run_at, 抢占时已设)。
func FinishSubscriptionRun(id int, status string) error {
	if Subscription == nil {
		return fmt.Errorf("subscription model not initialized")
	}
	if len(status) > 255 {
		status = status[:255]
	}
	_, err := Subscription.Update(xdb.Record{"last_status": status}, xdb.WhereEq("id", id))
	return err
}

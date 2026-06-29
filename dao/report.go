package dao

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
)

var Report xdb.Model

type ReportRecord struct {
	Id         int       `json:"id"`
	Name       string    `json:"name"`
	ParentID   int       `json:"parent_id"` // 父文件夹 id, 0 为根
	Type       string    `json:"type"`      // report / folder
	Sort       int       `json:"sort"`      // 同级排序
	DSN        string    `json:"dsn"`
	Content    string    `json:"content"`     // 发布版 (查看者/run/定时任务看的)
	DevContent string    `json:"dev_content"` // 开发版草稿 (编辑器编辑的); 发布同步到 content
	Settings   string    `json:"settings"`
	Remark     string    `json:"remark"`
	IsPublic   bool      `json:"is_public"`             // 是否开启公开分享
	ShareToken string    `json:"share_token,omitempty"` // 公开访问令牌 (不可枚举)
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// IsFolder 是否文件夹节点。
func (r *ReportRecord) IsFolder() bool { return r.Type == "folder" }

// ReportSettings 是 report.settings 的页面级配置 (JSON)。
type ReportSettings struct {
	AutoRefresh    int    `json:"auto_refresh"`    // 自动刷新间隔 (秒); 0 关闭
	PrependContent string `json:"prepend_content"` // 页面顶部注入的原始 HTML (移植自 dataddy); 前端 v-html 渲染
}

// ParseSettings 解析 settings JSON; 为空或非法时返回零值配置。
func (r *ReportRecord) ParseSettings() ReportSettings {
	var s ReportSettings
	if strings.TrimSpace(r.Settings) == "" {
		return s
	}
	_ = json.Unmarshal([]byte(r.Settings), &s)
	return s
}

func (r *ReportRecord) Record() xdb.Record {
	dsn := strings.TrimSpace(r.DSN)
	if dsn == "" {
		dsn = "default"
	}
	typ := strings.TrimSpace(r.Type)
	if typ != "folder" {
		typ = "report"
	}
	// 注意: 不含 content (发布版)。create/update 都只写 dev_content (草稿);
	// content 仅由 PublishReport (发布) 从 dev_content 同步, 实现草稿隔离。
	return xdb.Record{
		"name":        strings.TrimSpace(r.Name),
		"parent_id":   r.ParentID,
		"type":        typ,
		"sort":        r.Sort,
		"dsn":         dsn,
		"dev_content": r.DevContent,
		"settings":    r.Settings,
		"remark":      strings.TrimSpace(r.Remark),
	}
}

func (r *ReportRecord) FromRecord(record xdb.Record) {
	r.Id = record.GetInt("id")
	r.Name = record.GetString("name")
	r.ParentID = record.GetInt("parent_id")
	r.Type = record.GetString("type")
	if r.Type == "" {
		r.Type = "report"
	}
	r.Sort = record.GetInt("sort")
	r.DSN = record.GetString("dsn")
	r.Content = record.GetString("content")
	r.DevContent = record.GetString("dev_content")
	r.Settings = record.GetString("settings")
	r.Remark = record.GetString("remark")
	r.IsPublic = record.GetInt("is_public") != 0
	r.ShareToken = record.GetString("share_token")
	if t := record.GetTime("created_at"); t != nil {
		r.CreatedAt = *t
	}
	if t := record.GetTime("updated_at"); t != nil {
		r.UpdatedAt = *t
	}
}

func CreateReport(r *ReportRecord) (int64, error) {
	if Report == nil {
		return 0, fmt.Errorf("report model not initialized")
	}
	if strings.TrimSpace(r.Name) == "" {
		return 0, fmt.Errorf("report name is empty")
	}
	return Report.Insert(r.Record())
}

func GetReportByID(id int) (*ReportRecord, error) {
	if Report == nil {
		return nil, fmt.Errorf("report model not initialized")
	}
	record, err := Report.First(xdb.WhereEq("id", id))
	if err != nil {
		return nil, err
	}
	r := &ReportRecord{}
	r.FromRecord(record)
	return r, nil
}

// GetReportByShareToken 按公开令牌取报表 (仅 is_public=1 的报表可被取到)。
func GetReportByShareToken(token string) (*ReportRecord, error) {
	if Report == nil {
		return nil, fmt.Errorf("report model not initialized")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return nil, xdb.ErrNotFound
	}
	record, err := Report.First(
		xdb.WhereEq("share_token", token),
		xdb.WhereEq("is_public", 1),
	)
	if err != nil {
		return nil, err
	}
	r := &ReportRecord{}
	r.FromRecord(record)
	return r, nil
}

// SetReportShare 设置某报表的公开状态与令牌。enable=false 时关闭公开。
func SetReportShare(id int, enable bool, token string) error {
	if Report == nil {
		return fmt.Errorf("report model not initialized")
	}
	updates := xdb.Record{"is_public": boolToInt(enable)}
	if enable && token != "" {
		updates["share_token"] = token
	}
	_, err := Report.Update(updates, xdb.WhereEq("id", id))
	return err
}

func ListReports() ([]*ReportRecord, error) {
	if Report == nil {
		return nil, fmt.Errorf("report model not initialized")
	}
	// 文件夹优先、再按 sort、再按 id, 便于前端组树与稳定排序。
	records, err := Report.Selects(xdb.OrderByAsc("sort"), xdb.OrderByAsc("id"))
	if err != nil {
		return nil, err
	}
	out := make([]*ReportRecord, 0, len(records))
	for _, record := range records {
		r := &ReportRecord{}
		r.FromRecord(record)
		out = append(out, r)
	}
	return out, nil
}

// CountReportChildren 统计某文件夹下的直接子节点数 (删除文件夹前校验)。
func CountReportChildren(parentID int) (int64, error) {
	if Report == nil {
		return 0, fmt.Errorf("report model not initialized")
	}
	return Report.Count(xdb.WhereEq("parent_id", parentID))
}

// MoveReport 仅更新某节点的 parent_id 与 sort (拖拽排序用, 不动其它字段)。
func MoveReport(id, parentID, sort int) error {
	if Report == nil {
		return fmt.Errorf("report model not initialized")
	}
	_, err := Report.Update(xdb.Record{"parent_id": parentID, "sort": sort}, xdb.WhereEq("id", id))
	return err
}

func UpdateReportByID(id int, updates xdb.Record) error {
	if Report == nil {
		return fmt.Errorf("report model not initialized")
	}
	if len(updates) == 0 {
		return fmt.Errorf("updates is empty")
	}
	_, err := Report.Update(updates, xdb.WhereEq("id", id))
	return err
}

// PublishReport 发布: 把开发版草稿 dev_content 同步到发布版 content (对查看者生效)。
func PublishReport(id int) error {
	if Report == nil {
		return fmt.Errorf("report model not initialized")
	}
	r, err := GetReportByID(id)
	if err != nil {
		return err
	}
	_, err = Report.Update(xdb.Record{"content": r.DevContent}, xdb.WhereEq("id", id))
	return err
}

func DeleteReportByID(id int) error {
	if Report == nil {
		return fmt.Errorf("report model not initialized")
	}
	_, err := Report.Delete(xdb.WhereEq("id", id))
	return err
}

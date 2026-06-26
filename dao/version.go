package dao

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/daodao97/xgo/xdb"
)

var ReportVersion xdb.Model

// maxVersionsPerReport 每个报表保留的最大版本数; 超出时写新版本会删最旧的。
const maxVersionsPerReport = 30

// VersionRecord 是报表的一次发布快照。
type VersionRecord struct {
	Id        int       `json:"id"`
	ReportID  int       `json:"report_id"`
	Content   string    `json:"content,omitempty"` // 列表接口可省略以减小体积
	UserID    int       `json:"user_id"`
	UserNick  string    `json:"user_nick"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
}

func (v *VersionRecord) FromRecord(record xdb.Record) {
	v.Id = record.GetInt("id")
	v.ReportID = record.GetInt("report_id")
	v.Content = record.GetString("content")
	v.UserID = record.GetInt("user_id")
	v.UserNick = record.GetString("user_nick")
	v.Remark = record.GetString("remark")
	if t := record.GetTime("created_at"); t != nil {
		v.CreatedAt = *t
	}
}

// contentHash 计算内容的 sha256, 用于版本去重。
func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// AddReportVersion 记录一次发布快照, 并裁剪到每报表最近 maxVersionsPerReport 条。
// 内容去重: 若该报表已有相同内容 (content_hash) 的历史版本, 则删除旧记录再新增,
// 等效于把它"冒泡到最新" (时间/操作人更新为本次发布), 列表里同一内容只保留一条。
func AddReportVersion(reportID int, content string, userID int, userNick string) error {
	if ReportVersion == nil {
		return fmt.Errorf("report_version model not initialized")
	}
	hash := contentHash(content)

	// 命中同内容旧版本 -> 删除 (随后新增, 即冒泡到最新)
	if _, err := ReportVersion.Delete(
		xdb.WhereEq("report_id", reportID),
		xdb.WhereEq("content_hash", hash),
	); err != nil {
		return err
	}

	if _, err := ReportVersion.Insert(xdb.Record{
		"report_id":    reportID,
		"content":      content,
		"content_hash": hash,
		"user_id":      userID,
		"user_nick":    userNick,
	}); err != nil {
		return err
	}
	pruneReportVersions(reportID)
	return nil
}

// pruneReportVersions 删除该报表超出保留上限的旧版本 (按 id 降序保留前 N 条)。
func pruneReportVersions(reportID int) {
	// 取第 N+1 新的版本 id 作为阈值, 删除 id 小于它的全部旧版本。
	records, err := ReportVersion.Selects(
		xdb.WhereEq("report_id", reportID),
		xdb.Field("id"),
		xdb.OrderByDesc("id"),
		xdb.Offset(maxVersionsPerReport),
		xdb.Limit(1),
	)
	if err != nil || len(records) == 0 {
		return
	}
	threshold := records[0].GetInt("id")
	_, _ = ReportVersion.Delete(
		xdb.WhereEq("report_id", reportID),
		xdb.WhereLte("id", threshold),
	)
}

// ListReportVersions 列出某报表的版本 (不含 content, 仅元信息), 按时间倒序。
func ListReportVersions(reportID int) ([]*VersionRecord, error) {
	if ReportVersion == nil {
		return nil, fmt.Errorf("report_version model not initialized")
	}
	records, err := ReportVersion.Selects(
		xdb.WhereEq("report_id", reportID),
		xdb.Field("id", "report_id", "user_id", "user_nick", "remark", "created_at"),
		xdb.OrderByDesc("id"),
	)
	if err != nil {
		return nil, err
	}
	out := make([]*VersionRecord, 0, len(records))
	for _, record := range records {
		v := &VersionRecord{}
		v.FromRecord(record)
		out = append(out, v)
	}
	return out, nil
}

// GetReportVersion 取单条版本 (含 content), 用于查看/回滚。
func GetReportVersion(id int) (*VersionRecord, error) {
	if ReportVersion == nil {
		return nil, fmt.Errorf("report_version model not initialized")
	}
	record, err := ReportVersion.First(xdb.WhereEq("id", id))
	if err != nil {
		return nil, err
	}
	v := &VersionRecord{}
	v.FromRecord(record)
	return v, nil
}

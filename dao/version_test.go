package dao

import "testing"

func TestVersionFromRecord(t *testing.T) {
	v := &VersionRecord{}
	v.FromRecord(map[string]any{
		"id": 3, "report_id": 7, "content": "SELECT 1",
		"user_id": 2, "user_nick": "张三", "remark": "",
	})
	if v.Id != 3 || v.ReportID != 7 || v.Content != "SELECT 1" {
		t.Errorf("FromRecord 基本字段错误: %+v", v)
	}
	if v.UserID != 2 || v.UserNick != "张三" {
		t.Errorf("操作人字段错误: %+v", v)
	}
}

func TestMaxVersionsConst(t *testing.T) {
	// 保留上限应为正; 防止误改为 0 导致全删
	if maxVersionsPerReport <= 0 {
		t.Fatalf("maxVersionsPerReport 必须为正, got %d", maxVersionsPerReport)
	}
}

func TestContentHash(t *testing.T) {
	// 相同内容同 hash (去重依据), 不同内容不同 hash
	if contentHash("SELECT 1") != contentHash("SELECT 1") {
		t.Error("相同内容应得相同 hash")
	}
	if contentHash("SELECT 1") == contentHash("SELECT 2") {
		t.Error("不同内容应得不同 hash")
	}
	if len(contentHash("")) != 64 {
		t.Errorf("sha256 hex 应为 64 字符, got %d", len(contentHash("")))
	}
}

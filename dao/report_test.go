package dao

import "testing"

func TestParseSettings(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"empty", "", 0},
		{"valid", `{"auto_refresh":30}`, 30},
		{"zero", `{"auto_refresh":0}`, 0},
		{"absent", `{"other":1}`, 0},
		{"invalid json", `not json`, 0},
	}
	for _, c := range cases {
		r := &ReportRecord{Settings: c.raw}
		if got := r.ParseSettings().AutoRefresh; got != c.want {
			t.Errorf("%s: AutoRefresh = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestReportRecordExcludesContent(t *testing.T) {
	r := &ReportRecord{Name: "t", Content: "PUBLISHED", DevContent: "DRAFT"}
	rec := r.Record()
	// Record() 用于 create/update: 只写 dev_content, 不写 content (content 仅由发布同步)
	if _, has := rec["content"]; has {
		t.Error("Record() 不应包含 content (防止保存覆盖发布版)")
	}
	if rec["dev_content"] != "DRAFT" {
		t.Errorf("dev_content = %v, want DRAFT", rec["dev_content"])
	}
}

func TestReportFromRecordBothContents(t *testing.T) {
	r := &ReportRecord{}
	r.FromRecord(map[string]any{"id": 1, "content": "PUB", "dev_content": "DEV"})
	if r.Content != "PUB" || r.DevContent != "DEV" {
		t.Errorf("FromRecord 未正确读取双版本: content=%q dev=%q", r.Content, r.DevContent)
	}
}

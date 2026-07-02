package schedule

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"xproxy/internal/engine"
)

func TestDueAt(t *testing.T) {
	// 2026-06-25 09:00 (周四)
	at0900 := time.Date(2026, 6, 25, 9, 0, 0, 0, time.Local)
	at0901 := time.Date(2026, 6, 25, 9, 1, 0, 0, time.Local)

	if !dueAt("0 9 * * *", at0900) {
		t.Error("0 9 * * * 应在 09:00 触发")
	}
	if dueAt("0 9 * * *", at0901) {
		t.Error("0 9 * * * 不应在 09:01 触发")
	}
	if !dueAt("* * * * *", at0901) {
		t.Error("* * * * * 应每分钟触发")
	}
	if dueAt("bad cron", at0900) {
		t.Error("非法 cron 应视为不触发")
	}
}

func TestSilenced(t *testing.T) {
	now := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	recent := now.Add(-5 * time.Minute)
	old := now.Add(-2 * time.Hour)

	if silenced(0, &recent, now) {
		t.Error("silence_minutes=0 不应静默")
	}
	if silenced(30, nil, now) {
		t.Error("从未告警过不应静默")
	}
	if !silenced(30, &recent, now) {
		t.Error("5 分钟前告警过、静默期 30 分钟, 应静默")
	}
	if silenced(30, &old, now) {
		t.Error("2 小时前告警、静默期 30 分钟, 不应静默")
	}
}

func TestRenderText(t *testing.T) {
	r := &engine.Result{
		Blocks: []engine.Block{
			{
				Type: "table", Title: "渠道汇总",
				Columns: []engine.Column{{Name: "ch", Header: "渠道"}, {Name: "amt", Header: "金额"}},
				Rows:    []map[string]any{{"ch": "web", "amt": 100}, {"ch": "app", "amt": 200}},
			},
		},
	}
	out := RenderText("销售日报", r, "https://bi.example.com/x")
	if !strings.Contains(out, "销售日报") || !strings.Contains(out, "渠道汇总") {
		t.Fatalf("缺少标题: %q", out)
	}
	if !strings.Contains(out, "渠道 | 金额") {
		t.Errorf("缺少表头: %q", out)
	}
	if !strings.Contains(out, "https://bi.example.com/x") {
		t.Errorf("缺少链接: %q", out)
	}
}

func TestRenderTextNoLink(t *testing.T) {
	r := &engine.Result{Blocks: []engine.Block{{Type: "table", Title: "t",
		Columns: []engine.Column{{Name: "a", Header: "A"}}, Rows: []map[string]any{{"a": 1}}}}}
	out := RenderText("r", r, "")
	if strings.Contains(out, "🔗") {
		t.Errorf("无链接时不应出现链接标记: %q", out)
	}
}

// HTTP 路径测试用 postJSON + httptest (localhost 合法; SSRF 校验是针对用户填的 URL,
// 在 push() 层, 这里单测网络/payload 路径)。
func TestPushPayload(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewDecoder(req.Body).Decode(&got)
		w.Write([]byte(`{"code":0}`))
	}))
	defer srv.Close()

	if err := postJSON(srv.URL, buildPayload(ChannelLark, "hi")); err != nil {
		t.Fatalf("lark push: %v", err)
	}
	if got["msg_type"] != "text" {
		t.Errorf("飞书 payload 错误: %+v", got)
	}

	if err := postJSON(srv.URL, buildPayload(ChannelWework, "hi")); err != nil {
		t.Fatalf("wework push: %v", err)
	}
	if got["msgtype"] != "text" {
		t.Errorf("企微 payload 错误: %+v", got)
	}
}

func TestPushErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook"}`))
	}))
	defer srv.Close()
	err := postJSON(srv.URL, buildPayload(ChannelWework, "hi"))
	if err == nil || !strings.Contains(err.Error(), "93000") {
		t.Errorf("应返回 errcode 错误, got %v", err)
	}
}

// push() 对非白名单/内网地址应直接拒绝 (SSRF 防护)。
func TestPushRejectsUntrustedURL(t *testing.T) {
	if err := push(ChannelLark, "  ", "hi"); err == nil {
		t.Error("空 webhook 应报错")
	}
	if err := push(ChannelLark, "http://127.0.0.1/hook", "hi"); err == nil {
		t.Error("内网地址应被拒绝")
	}
	if err := push(ChannelLark, "https://evil.com/hook", "hi"); err == nil {
		t.Error("非白名单域名应被拒绝")
	}
}

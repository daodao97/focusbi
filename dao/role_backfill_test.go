package dao

import "testing"

func TestHasAnyDsnKey(t *testing.T) {
	if !hasAnyDsnKey(map[string]string{"dsn": "r"}) {
		t.Error("dsn 键应识别")
	}
	if !hasAnyDsnKey(map[string]string{"dsn.5": "r"}) {
		t.Error("dsn.5 键应识别")
	}
	if hasAnyDsnKey(map[string]string{"report": "Rr"}) {
		t.Error("无 dsn 键不应误判")
	}
}

func TestHasReportReadKey(t *testing.T) {
	if !hasReportReadKey(map[string]string{"report": "Rr"}) {
		t.Error("report:Rr 含读")
	}
	if !hasReportReadKey(map[string]string{"report.5": "rw"}) {
		t.Error("report.5:rw 含读")
	}
	if hasReportReadKey(map[string]string{"dsn": "r"}) {
		t.Error("仅 dsn 键不应算 report 读")
	}
}

// 回填判定: 含报表读 + 无 dsn -> 需回填; 否则不动。
func TestBackfillDecision(t *testing.T) {
	needs := func(res map[string]string) bool {
		return hasReportReadKey(res) && !hasAnyDsnKey(res)
	}
	if !needs(map[string]string{"report": "Rr"}) {
		t.Error("能读报表但无 dsn -> 应回填")
	}
	if needs(map[string]string{"report": "Rr", "dsn": "r"}) {
		t.Error("已有 dsn -> 不回填")
	}
	if needs(map[string]string{"report": "Rr", "dsn.5": "r"}) {
		t.Error("已有 dsn.5 -> 不回填")
	}
	if needs(map[string]string{"dsn": "r"}) {
		t.Error("无报表读 -> 不回填")
	}
}

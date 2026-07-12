package datasource

import (
	"context"
	"testing"
	"time"

	"xproxy/conf"
)

func TestFormatQueryDeadlineError(t *testing.T) {
	old := conf.ConfInstance
	conf.ConfInstance = &conf.Conf{Engine: conf.EngineConf{QueryTimeout: "3m"}}
	defer func() { conf.ConfInstance = old }()

	err := formatQueryError(context.DeadlineExceeded)
	if err == nil || err.Error() != "SQL 查询超时（超过 3分钟）" {
		t.Fatalf("timeout error = %v", err)
	}
}

func TestExplainPrefix(t *testing.T) {
	cases := map[string]string{
		"mysql":    "EXPLAIN ",
		"postgres": "EXPLAIN (FORMAT JSON) ",
		"sqlite":   "EXPLAIN QUERY PLAN ",
	}
	for driver, want := range cases {
		if got := explainPrefix(driver); got != want {
			t.Fatalf("driver=%s prefix=%q, want %q", driver, got, want)
		}
	}
}

func TestFormatDurationCN(t *testing.T) {
	cases := map[time.Duration]string{
		3 * time.Minute:  "3分钟",
		45 * time.Second: "45秒",
		90 * time.Second: "1分30秒",
		2 * time.Hour:    "2小时",
	}
	for in, want := range cases {
		if got := formatDurationCN(in); got != want {
			t.Fatalf("formatDurationCN(%v) = %q, want %q", in, got, want)
		}
	}
}

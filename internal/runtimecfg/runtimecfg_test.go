package runtimecfg

import (
	"testing"
	"time"

	"xproxy/conf"
	"xproxy/dao"
)

func TestScriptFetchFallsBackToConfig(t *testing.T) {
	oldConf, oldModel := conf.ConfInstance, dao.SystemSetting
	conf.ConfInstance = &conf.Conf{Engine: conf.EngineConf{ScriptFetch: "ON"}}
	dao.SystemSetting = nil
	defer func() {
		conf.ConfInstance, dao.SystemSetting = oldConf, oldModel
		Invalidate()
	}()
	Invalidate()
	mode, source := ScriptFetch()
	if mode != "on" || source != "config" {
		t.Fatalf("mode=%q source=%q", mode, source)
	}
}

func TestSetScriptFetchRejectsInvalidModeBeforeDB(t *testing.T) {
	if err := SetScriptFetch("not-a-url"); err == nil {
		t.Fatal("非法白名单应被拒绝")
	}
}

func TestRuntimeSettingsFallBackToConfig(t *testing.T) {
	oldConf, oldModel := conf.ConfInstance, dao.SystemSetting
	disabled := false
	conf.ConfInstance = &conf.Conf{
		Engine: conf.EngineConf{
			QueryTimeout:     "45s",
			QueryConcurrency: 3,
			ScriptTimeout:    "12s",
		},
		Schedule: conf.ScheduleConf{Enabled: &disabled},
		Security: conf.SecurityConf{PublicShareEnabled: &disabled},
	}
	dao.SystemSetting = nil
	defer func() {
		conf.ConfInstance, dao.SystemSetting = oldConf, oldModel
		Invalidate()
	}()
	Invalidate()

	if got := QueryTimeout(); got != 45*time.Second {
		t.Fatalf("query timeout = %v", got)
	}
	if got := QueryConcurrency(); got != 3 {
		t.Fatalf("query concurrency = %d", got)
	}
	if got := ScriptTimeout(); got != 12*time.Second {
		t.Fatalf("script timeout = %v", got)
	}
	if ScheduleEnabled() || PublicShareEnabled() {
		t.Fatal("配置文件中的关闭开关未生效")
	}

	values, sources := Snapshot()
	if values[QueryTimeoutKey] != "45s" || values[ScriptTimeoutKey] != "12s" ||
		values[QueryConcurrencyKey] != "3" || values[ScheduleEnabledKey] != "false" ||
		values[PublicShareEnabledKey] != "false" {
		t.Fatalf("snapshot values = %#v", values)
	}
	for _, key := range []string{QueryTimeoutKey, QueryConcurrencyKey, ScriptTimeoutKey, ScheduleEnabledKey, PublicShareEnabledKey} {
		if sources[key] != "config" {
			t.Fatalf("%s source = %q", key, sources[key])
		}
	}
}

func TestSnapshotReturnsEffectiveValuesForInvalidConfig(t *testing.T) {
	oldConf, oldModel := conf.ConfInstance, dao.SystemSetting
	conf.ConfInstance = &conf.Conf{Engine: conf.EngineConf{
		QueryTimeout:     "bad",
		QueryConcurrency: -1,
		ScriptTimeout:    "bad",
	}}
	dao.SystemSetting = nil
	defer func() {
		conf.ConfInstance, dao.SystemSetting = oldConf, oldModel
		Invalidate()
	}()
	Invalidate()

	values, _ := Snapshot()
	if values[QueryTimeoutKey] != "3m0s" || values[QueryConcurrencyKey] != "8" || values[ScriptTimeoutKey] != "3m0s" {
		t.Fatalf("snapshot values = %#v", values)
	}
}

func TestValidateDynamicSettings(t *testing.T) {
	tests := []struct {
		key   string
		value string
		want  string
		ok    bool
	}{
		{QueryTimeoutKey, "90s", "1m30s", true},
		{QueryTimeoutKey, "500ms", "", false},
		{ScriptTimeoutKey, "31m", "", false},
		{QueryConcurrencyKey, "64", "64", true},
		{QueryConcurrencyKey, "65", "", false},
		{ScheduleEnabledKey, "FALSE", "false", true},
		{PublicShareEnabledKey, "yes", "", false},
		{"unknown", "1", "", false},
	}
	for _, tt := range tests {
		got, err := validate(tt.key, tt.value)
		if (err == nil) != tt.ok || got != tt.want {
			t.Errorf("validate(%q, %q) = %q, %v", tt.key, tt.value, got, err)
		}
	}
}

func TestNormalizedDefaultUsesConfig(t *testing.T) {
	oldConf := conf.ConfInstance
	disabled := false
	conf.ConfInstance = &conf.Conf{
		Engine: conf.EngineConf{
			ScriptFetch:      "ON",
			QueryTimeout:     "90s",
			QueryConcurrency: 4,
			ScriptTimeout:    "20s",
		},
		Schedule: conf.ScheduleConf{Enabled: &disabled},
		Security: conf.SecurityConf{PublicShareEnabled: &disabled},
	}
	defer func() { conf.ConfInstance = oldConf }()

	wants := map[string]string{
		ScriptFetchKey:        "on",
		QueryTimeoutKey:       "1m30s",
		QueryConcurrencyKey:   "4",
		ScriptTimeoutKey:      "20s",
		ScheduleEnabledKey:    "false",
		PublicShareEnabledKey: "false",
	}
	for key, want := range wants {
		got, err := normalizedDefault(key)
		if err != nil || got != want {
			t.Errorf("normalizedDefault(%q) = %q, %v; want %q", key, got, err, want)
		}
	}
}

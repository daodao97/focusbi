package conf

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caarlos0/env/v11"
)

func TestValidateJWTSecret(t *testing.T) {
	bad := []string{"", "   ", defaultJWTSecret}
	for _, s := range bad {
		if (&Conf{Site: SiteConf{JWTSecret: s}}).validateJWTSecret() == nil {
			t.Errorf("密钥 %q 应被拒绝", s)
		}
	}
	good := []string{"a-real-secret", "0123456789abcdef"}
	for _, s := range good {
		if (&Conf{Site: SiteConf{JWTSecret: s}}).validateJWTSecret() != nil {
			t.Errorf("密钥 %q 应被接受", s)
		}
	}
}

func TestQueryTimeoutDuration(t *testing.T) {
	if got := (*Conf)(nil).QueryTimeoutDuration(); got != 3*time.Minute {
		t.Fatalf("nil conf timeout = %v, want 3m", got)
	}
	if got := (&Conf{}).QueryTimeoutDuration(); got != 3*time.Minute {
		t.Fatalf("empty timeout = %v, want 3m", got)
	}
	if got := (&Conf{Engine: EngineConf{QueryTimeout: "bad"}}).QueryTimeoutDuration(); got != 3*time.Minute {
		t.Fatalf("bad timeout = %v, want 3m", got)
	}
	if got := (&Conf{Engine: EngineConf{QueryTimeout: "-1s"}}).QueryTimeoutDuration(); got != 3*time.Minute {
		t.Fatalf("negative timeout = %v, want 3m", got)
	}
	if got := (&Conf{Engine: EngineConf{QueryTimeout: "45s"}}).QueryTimeoutDuration(); got != 45*time.Second {
		t.Fatalf("custom timeout = %v, want 45s", got)
	}
}

func TestRuntimeSettingDefaults(t *testing.T) {
	if got := (*Conf)(nil).ScriptTimeoutDuration(); got != 3*time.Minute {
		t.Fatalf("nil script timeout = %v", got)
	}
	if got := (&Conf{Engine: EngineConf{ScriptTimeout: "20s"}}).ScriptTimeoutDuration(); got != 20*time.Second {
		t.Fatalf("script timeout = %v", got)
	}
	if !(*Conf)(nil).ScheduleEnabled() || !(&Conf{}).PublicShareEnabled() {
		t.Fatal("未配置的功能开关应默认开启")
	}
	disabled := false
	c := &Conf{
		Schedule: ScheduleConf{Enabled: &disabled},
		Security: SecurityConf{PublicShareEnabled: &disabled},
	}
	if c.ScheduleEnabled() || c.PublicShareEnabled() {
		t.Fatal("显式关闭的功能开关未生效")
	}
}

func TestScriptFetchAllowed(t *testing.T) {
	mk := func(mode string) *Conf { return &Conf{Engine: EngineConf{ScriptFetch: mode}} }

	// 默认关闭; on 仅在 URL 结构合法时通过配置层校验 (私网由网络层拒绝)。
	for _, mode := range []string{"", "off", "OFF", " off "} {
		if mk(mode).ScriptFetchAllowed("https://api.example.com/x") == nil {
			t.Errorf("mode=%q 应拒绝", mode)
		}
	}
	if mk("on").ScriptFetchAllowed("https://api.example.com/x") != nil {
		t.Error("on 应允许合法公网 URL")
	}
	// 白名单: 前缀命中放行, 未命中拒绝
	wl := mk("https://api.example.com, https://open.feishu.cn")
	if wl.ScriptFetchAllowed("https://api.example.com/v1/rate") != nil {
		t.Error("白名单前缀命中应放行")
	}
	if wl.ScriptFetchAllowed("https://open.feishu.cn/hook") != nil {
		t.Error("白名单第二项应放行")
	}
	if wl.ScriptFetchAllowed("http://169.254.169.254/meta") == nil {
		t.Error("白名单未命中应拒绝")
	}
	if wl.ScriptFetchAllowed("https://api.example.com.evil.com/x") == nil {
		t.Error("前缀伪装域名应拒绝")
	}
	if wl.ScriptFetchAllowed("https://api.example.com:@evil.com/x") == nil {
		t.Error("userinfo 伪装域名应拒绝")
	}
	pathWL := mk("https://api.example.com/v1")
	if pathWL.ScriptFetchAllowed("https://api.example.com/v1/rate") != nil ||
		pathWL.ScriptFetchAllowed("https://api.example.com/v10/rate") == nil {
		t.Error("路径白名单应按段边界匹配")
	}
	if pathWL.ScriptFetchAllowed("https://api.example.com/v1/%2e%2e/admin") == nil {
		t.Error("编码后的路径穿越不应绕过白名单")
	}
}

func TestEnvOverridesNestedConfig(t *testing.T) {
	t.Setenv("SITE_URL", "https://bi.example.com")
	t.Setenv("SITE_JWT_SECRET", "env-secret")
	t.Setenv("ENGINE_QUERY_TIMEOUT", "30s")
	t.Setenv("ENGINE_SCRIPT_TIMEOUT", "20s")
	t.Setenv("SCHEDULE_ENABLED", "false")
	t.Setenv("SECURITY_PUBLIC_SHARE_ENABLED", "false")
	t.Setenv("AI_PROVIDER", "claude")
	t.Setenv("AI_BASE_URL", "https://api.example.com")
	t.Setenv("TURNSTILE_SITE_KEY", "site-key")
	t.Setenv("TURNSTILE_SECRET_KEY", "secret-key")

	c := &Conf{
		Site: SiteConf{
			URL:       "http://127.0.0.1:8099",
			JWTSecret: defaultJWTSecret,
		},
		Engine: EngineConf{QueryTimeout: "3m"},
	}
	if err := env.Parse(c); err != nil {
		t.Fatalf("parse env: %v", err)
	}

	if c.Site.URL != "https://bi.example.com" {
		t.Fatalf("site url = %q", c.Site.URL)
	}
	if c.Site.JWTSecret != "env-secret" {
		t.Fatalf("jwt secret = %q", c.Site.JWTSecret)
	}
	if c.Engine.QueryTimeout != "30s" {
		t.Fatalf("query timeout = %q", c.Engine.QueryTimeout)
	}
	if c.Engine.ScriptTimeout != "20s" {
		t.Fatalf("script timeout = %q", c.Engine.ScriptTimeout)
	}
	if c.ScheduleEnabled() || c.PublicShareEnabled() {
		t.Fatal("环境变量中的运行时开关未生效")
	}
	if c.AI.Provider != "claude" {
		t.Fatalf("ai provider = %q", c.AI.Provider)
	}
	if c.AI.BaseURL != "https://api.example.com" {
		t.Fatalf("ai base url = %q", c.AI.BaseURL)
	}
	if c.Turnstile.SiteKey != "site-key" {
		t.Fatalf("turnstile site key = %q", c.Turnstile.SiteKey)
	}
	if c.Turnstile.SecretKey != "secret-key" {
		t.Fatalf("turnstile secret key = %q", c.Turnstile.SecretKey)
	}
}

func TestLoadDotEnvDoesNotOverrideExistingEnv(t *testing.T) {
	preserveEnv(t, "SITE_URL", "SITE_JWT_SECRET", "ENGINE_QUERY_TIMEOUT", "EXTRA_VALUE")
	t.Setenv("SITE_JWT_SECRET", "from-env")
	if err := os.Unsetenv("SITE_URL"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("ENGINE_QUERY_TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv("EXTRA_VALUE"); err != nil {
		t.Fatal(err)
	}

	envPath := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(envPath, []byte(`
# comment
SITE_URL="https://dotenv.example.com"
SITE_JWT_SECRET=from-dotenv
ENGINE_QUERY_TIMEOUT='45s'
export EXTRA_VALUE=ok
`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := loadDotEnv(envPath); err != nil {
		t.Fatalf("load .env: %v", err)
	}
	if got := os.Getenv("SITE_JWT_SECRET"); got != "from-env" {
		t.Fatalf("SITE_JWT_SECRET = %q", got)
	}
	if got := os.Getenv("SITE_URL"); got != "https://dotenv.example.com" {
		t.Fatalf("SITE_URL = %q", got)
	}
	if got := os.Getenv("ENGINE_QUERY_TIMEOUT"); got != "45s" {
		t.Fatalf("ENGINE_QUERY_TIMEOUT = %q", got)
	}
	if got := os.Getenv("EXTRA_VALUE"); got != "ok" {
		t.Fatalf("EXTRA_VALUE = %q", got)
	}
}

func preserveEnv(t *testing.T, keys ...string) {
	t.Helper()
	type oldEnv struct {
		key    string
		value  string
		exists bool
	}
	old := make([]oldEnv, 0, len(keys))
	for _, key := range keys {
		value, exists := os.LookupEnv(key)
		old = append(old, oldEnv{key: key, value: value, exists: exists})
	}
	t.Cleanup(func() {
		for _, item := range old {
			if item.exists {
				_ = os.Setenv(item.key, item.value)
			} else {
				_ = os.Unsetenv(item.key)
			}
		}
	})
}

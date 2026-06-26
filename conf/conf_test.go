package conf

import (
	"testing"
	"time"
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

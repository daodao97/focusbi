package conf

import "testing"

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

package dao

import (
	"strings"
	"testing"
	"time"
)

func TestHashAPITokenStable(t *testing.T) {
	// 相同明文同哈希 (鉴权依据), 不同明文不同哈希; 长度为 sha256 hex 64。
	if HashAPIToken("fbt_abc") != HashAPIToken("fbt_abc") {
		t.Error("相同明文应得相同哈希")
	}
	if HashAPIToken("fbt_abc") == HashAPIToken("fbt_def") {
		t.Error("不同明文应得不同哈希")
	}
	if len(HashAPIToken("x")) != 64 {
		t.Errorf("sha256 hex 应为 64 字符, got %d", len(HashAPIToken("x")))
	}
	// 前后空白应被忽略 (与建库时 TrimSpace 一致)
	if HashAPIToken(" fbt_abc ") != HashAPIToken("fbt_abc") {
		t.Error("哈希前应 TrimSpace")
	}
}

func TestAPITokenExpired(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)
	if (&APITokenRecord{ExpiresAt: nil}).Expired() {
		t.Error("ExpiresAt 为 nil 应永不过期")
	}
	if !(&APITokenRecord{ExpiresAt: &past}).Expired() {
		t.Error("过去时间应已过期")
	}
	if (&APITokenRecord{ExpiresAt: &future}).Expired() {
		t.Error("未来时间不应过期")
	}
}

func TestAPITokenPrefixConst(t *testing.T) {
	// 明文令牌前缀, 鉴权按它区分 API token 与登录 JWT。
	if !strings.HasPrefix(apiTokenPrefix, "fbt") {
		t.Errorf("apiTokenPrefix 预期以 fbt 开头, got %q", apiTokenPrefix)
	}
}

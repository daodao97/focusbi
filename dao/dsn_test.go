package dao

import (
	"encoding/json"
	"strings"
	"testing"
)

// 出站脱敏: 连接串密码段 + SSH 密码/私钥/口令均不得以明文出现在 JSON 里。
func TestDsnMarshalJSONMasksSecrets(t *testing.T) {
	d := &DsnRecord{
		Name: "sales", Driver: "mysql",
		DSN:              "root:s3cret@tcp(10.0.0.1:3306)/db?charset=utf8mb4",
		SSHPassword:      "sshpw",
		SSHKey:           "-----BEGIN KEY-----abc-----END KEY-----",
		SSHKeyPassphrase: "keypass",
	}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, secret := range []string{"s3cret", "sshpw", "BEGIN KEY", "keypass"} {
		if strings.Contains(s, secret) {
			t.Errorf("JSON 泄漏明文 %q: %s", secret, s)
		}
	}
	// 结构信息保留 (host/db/user 供编辑显示)
	if !strings.Contains(s, "10.0.0.1") || !strings.Contains(s, "root:****@") {
		t.Errorf("连接串结构应保留、密码段应脱敏: %s", s)
	}
}

// 无密码的连接串不应被破坏。
func TestMaskDSNPasswordNoPassword(t *testing.T) {
	if got := maskDSNPassword("/var/data.db"); got != "/var/data.db" {
		t.Errorf("sqlite 路径不应改动: %q", got)
	}
	if got := maskDSNPassword("postgres://user@127.0.0.1:5432/db"); strings.Contains(got, "****") {
		t.Errorf("无密码连接串不应出现 ****: %q", got)
	}
}

// 含特殊字符 (@ : /) 的 MySQL 密码必须整体脱敏, 不泄露片段。
// 正则 `:[^:@/]*@` 会在 @ / 处截断, 故改用 driver ParseDSN 结构化脱敏。
func TestMaskDSNPasswordSpecialChars(t *testing.T) {
	cases := map[string]string{ // dsn -> 不应泄露的密码
		"user:p@ss@tcp(127.0.0.1:3306)/db": "p@ss",
		"user:p/w@tcp(127.0.0.1:3306)/db":  "p/w",
		"user:p:ss@tcp(127.0.0.1:3306)/db": "p:ss",
	}
	for dsn, pw := range cases {
		got := maskDSNPassword(dsn)
		if strings.Contains(got, pw) {
			t.Errorf("密码 %q 未完全脱敏: %q", pw, got)
		}
		if !strings.Contains(got, "user:****@") {
			t.Errorf("应脱敏为 user:****@: %q", got)
		}
	}
}

// 含特殊字符的密码, 编辑未改时也应能正确补回 (脱敏/补回对称)。
func TestMergeSecretsSpecialCharRoundTrip(t *testing.T) {
	old := &DsnRecord{DSN: "user:p@ss/w@tcp(10.0.0.1:3306)/db"}
	masked := maskDSNPassword(old.DSN) // user:****@tcp(...)
	edited := &DsnRecord{DSN: masked}
	edited.MergeSecretsFrom(old)
	if edited.DSN != old.DSN {
		t.Errorf("特殊字符密码补回失败: got %q want %q", edited.DSN, old.DSN)
	}
}

func TestMaskDSNPasswordKeywordStyle(t *testing.T) {
	got := maskDSNPassword("host=127.0.0.1 user=bi password=s3cret dbname=app sslmode=disable")
	if strings.Contains(got, "s3cret") {
		t.Fatalf("keyword DSN 泄漏密码: %q", got)
	}
	if !strings.Contains(got, "password=****") {
		t.Fatalf("keyword DSN 应脱敏 password 字段: %q", got)
	}
}

// 入站补回: 未改动的脱敏字段用库中原值填回; 改动的字段以新值为准。
func TestMergeSecretsFrom(t *testing.T) {
	old := &DsnRecord{
		DSN:         "root:realpw@tcp(10.0.0.1:3306)/db",
		SSHPassword: "oldpw", SSHKey: "oldkey", SSHKeyPassphrase: "oldpass",
	}

	// 场景 1: 用户只改了 host, 密码保持脱敏占位 -> 补回真密码
	edited := &DsnRecord{
		DSN:         "root:****@tcp(10.0.0.9:3306)/db",
		SSHPassword: DsnSecretMask, SSHKey: DsnSecretMask, SSHKeyPassphrase: DsnSecretMask,
	}
	edited.MergeSecretsFrom(old)
	if edited.DSN != "root:realpw@tcp(10.0.0.9:3306)/db" {
		t.Errorf("应补回原密码并保留新 host, got %q", edited.DSN)
	}
	if edited.SSHPassword != "oldpw" || edited.SSHKey != "oldkey" || edited.SSHKeyPassphrase != "oldpass" {
		t.Errorf("未改动的 SSH 凭据应补回原值: %+v", edited)
	}

	// 场景 2: 用户提交了新密码 -> 以新值为准, 不被覆盖
	edited2 := &DsnRecord{
		DSN:         "root:newpw@tcp(10.0.0.1:3306)/db",
		SSHPassword: "newsshpw",
	}
	edited2.MergeSecretsFrom(old)
	if edited2.DSN != "root:newpw@tcp(10.0.0.1:3306)/db" {
		t.Errorf("新密码不应被覆盖: %q", edited2.DSN)
	}
	if edited2.SSHPassword != "newsshpw" {
		t.Errorf("新 SSH 密码不应被覆盖: %q", edited2.SSHPassword)
	}

	oldPg := &DsnRecord{DSN: "host=127.0.0.1 user=bi password=realpw dbname=app"}
	editedPg := &DsnRecord{DSN: "host=127.0.0.2 user=bi password=**** dbname=app"}
	editedPg.MergeSecretsFrom(oldPg)
	if !strings.Contains(editedPg.DSN, "password=realpw") || !strings.Contains(editedPg.DSN, "127.0.0.2") {
		t.Errorf("keyword DSN 应补回原密码并保留新 host, got %q", editedPg.DSN)
	}
}

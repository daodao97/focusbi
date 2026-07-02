package dao

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
	mysqldriver "github.com/go-sql-driver/mysql"
)

var Dsn xdb.Model

type DsnRecord struct {
	Id     int    `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	DSN    string `json:"dsn"`
	Remark string `json:"remark"`
	// SSH 隧道 (仅 mysql)
	SSHEnabled       bool   `json:"ssh_enabled"`
	SSHHost          string `json:"ssh_host"`
	SSHPort          int    `json:"ssh_port"`
	SSHUser          string `json:"ssh_user"`
	SSHAuth          string `json:"ssh_auth"` // password | key
	SSHPassword      string `json:"ssh_password"`
	SSHKey           string `json:"ssh_key"`
	SSHKeyPassphrase string `json:"ssh_key_passphrase"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DsnSecretMask 是 API 出站时敏感字段的脱敏占位。前端编辑回填时若某字段仍为此值,
// 表示用户未改动, 后端 MergeSecretsFrom 会用库中原值补回, 不覆盖。
const DsnSecretMask = "****"

// dsnPwRe 匹配连接串里的 ":密码@" 段 (host/user/db 结构保留, 仅密码脱敏)。
var dsnPwRe = regexp.MustCompile(`:[^:@/]*@`)
var dsnKeywordPwRe = regexp.MustCompile(`(?i)(^|\s)(password|pass|pwd)=('[^']*'|"[^"]*"|\S*)`)

// maskDSNPassword 把连接串的密码段脱敏: user:pass@tcp(...) -> user:****@tcp(...)。
// 无密码的连接串 (如 sqlite 路径、user@host) 原样返回。
//
// MySQL 串优先用 driver 的 ParseDSN 结构化脱敏: 正则 `:[^:@/]*@` 对含 @ / 的密码
// (如 p@ss / p/w, go-sql-driver 均视为合法密码) 会漏脱敏或泄露片段, 而 ParseDSN 按
// 驱动真实规则解析密码, 再 FormatDSN 重建, 密码整体被替换。非 MySQL 格式 ParseDSN 报错,
// 回退到 URL / keyword / 正则链处理 postgres URL、keyword DSN 等。
func maskDSNPassword(dsn string) string {
	if cfg, err := mysqldriver.ParseDSN(dsn); err == nil && cfg.Passwd != "" {
		cfg.Passwd = DsnSecretMask
		return cfg.FormatDSN()
	}
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" && u.User != nil {
		if _, ok := u.User.Password(); ok {
			return strings.Replace(dsn, u.User.String()+"@", u.User.Username()+":"+DsnSecretMask+"@", 1)
		}
	}
	dsn = dsnKeywordPwRe.ReplaceAllString(dsn, "${1}${2}="+DsnSecretMask)
	return dsnPwRe.ReplaceAllString(dsn, ":"+DsnSecretMask+"@")
}

// MarshalJSON 出站脱敏: 连接串密码段与 SSH 密码/私钥/口令均以 **** 替代,
// 避免任何持 dsn:r 的用户经 GET /dsn 拿到明文凭据。结构信息 (host/db/user) 保留供编辑。
func (d *DsnRecord) MarshalJSON() ([]byte, error) {
	type alias DsnRecord // 复用字段布局但不带本方法, 避免递归
	view := alias(*d)
	view.DSN = maskDSNPassword(view.DSN)
	if view.SSHPassword != "" {
		view.SSHPassword = DsnSecretMask
	}
	if view.SSHKey != "" {
		view.SSHKey = DsnSecretMask
	}
	if view.SSHKeyPassphrase != "" {
		view.SSHKeyPassphrase = DsnSecretMask
	}
	return json.Marshal(view)
}

// MergeSecretsFrom 入站补回: 前端提交的敏感字段若仍是脱敏占位 (用户未改), 用 old 的原值填回。
// 连接串: 若密码段仍是 :****@ 则换回 old 的真实密码 (允许用户只改 host/db 不动密码)。
func (d *DsnRecord) MergeSecretsFrom(old *DsnRecord) {
	if old == nil {
		return
	}
	// MySQL: 用 driver 解析, 若提交串的密码仍是脱敏占位则换回旧串的真实密码 (与脱敏对称,
	// 正确处理含 @ / 的密码)。
	if cfg, err := mysqldriver.ParseDSN(d.DSN); err == nil && cfg.Passwd == DsnSecretMask {
		if oldCfg, err := mysqldriver.ParseDSN(old.DSN); err == nil {
			cfg.Passwd = oldCfg.Passwd
			d.DSN = cfg.FormatDSN()
		}
	} else if strings.Contains(d.DSN, ":"+DsnSecretMask+"@") {
		if m := dsnPwRe.FindString(old.DSN); m != "" {
			d.DSN = strings.Replace(d.DSN, ":"+DsnSecretMask+"@", m, 1)
		}
	}
	d.DSN = mergeKeywordPassword(d.DSN, old.DSN)
	if d.SSHPassword == DsnSecretMask {
		d.SSHPassword = old.SSHPassword
	}
	if d.SSHKey == DsnSecretMask {
		d.SSHKey = old.SSHKey
	}
	if d.SSHKeyPassphrase == DsnSecretMask {
		d.SSHKeyPassphrase = old.SSHKeyPassphrase
	}
}

func mergeKeywordPassword(dsn, oldDSN string) string {
	oldMatch := dsnKeywordPwRe.FindStringSubmatch(oldDSN)
	if len(oldMatch) < 4 {
		return dsn
	}
	return dsnKeywordPwRe.ReplaceAllStringFunc(dsn, func(token string) string {
		m := dsnKeywordPwRe.FindStringSubmatch(token)
		if len(m) < 4 || m[3] != DsnSecretMask {
			return token
		}
		return m[1] + m[2] + "=" + oldMatch[3]
	})
}

func (d *DsnRecord) Record() xdb.Record {
	driver := strings.TrimSpace(d.Driver)
	if driver == "" {
		driver = "mysql"
	}
	sshPort := d.SSHPort
	if sshPort == 0 {
		sshPort = 22
	}
	sshAuth := strings.TrimSpace(d.SSHAuth)
	if sshAuth == "" {
		sshAuth = "password"
	}
	return xdb.Record{
		"name":               strings.TrimSpace(d.Name),
		"driver":             driver,
		"dsn":                strings.TrimSpace(d.DSN),
		"remark":             strings.TrimSpace(d.Remark),
		"ssh_enabled":        boolToInt(d.SSHEnabled),
		"ssh_host":           strings.TrimSpace(d.SSHHost),
		"ssh_port":           sshPort,
		"ssh_user":           strings.TrimSpace(d.SSHUser),
		"ssh_auth":           sshAuth,
		"ssh_password":       d.SSHPassword,
		"ssh_key":            d.SSHKey,
		"ssh_key_passphrase": d.SSHKeyPassphrase,
	}
}

func (d *DsnRecord) FromRecord(record xdb.Record) {
	d.Id = record.GetInt("id")
	d.Name = record.GetString("name")
	d.Driver = record.GetString("driver")
	d.DSN = record.GetString("dsn")
	d.Remark = record.GetString("remark")
	d.SSHEnabled = record.GetInt("ssh_enabled") != 0
	d.SSHHost = record.GetString("ssh_host")
	d.SSHPort = record.GetInt("ssh_port")
	d.SSHUser = record.GetString("ssh_user")
	d.SSHAuth = record.GetString("ssh_auth")
	d.SSHPassword = record.GetString("ssh_password")
	d.SSHKey = record.GetString("ssh_key")
	d.SSHKeyPassphrase = record.GetString("ssh_key_passphrase")
	if t := record.GetTime("created_at"); t != nil {
		d.CreatedAt = *t
	}
	if t := record.GetTime("updated_at"); t != nil {
		d.UpdatedAt = *t
	}
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func CreateDsn(d *DsnRecord) (int64, error) {
	if Dsn == nil {
		return 0, fmt.Errorf("dsn model not initialized")
	}
	if strings.TrimSpace(d.Name) == "" {
		return 0, fmt.Errorf("dsn name is empty")
	}
	return Dsn.Insert(d.Record())
}

func GetDsnByID(id int) (*DsnRecord, error) {
	if Dsn == nil {
		return nil, fmt.Errorf("dsn model not initialized")
	}
	record, err := Dsn.First(xdb.WhereEq("id", id))
	if err != nil {
		return nil, err
	}
	r := &DsnRecord{}
	r.FromRecord(record)
	return r, nil
}

func GetDsnByName(name string) (*DsnRecord, error) {
	if Dsn == nil {
		return nil, fmt.Errorf("dsn model not initialized")
	}
	record, err := Dsn.First(xdb.WhereEq("name", strings.TrimSpace(name)))
	if err != nil {
		return nil, err
	}
	r := &DsnRecord{}
	r.FromRecord(record)
	return r, nil
}

func ListDsn() ([]*DsnRecord, error) {
	if Dsn == nil {
		return nil, fmt.Errorf("dsn model not initialized")
	}
	records, err := Dsn.Selects(xdb.OrderByDesc("id"))
	if err != nil {
		return nil, err
	}
	out := make([]*DsnRecord, 0, len(records))
	for _, record := range records {
		r := &DsnRecord{}
		r.FromRecord(record)
		out = append(out, r)
	}
	return out, nil
}

func UpdateDsnByID(id int, updates xdb.Record) error {
	if Dsn == nil {
		return fmt.Errorf("dsn model not initialized")
	}
	if len(updates) == 0 {
		return fmt.Errorf("updates is empty")
	}
	_, err := Dsn.Update(updates, xdb.WhereEq("id", id))
	return err
}

func DeleteDsnByID(id int) error {
	if Dsn == nil {
		return fmt.Errorf("dsn model not initialized")
	}
	_, err := Dsn.Delete(xdb.WhereEq("id", id))
	return err
}

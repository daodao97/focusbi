package dao

import (
	"fmt"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
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

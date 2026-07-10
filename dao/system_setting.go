package dao

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/daodao97/xgo/xdb"
)

var SystemSetting xdb.Model

func GetSystemSetting(name string) (string, bool, error) {
	if SystemSetting == nil {
		return "", false, fmt.Errorf("system_setting model not initialized")
	}
	record, err := SystemSetting.First(xdb.WhereEq("name", strings.TrimSpace(name)))
	if err == xdb.ErrNotFound {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return record.GetString("value"), true, nil
}

func SetSystemSetting(name, value string) error {
	return SetSystemSettings(map[string]string{name: value})
}

// SetSystemSettings 在一个事务内写入一组设置, 避免后台批量保存时部分成功。
func SetSystemSettings(values map[string]string) error {
	changes := make(map[string]*string, len(values))
	for name, value := range values {
		value := value
		changes[name] = &value
	}
	return ApplySystemSettings(changes)
}

// ApplySystemSettings 在一个事务内应用设置; nil 表示删除数据库覆盖并回退配置文件。
func ApplySystemSettings(changes map[string]*string) error {
	if SystemSetting == nil {
		return fmt.Errorf("system_setting model not initialized")
	}
	return SystemSetting.Transaction(func(_ *sql.Tx, txSetting xdb.Model) error {
		for name, value := range changes {
			name = strings.TrimSpace(name)
			if name == "" {
				return fmt.Errorf("setting name is empty")
			}
			if value == nil {
				if _, err := txSetting.Delete(xdb.WhereEq("name", name)); err != nil {
					return err
				}
				continue
			}
			if _, err := txSetting.InsertOrUpdate(xdb.Record{"name": name, "value": *value}, "value"); err != nil {
				return err
			}
		}
		return nil
	})
}

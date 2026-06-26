package datasource

import (
	"fmt"
	"strings"
)

// Column 描述一个表字段。
type Column struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Comment string `json:"comment,omitempty"`
}

// mysqlSysDBs 是 mysql 的系统库, 列举数据库时过滤掉。
var mysqlSysDBs = map[string]bool{
	"information_schema": true, "mysql": true,
	"performance_schema": true, "sys": true,
}

// ListDatabases 返回某数据源下可见的数据库 (mysql) / schema (postgres)。
// sqlite 无多库概念, 返回单个占位 "main"。
func ListDatabases(name string) ([]string, error) {
	driver, _, _, err := resolve(name)
	if err != nil {
		return nil, err
	}
	driver = normalizeDriver(driver)

	switch driver {
	case "sqlite":
		return []string{"main"}, nil
	case "postgres":
		res, err := Query(name, `SELECT schema_name FROM information_schema.schemata
			WHERE schema_name NOT IN ('pg_catalog','information_schema')
			AND schema_name NOT LIKE 'pg_%' ORDER BY schema_name`)
		if err != nil {
			return nil, err
		}
		return firstCol(res), nil
	default: // mysql
		res, err := Query(name, "SHOW DATABASES")
		if err != nil {
			return nil, err
		}
		var dbs []string
		for _, d := range firstCol(res) {
			if !mysqlSysDBs[strings.ToLower(d)] {
				dbs = append(dbs, d)
			}
		}
		return dbs, nil
	}
}

// ListTables 返回某数据源指定库 (db) 下的所有表名。db 为空时使用连接的默认库。
func ListTables(name, db string) ([]string, error) {
	driver, _, _, err := resolve(name)
	if err != nil {
		return nil, err
	}
	driver = normalizeDriver(driver)

	if db != "" && !validIdent(db) {
		return nil, fmt.Errorf("非法库名: %q", db)
	}

	switch driver {
	case "postgres":
		schema := db
		if schema == "" {
			schema = "public"
		}
		res, err := Query(name, `SELECT table_name FROM information_schema.tables
			WHERE table_schema = $1 ORDER BY table_name`, schema)
		if err != nil {
			return nil, err
		}
		return firstCol(res), nil
	case "sqlite":
		res, err := Query(name, `SELECT name FROM sqlite_master WHERE type='table'
			AND name NOT LIKE 'sqlite_%' ORDER BY name`)
		if err != nil {
			return nil, err
		}
		return firstCol(res), nil
	default: // mysql
		q := "SHOW TABLES"
		if db != "" {
			q = "SHOW TABLES FROM `" + db + "`"
		}
		res, err := Query(name, q)
		if err != nil {
			return nil, err
		}
		return firstCol(res), nil
	}
}

// TableColumns 返回某库某表的字段定义。db 为空时使用连接默认库。
func TableColumns(name, db, table string) ([]Column, error) {
	driver, _, _, err := resolve(name)
	if err != nil {
		return nil, err
	}
	driver = normalizeDriver(driver)

	if !validIdent(table) {
		return nil, fmt.Errorf("非法表名: %q", table)
	}
	if db != "" && !validIdent(db) {
		return nil, fmt.Errorf("非法库名: %q", db)
	}

	switch driver {
	case "postgres":
		return pgColumns(name, db, table)
	case "sqlite":
		return sqliteColumns(name, table)
	default:
		return mysqlColumns(name, db, table)
	}
}

func mysqlColumns(name, db, table string) ([]Column, error) {
	ref := "`" + table + "`"
	if db != "" {
		ref = "`" + db + "`.`" + table + "`"
	}
	res, err := Query(name, "SHOW FULL COLUMNS FROM "+ref)
	if err != nil {
		return nil, err
	}
	cols := make([]Column, 0, len(res.Rows))
	for _, row := range res.Rows {
		cols = append(cols, Column{
			Name:    str(row["Field"]),
			Type:    str(row["Type"]),
			Comment: str(row["Comment"]),
		})
	}
	return cols, nil
}

func pgColumns(name, schema, table string) ([]Column, error) {
	if schema == "" {
		schema = "public"
	}
	res, err := Query(name,
		`SELECT column_name, data_type FROM information_schema.columns
		 WHERE table_schema=$1 AND table_name=$2 ORDER BY ordinal_position`, schema, table)
	if err != nil {
		return nil, err
	}
	cols := make([]Column, 0, len(res.Rows))
	for _, row := range res.Rows {
		cols = append(cols, Column{Name: str(row["column_name"]), Type: str(row["data_type"])})
	}
	return cols, nil
}

func sqliteColumns(name, table string) ([]Column, error) {
	res, err := Query(name, "PRAGMA table_info('"+table+"')")
	if err != nil {
		return nil, err
	}
	cols := make([]Column, 0, len(res.Rows))
	for _, row := range res.Rows {
		cols = append(cols, Column{Name: str(row["name"]), Type: str(row["type"])})
	}
	return cols, nil
}

// firstCol 取结果集每行第一列的值 (各驱动列名不同时通用)。
func firstCol(res *QueryResult) []string {
	out := make([]string, 0, len(res.Rows))
	for _, row := range res.Rows {
		for _, c := range res.Columns {
			if v, ok := row[c]; ok && v != nil {
				out = append(out, fmt.Sprint(v))
				break
			}
		}
	}
	return out
}

// validIdent 校验标识符只含合法字符 (字母/数字/下划线/$), 防止注入。
func validIdent(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' ||
			r == '_' || r == '$') {
			return false
		}
	}
	return true
}

func str(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

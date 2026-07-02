package datasource

import (
	"database/sql"
	"database/sql/driver"
	"io"
)

// eofDriver 是一个最小 database/sql 驱动, 仅供测试:
//   - 查询 "BADCONN" 返回 io.EOF (模拟旧版坏连接判断会命中的错误);
//   - 其余查询返回空结果集。
//
// 用于确定性复刻「坏连接出错」, 验证 Query 不会因此 Invalidate 共享池。
type eofDriver struct{}

func (eofDriver) Open(string) (driver.Conn, error) { return eofConn{}, nil }

type eofConn struct{}

func (eofConn) Prepare(q string) (driver.Stmt, error) { return eofStmt{q: q}, nil }
func (eofConn) Close() error                          { return nil }
func (eofConn) Begin() (driver.Tx, error)             { return nil, io.EOF }

type eofStmt struct{ q string }

func (eofStmt) Close() error  { return nil }
func (eofStmt) NumInput() int { return 0 }
func (eofStmt) Exec([]driver.Value) (driver.Result, error) {
	return nil, io.EOF
}
func (s eofStmt) Query([]driver.Value) (driver.Rows, error) {
	if s.q == "BADCONN" {
		return nil, io.EOF // 坏连接
	}
	return eofRows{}, nil
}

// eofRows 是一个空结果集 (无列、无行)。
type eofRows struct{}

func (eofRows) Columns() []string         { return []string{} }
func (eofRows) Close() error              { return nil }
func (eofRows) Next([]driver.Value) error { return io.EOF } // 无行
func init()                               { sql.Register("eofdrv", eofDriver{}) }

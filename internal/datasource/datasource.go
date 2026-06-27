// Package datasource 管理报表系统内定义的多个数据源 (DSN),
// 负责按名称解析连接、缓存连接池, 并执行原始查询。
package datasource

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"xproxy/conf"
	"xproxy/dao"

	"github.com/daodao97/xgo/xdb"

	mysqldriver "github.com/go-sql-driver/mysql" // mysql
	_ "github.com/lib/pq"                        // postgres
	_ "modernc.org/sqlite"                       // sqlite (纯 Go, 无需 CGO)
)

// normalizeDriver 把用户填写的驱动名归一化为 database/sql 注册的驱动名。
//
//	mysql                       -> mysql
//	postgres/postgresql/pg      -> postgres
//	sqlite/sqlite3              -> sqlite
func normalizeDriver(driver string) string {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "mysql":
		return "mysql"
	case "postgres", "postgresql", "pg":
		return "postgres"
	case "sqlite", "sqlite3":
		return "sqlite"
	default:
		return strings.ToLower(strings.TrimSpace(driver))
	}
}

// QueryResult 是一次查询的结果, 保留列顺序。
type QueryResult struct {
	Columns []string         `json:"columns"`
	Rows    []map[string]any `json:"rows"`
}

type pooled struct {
	db        *sql.DB
	driver    string
	signature string // dsn 字符串, 变化时重建连接
}

var (
	mu    sync.Mutex
	cache = map[string]*pooled{}
)

// resolve 返回某个数据源名称对应的 driver、连接串, 以及完整记录 (可能为 nil)。
//   - "default" 直接复用主库配置 (conf.database[default]), rec 为 nil
//   - 其余从 dsn 表读取, rec 携带 ssh 等配置
func resolve(name string) (driver, dsn string, rec *dao.DsnRecord, err error) {
	if name == "" {
		name = "default"
	}

	if name == "default" {
		for _, c := range conf.Get().Database {
			if c.Name == "default" {
				d := c.Driver
				if d == "" {
					d = "mysql"
				}
				return d, c.DSN, nil, nil
			}
		}
		return "", "", nil, fmt.Errorf("default database not configured")
	}

	r, err := dao.GetDsnByName(name)
	if err != nil {
		if err == xdb.ErrNotFound {
			return "", "", nil, fmt.Errorf("dsn %q not found", name)
		}
		return "", "", nil, err
	}
	d := r.Driver
	if d == "" {
		d = "mysql"
	}
	return d, r.DSN, r, nil
}

// get 返回某数据源的 *sql.DB, 按 (driver, dsn, ssh) 指纹缓存; 配置变更时自动重建。
func get(name string) (*sql.DB, error) {
	if name == "" {
		name = "default"
	}

	driver, dsn, rec, err := resolve(name)
	if err != nil {
		return nil, err
	}
	driver = normalizeDriver(driver)

	// 启用 ssh 隧道时 (仅 mysql), 建立隧道并改写连接串的网络类型。
	sig := driver + "|" + dsn
	if rec != nil && rec.SSHEnabled && driver == "mysql" {
		netName, err := ensureTunnel(name, rec)
		if err != nil {
			return nil, err
		}
		dsn, err = rewriteMySQLNet(dsn, netName)
		if err != nil {
			return nil, err
		}
		sig = "ssh|" + sshSignature(rec) + "|" + dsn
	}

	mu.Lock()
	defer mu.Unlock()

	if p, ok := cache[name]; ok {
		if p.signature == sig && p.driver == driver {
			return p.db, nil
		}
		// 配置已变更, 关闭旧连接
		_ = p.db.Close()
		delete(cache, name)
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s datasource: %w", name, err)
	}
	if rec != nil && rec.SSHEnabled && driver == "mysql" {
		// 同一条 ssh client 上的 direct-tcpip channel 数也受跳板机限制。
		// 并发报表下让 database/sql 在本地排队, 不把连接风暴打到 SSH 层。
		db.SetMaxOpenConns(sshMaxConcurrentDials)
		db.SetMaxIdleConns(sshMaxConcurrentDials)
		// ssh 隧道下的连接更易因跳板机断线而失效, 缩短存活/空闲时间,
		// 让连接池尽快淘汰挂在已断隧道上的旧连接, 避免复用时 unexpected EOF。
		db.SetConnMaxLifetime(5 * time.Minute)
		db.SetConnMaxIdleTime(60 * time.Second)
	} else {
		db.SetMaxOpenConns(20)
		db.SetMaxIdleConns(5)
		db.SetConnMaxLifetime(time.Hour)
	}

	cache[name] = &pooled{db: db, driver: driver, signature: sig}
	return db, nil
}

// Ping 校验某数据源可连通。
func Ping(name string) error {
	db, err := get(name)
	if err != nil {
		return err
	}
	ctx, cancel := contextTimeout()
	defer cancel()
	return db.PingContext(ctx)
}

// PingDSN 在不写入配置的情况下, 测试一个临时 driver/dsn 是否可连通。
func PingDSN(driver, dsn string) error {
	driver = normalizeDriver(driver)
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := contextTimeout()
	defer cancel()
	return db.PingContext(ctx)
}

// PingRecord 测试一条 (尚未保存的) 数据源记录是否可连通, 支持 ssh 隧道。
func PingRecord(r *dao.DsnRecord) error {
	driver := normalizeDriver(r.Driver)
	dsn := strings.TrimSpace(r.DSN)

	if r.SSHEnabled && driver == "mysql" {
		// 用临时唯一名建立一次性隧道, 测完即关。
		tunName := "__test__" + r.SSHHost + "_" + fmt.Sprint(r.SSHPort) + "_" + r.SSHUser
		netName, err := ensureTunnel(tunName, r)
		if err != nil {
			return err
		}
		defer closeTunnel(tunName)
		dsn, err = rewriteMySQLNet(dsn, netName)
		if err != nil {
			return err
		}
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx, cancel := contextTimeout()
	defer cancel()
	return db.PingContext(ctx)
}

// Invalidate 关闭并清除某数据源的连接与 ssh 隧道缓存 (配置更新/删除后调用)。
func Invalidate(name string) {
	mu.Lock()
	if p, ok := cache[name]; ok {
		_ = p.db.Close()
		delete(cache, name)
	}
	mu.Unlock()
	closeTunnel(name)
}

// Query 在指定数据源上执行查询, 返回保留列序的结果。
//
// 坏连接 (ssh 隧道断开后池中残留的失效连接) 不在此处主动失效整个连接池:
//   - database/sql 自身会淘汰坏连接 —— 驱动在「确定未执行、可安全重试」时返回
//     driver.ErrBadConn, 库据此自动换连接重试; 连接中途断时返回 ErrInvalidConn,
//     库也会把该连接移出池, 下次取到的是新连接。
//   - 隧道侧自愈: 新连接经 registerTunnelDialer 拨号, 若 ssh client 失效会自动
//     reconnectTunnel 重建 (见 ssh.go)。
//
// 早期版本在这里 isBadConnErr -> Invalidate(name) -> 重试, 会关闭【多个并发查询
// 共用】的整个 *sql.DB 池与隧道, 把正在用该池的其它并发查询连带打断 (unexpected
// EOF), 并触发 Invalidate 正反馈雪崩。并发预取上线后暴露, 故移除。
func Query(name, query string, args ...any) (*QueryResult, error) {
	return queryOnce(name, query, args...)
}

// queryOnce 执行一次查询。
func queryOnce(name, query string, args ...any) (*QueryResult, error) {
	db, err := get(name)
	if err != nil {
		return nil, err
	}

	ctx, cancel := contextTimeout()
	defer cancel()

	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, formatQueryError(err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}

	result := &QueryResult{Columns: cols, Rows: []map[string]any{}}

	for rows.Next() {
		holders := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range holders {
			ptrs[i] = &holders[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = normalize(holders[i])
		}
		result.Rows = append(result.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, formatQueryError(err)
	}
	return result, nil
}

func formatQueryError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("SQL 查询超时（超过 %s）", formatDurationCN(conf.Get().QueryTimeoutDuration()))
	}
	return err
}

func formatDurationCN(d time.Duration) string {
	if d <= 0 {
		return d.String()
	}
	if d%time.Hour == 0 {
		return fmt.Sprintf("%d小时", int(d/time.Hour))
	}
	if d%time.Minute == 0 {
		return fmt.Sprintf("%d分钟", int(d/time.Minute))
	}
	if d%time.Second == 0 {
		min := d / time.Minute
		sec := (d % time.Minute) / time.Second
		if min > 0 {
			return fmt.Sprintf("%d分%d秒", int(min), int(sec))
		}
		return fmt.Sprintf("%d秒", int(sec))
	}
	return d.String()
}

// isBadConnErr 判断错误是否为连接层失效 (值得失效缓存并重试)。
func isBadConnErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysqldriver.ErrInvalidConn) ||
		errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, kw := range []string{
		"invalid connection", "unexpected eof", "broken pipe",
		"connection reset", "use of closed network connection", "bad connection",
		"eof",
	} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// normalize 把 driver 返回的 []byte 转成字符串, 其它类型原样保留。
func normalize(v any) any {
	switch val := v.(type) {
	case []byte:
		return string(val)
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	default:
		return val
	}
}

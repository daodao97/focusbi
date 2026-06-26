package dao

import (
	"fmt"
	"strings"

	"xproxy/conf"
	"xproxy/db/migrations"

	xcache "github.com/daodao97/xgo/cache"
	"github.com/daodao97/xgo/xdb"
	"github.com/daodao97/xgo/xredis"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pressly/goose/v3"
)

func Init() error {
	err := xdb.Inits(conf.Get().Database)
	if err != nil {
		return err
	}
	if err := initSchema(); err != nil {
		return err
	}
	if rdb := xredis.Get(); rdb != nil {
		xdb.SetCache(xcache.NewRedis(rdb, xcache.WithPrefix("xdb")))
	}

	ProjectUser = xdb.New("project_user", xdb.WithCacheKey("id"))
	Dsn = xdb.New("dsn")
	Report = xdb.New("report")
	Subscription = xdb.New("report_subscription")
	ReportVersion = xdb.New("report_version")
	User = xdb.New("user")
	Role = xdb.New("role")
	APIToken = xdb.New("api_token")

	// 引入按数据源权限后, 给旧角色回填全局 dsn:r, 保持升级前行为 (幂等)。
	if err := BackfillRoleDsnPerm(); err != nil {
		return err
	}

	return nil
}

func initSchema() error {
	db, err := xdb.DB("default")
	if err != nil {
		return err
	}
	driver := defaultDBDriver()
	dialect, err := gooseDialect(driver)
	if err != nil {
		return err
	}
	if dialect != "mysql" {
		return fmt.Errorf("auto migration files currently support mysql only, got %q", driver)
	}
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect(dialect); err != nil {
		return err
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}
	return nil
}

func defaultDBDriver() string {
	for _, cfg := range conf.Get().Database {
		if cfg.Name == "default" {
			return strings.TrimSpace(cfg.Driver)
		}
	}
	return ""
}

func gooseDialect(driver string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "", "mysql":
		return "mysql", nil
	case "postgres", "postgresql", "pgx":
		return "postgres", nil
	case "sqlite", "sqlite3":
		return "sqlite3", nil
	default:
		return "", fmt.Errorf("unsupported database driver for migrations: %q", driver)
	}
}

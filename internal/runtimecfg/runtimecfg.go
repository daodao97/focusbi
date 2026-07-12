// Package runtimecfg 提供数据库可动态覆盖、配置文件兜底的运行参数。
package runtimecfg

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"xproxy/conf"
	"xproxy/dao"
)

const (
	ScriptFetchKey        = "engine.script_fetch"
	ReportTimeoutKey      = "engine.report_timeout"
	QueryTimeoutKey       = "engine.query_timeout"
	QueryConcurrencyKey   = "engine.query_concurrency"
	ScriptTimeoutKey      = "engine.script_timeout"
	ScheduleEnabledKey    = "schedule.enabled"
	PublicShareEnabledKey = "security.public_share_enabled"
)

const cacheTTL = 5 * time.Second

type cachedSetting struct {
	value   string
	source  string
	expires time.Time
}

var (
	mu    sync.Mutex
	cache = map[string]cachedSetting{}
)

func get(key, fallback string) (string, string) {
	now := time.Now()
	mu.Lock()
	defer mu.Unlock()
	if entry, ok := cache[key]; ok && now.Before(entry.expires) {
		return entry.value, entry.source
	}
	value, found, err := dao.GetSystemSetting(key)
	source := "database"
	if err != nil || !found {
		value = fallback
		source = "config"
	}
	value = strings.TrimSpace(value)
	cache[key] = cachedSetting{value: value, source: source, expires: now.Add(cacheTTL)}
	return value, source
}

func config() *conf.Conf { return conf.Get() }

func ScriptFetch() (string, string) {
	fallback := "off"
	if c := config(); c != nil {
		fallback = c.Engine.ScriptFetch
	}
	value, source := get(ScriptFetchKey, fallback)
	return normalizeScriptFetch(value), source
}

func QueryTimeout() time.Duration {
	c := config()
	fallback := (3 * time.Minute).String()
	if c != nil {
		fallback = c.QueryTimeoutDuration().String()
	}
	value, _ := get(QueryTimeoutKey, fallback)
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 3 * time.Minute
	}
	return d
}

func ReportTimeout() time.Duration {
	fallback := 10 * time.Minute
	if c := config(); c != nil {
		fallback = c.ReportTimeoutDuration()
	}
	value, _ := get(ReportTimeoutKey, fallback.String())
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 10 * time.Minute
	}
	return d
}

func QueryConcurrency() int {
	fallback := 8
	if c := config(); c != nil {
		fallback = c.QueryConcurrency()
	}
	value, _ := get(QueryConcurrencyKey, strconv.Itoa(fallback))
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 8
	}
	return n
}

func ScriptTimeout() time.Duration {
	fallback := 3 * time.Minute
	if c := config(); c != nil {
		fallback = c.ScriptTimeoutDuration()
	}
	value, _ := get(ScriptTimeoutKey, fallback.String())
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		return 3 * time.Minute
	}
	return d
}

func ScheduleEnabled() bool {
	fallback := true
	if c := config(); c != nil {
		fallback = c.ScheduleEnabled()
	}
	value, _ := get(ScheduleEnabledKey, strconv.FormatBool(fallback))
	enabled, err := strconv.ParseBool(value)
	return fallbackIfInvalidBool(enabled, err, fallback)
}

func PublicShareEnabled() bool {
	fallback := true
	if c := config(); c != nil {
		fallback = c.PublicShareEnabled()
	}
	value, _ := get(PublicShareEnabledKey, strconv.FormatBool(fallback))
	enabled, err := strconv.ParseBool(value)
	return fallbackIfInvalidBool(enabled, err, fallback)
}

func fallbackIfInvalidBool(value bool, err error, fallback bool) bool {
	if err != nil {
		return fallback
	}
	return value
}

// Snapshot 返回后台页面需要的全部有效值与来源。
func Snapshot() (map[string]string, map[string]string) {
	values := map[string]string{}
	sources := map[string]string{}
	readSource := func(key string) {
		_, sources[key] = get(key, "")
	}
	fetch, fetchSource := ScriptFetch()
	values[ScriptFetchKey], sources[ScriptFetchKey] = fetch, fetchSource
	values[QueryTimeoutKey] = QueryTimeout().String()
	values[ReportTimeoutKey] = ReportTimeout().String()
	values[QueryConcurrencyKey] = strconv.Itoa(QueryConcurrency())
	values[ScriptTimeoutKey] = ScriptTimeout().String()
	values[ScheduleEnabledKey] = strconv.FormatBool(ScheduleEnabled())
	values[PublicShareEnabledKey] = strconv.FormatBool(PublicShareEnabled())
	readSource(QueryTimeoutKey)
	readSource(ReportTimeoutKey)
	readSource(QueryConcurrencyKey)
	readSource(ScriptTimeoutKey)
	readSource(ScheduleEnabledKey)
	readSource(PublicShareEnabledKey)
	return values, sources
}

// Update 完整校验后以单个事务保存一组受支持的动态设置。
// 与配置文件默认值相同的项会删除数据库覆盖, 避免无意义地变成动态配置。
func Update(values map[string]string) error {
	normalized := make(map[string]string, len(values))
	defaults := make(map[string]string, len(values))
	changes := make(map[string]*string, len(values))
	for key, value := range values {
		v, err := validate(key, value)
		if err != nil {
			return err
		}
		normalized[key] = v
		fallback, err := normalizedDefault(key)
		if err != nil {
			return err
		}
		defaults[key] = fallback
		if v == fallback {
			changes[key] = nil
		} else {
			value := v
			changes[key] = &value
		}
	}
	if err := dao.ApplySystemSettings(changes); err != nil {
		return err
	}
	now := time.Now()
	mu.Lock()
	for key, value := range normalized {
		source := "database"
		if changes[key] == nil {
			value = defaults[key]
			source = "config"
		}
		cache[key] = cachedSetting{value: value, source: source, expires: now.Add(cacheTTL)}
	}
	mu.Unlock()
	return nil
}

func normalizedDefault(key string) (string, error) {
	c := config()
	value := ""
	switch key {
	case ScriptFetchKey:
		value = "off"
		if c != nil {
			value = c.Engine.ScriptFetch
		}
	case QueryTimeoutKey:
		value = (3 * time.Minute).String()
		if c != nil {
			value = c.QueryTimeoutDuration().String()
		}
	case ReportTimeoutKey:
		value = (10 * time.Minute).String()
		if c != nil {
			value = c.ReportTimeoutDuration().String()
		}
	case QueryConcurrencyKey:
		value = "8"
		if c != nil {
			value = strconv.Itoa(c.QueryConcurrency())
		}
	case ScriptTimeoutKey:
		value = (3 * time.Minute).String()
		if c != nil {
			value = c.ScriptTimeoutDuration().String()
		}
	case ScheduleEnabledKey:
		value = "true"
		if c != nil {
			value = strconv.FormatBool(c.ScheduleEnabled())
		}
	case PublicShareEnabledKey:
		value = "true"
		if c != nil {
			value = strconv.FormatBool(c.PublicShareEnabled())
		}
	default:
		return "", fmt.Errorf("不支持的动态配置: %s", key)
	}
	return validate(key, value)
}

func SetScriptFetch(mode string) error {
	return Update(map[string]string{ScriptFetchKey: mode})
}

func validate(key, value string) (string, error) {
	value = strings.TrimSpace(value)
	switch key {
	case ScriptFetchKey:
		value = normalizeScriptFetch(value)
		if err := conf.ValidateScriptFetchMode(value); err != nil {
			return "", err
		}
	case QueryTimeoutKey, ScriptTimeoutKey, ReportTimeoutKey:
		d, err := time.ParseDuration(value)
		if err != nil || d < time.Second || d > 30*time.Minute {
			return "", fmt.Errorf("%s 必须在 1s 到 30m 之间", key)
		}
		value = d.String()
	case QueryConcurrencyKey:
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 64 {
			return "", fmt.Errorf("%s 必须在 1 到 64 之间", key)
		}
		value = strconv.Itoa(n)
	case ScheduleEnabledKey, PublicShareEnabledKey:
		v, err := strconv.ParseBool(value)
		if err != nil {
			return "", fmt.Errorf("%s 必须是布尔值", key)
		}
		value = strconv.FormatBool(v)
	default:
		return "", fmt.Errorf("不支持的动态配置: %s", key)
	}
	return value, nil
}

func normalizeScriptFetch(mode string) string {
	switch {
	case mode == "", strings.EqualFold(mode, "off"):
		return "off"
	case strings.EqualFold(mode, "on"):
		return "on"
	default:
		return mode
	}
}

// Invalidate 清理本实例缓存, 主要供测试及未来批量更新复用。
func Invalidate() {
	mu.Lock()
	cache = map[string]cachedSetting{}
	mu.Unlock()
}

package conf

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"strings"
	"time"

	"github.com/daodao97/xgo/xapp"
	"github.com/daodao97/xgo/xdb"
	"github.com/daodao97/xgo/xredis"
	"github.com/joho/godotenv"
)

// defaultJWTSecret 是未配置时的占位密钥; 用它签名等于谁都能伪造 token,
// 故启动时会拒绝 (见 Init)。
const defaultJWTSecret = "focusbi_default_jwt_secret_change_me"

// AIConf 配置对话式报表修改使用的大模型服务。
// Provider 决定 Eino 使用的 provider: claude/anthropic (默认/优先) 或 openai。
type AIConf struct {
	Provider string `json:"provider" yaml:"provider" env:"PROVIDER"` // claude | anthropic | openai
	BaseURL  string `json:"base_url" yaml:"base_url" env:"BASE_URL"`
	APIKey   string `json:"api_key" yaml:"api_key" env:"API_KEY"`
	Model    string `json:"model" yaml:"model" env:"MODEL"`
}

// TurnstileConf 配置 Cloudflare Turnstile 登录人机验证。
// 同时配置 site_key 与 secret_key 后启用; 未配置时本地开发不受影响。
type TurnstileConf struct {
	SiteKey   string `json:"site_key" yaml:"site_key" env:"SITE_KEY"`
	SecretKey string `json:"secret_key" yaml:"secret_key" env:"SECRET_KEY"`
}

// SiteConf 是站点 / 服务级配置。
type SiteConf struct {
	// URL 站点对外可访问地址 (如 https://bi.example.com), 用于定时任务里拼报表查看链接。
	// 留空时推送消息不带链接。
	URL string `json:"url" yaml:"url" env:"URL"`
	// JWTSecret 登录 token 的签名密钥 (必填)。为空或默认占位值时拒绝启动 (见 Init)。
	JWTSecret string `json:"jwt_secret" yaml:"jwt_secret" env:"JWT_SECRET"`
}

// ScheduleConf 配置定时任务运行时默认开关。ENABLE_CRON 仍决定是否启动调度器。
type ScheduleConf struct {
	Enabled *bool `json:"enabled" yaml:"enabled" env:"ENABLED"`
}

// SecurityConf 配置可在后台动态覆盖的安全功能默认值。
type SecurityConf struct {
	PublicShareEnabled *bool `json:"public_share_enabled" yaml:"public_share_enabled" env:"PUBLIC_SHARE_ENABLED"`
}

// EngineConf 是报表执行引擎配置。
type EngineConf struct {
	// QueryTimeout 单次数据源查询超时, Go duration 字符串, 如 "30s" / "3m"。
	// 为空或非法时使用默认值。
	QueryTimeout string `json:"query_timeout" yaml:"query_timeout" env:"QUERY_TIMEOUT"`
	// QueryConcurrency 同一报表内独立 SQL 区块的并发查询数 (worker 池大小)。
	// <=0 时回退默认值; 设为 1 即退回逐块串行执行。
	QueryConcurrency int `json:"query_concurrency" yaml:"query_concurrency" env:"QUERY_CONCURRENCY"`
	// ScriptTimeout 单个脚本区块的最大执行时间, Go duration 字符串。
	ScriptTimeout string `json:"script_timeout" yaml:"script_timeout" env:"SCRIPT_TIMEOUT"`
	// ScriptFetch 控制报表脚本里 fetch() 的外呼权限 (SSRF 面):
	//   "" / "off" -> 禁用 fetch (安全默认)
	//   "on"        -> 允许公网 http/https, 网络层仍拒绝私网/环回地址
	//   其它         -> 逗号分隔的 URL 白名单, 按协议/主机/端口/路径匹配
	ScriptFetch string `json:"script_fetch" yaml:"script_fetch" env:"SCRIPT_FETCH"`
}

type Conf struct {
	Database  []xdb.Config     `json:"database" yaml:"database" envPrefix:"DATABASE"`
	Redis     []xredis.Options `json:"redis" yaml:"redis" envPrefix:"REDIS"`
	AI        AIConf           `json:"ai" yaml:"ai" envPrefix:"AI_"`
	Turnstile TurnstileConf    `json:"turnstile" yaml:"turnstile" envPrefix:"TURNSTILE_"`
	Site      SiteConf         `json:"site" yaml:"site" envPrefix:"SITE_"`
	Engine    EngineConf       `json:"engine" yaml:"engine" envPrefix:"ENGINE_"`
	Schedule  ScheduleConf     `json:"schedule" yaml:"schedule" envPrefix:"SCHEDULE_"`
	Security  SecurityConf     `json:"security" yaml:"security" envPrefix:"SECURITY_"`
}

// SiteBaseURL 返回站点地址 (去掉尾部斜杠), 仅从配置读取。
func (c *Conf) SiteBaseURL() string {
	return strings.TrimRight(strings.TrimSpace(c.Site.URL), "/")
}

// JWTSecretOrDefault 返回 JWT 签名密钥, 未配置时回退到固定占位值。
// 占位值不安全, Init 会在启动时拒绝它 (见 Init); 这里保留回退仅为兼容单测等无配置场景。
func (c *Conf) JWTSecretOrDefault() string {
	if s := strings.TrimSpace(c.Site.JWTSecret); s != "" {
		return s
	}
	return defaultJWTSecret
}

const defaultQueryTimeout = 3 * time.Minute
const defaultScriptTimeout = 3 * time.Minute

// QueryTimeoutDuration 返回单次数据源查询超时。未配置/非法/非正数时回退到默认 3 分钟。
func (c *Conf) QueryTimeoutDuration() time.Duration {
	if c == nil {
		return defaultQueryTimeout
	}
	s := strings.TrimSpace(c.Engine.QueryTimeout)
	if s == "" {
		return defaultQueryTimeout
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return defaultQueryTimeout
	}
	return d
}

// ScriptTimeoutDuration 返回单个脚本区块的最大执行时间。未配置/非法时回退到默认 3 分钟。
func (c *Conf) ScriptTimeoutDuration() time.Duration {
	if c == nil {
		return defaultScriptTimeout
	}
	d, err := time.ParseDuration(strings.TrimSpace(c.Engine.ScriptTimeout))
	if err != nil || d <= 0 {
		return defaultScriptTimeout
	}
	return d
}

// ScheduleEnabled 返回定时任务运行时默认开关。未配置时保持兼容, 默认开启。
func (c *Conf) ScheduleEnabled() bool {
	return c == nil || c.Schedule.Enabled == nil || *c.Schedule.Enabled
}

// PublicShareEnabled 返回公开链接分享的默认开关。未配置时保持兼容, 默认开启。
func (c *Conf) PublicShareEnabled() bool {
	return c == nil || c.Security.PublicShareEnabled == nil || *c.Security.PublicShareEnabled
}

const defaultQueryConcurrency = 8

// QueryConcurrency 返回报表内独立 SQL 区块的并发查询数。未配置/非正数时回退默认 8。
func (c *Conf) QueryConcurrency() int {
	if c == nil || c.Engine.QueryConcurrency <= 0 {
		return defaultQueryConcurrency
	}
	return c.Engine.QueryConcurrency
}

// ScriptFetchAllowed 判断脚本 fetch() 是否允许请求该 URL (见 EngineConf.ScriptFetch)。
func (c *Conf) ScriptFetchAllowed(url string) error {
	mode := ""
	if c != nil {
		mode = strings.TrimSpace(c.Engine.ScriptFetch)
	}
	return ScriptFetchAllowed(mode, url)
}

// ScriptFetchAllowed 按给定运行模式校验目标 URL。
func ScriptFetchAllowed(mode, rawURL string) error {
	mode = strings.TrimSpace(mode)
	switch strings.ToLower(mode) {
	case "", "off":
		return fmt.Errorf("脚本 fetch 已禁用 (engine.script_fetch=off)")
	case "on":
		return validateHTTPURL(rawURL)
	}
	target, err := urlpkg(rawURL)
	if err != nil {
		return err
	}
	for _, rawRule := range strings.Split(mode, ",") {
		rule, err := urlpkg(strings.TrimSpace(rawRule))
		if err != nil || !sameURLAuthority(rule, target) {
			continue
		}
		rulePath := normalizedURLPath(rule)
		targetPath := normalizedURLPath(target)
		if rulePath == "" || targetPath == rulePath || strings.HasPrefix(targetPath, rulePath+"/") {
			return nil
		}
	}
	return fmt.Errorf("脚本 fetch 目标不在白名单内 (engine.script_fetch): %s", rawURL)
}

func normalizedURLPath(u *url.URL) string {
	p := path.Clean("/" + strings.TrimPrefix(u.Path, "/"))
	if p == "/" || p == "." {
		return ""
	}
	return strings.TrimSuffix(p, "/")
}

// ScriptFetchHostExplicitlyAllowed 判断某主机是否在显式白名单中。
// 显式配置代表管理员有意允许该主机, 网络层据此决定是否允许私网地址。
func (c *Conf) ScriptFetchHostExplicitlyAllowed(host string) bool {
	if c == nil {
		return false
	}
	return ScriptFetchHostExplicitlyAllowed(c.Engine.ScriptFetch, host)
}

func ScriptFetchHostExplicitlyAllowed(mode, host string) bool {
	mode = strings.TrimSpace(mode)
	if mode == "" || strings.EqualFold(mode, "off") || strings.EqualFold(mode, "on") {
		return false
	}
	for _, rawRule := range strings.Split(mode, ",") {
		rule, err := urlpkg(strings.TrimSpace(rawRule))
		if err == nil && strings.EqualFold(rule.Hostname(), host) {
			return true
		}
	}
	return false
}

// ValidateScriptFetchMode 校验后台保存的模式是否合法。
func ValidateScriptFetchMode(mode string) error {
	mode = strings.TrimSpace(mode)
	if mode == "" || strings.EqualFold(mode, "off") || strings.EqualFold(mode, "on") {
		return nil
	}
	for _, rawRule := range strings.Split(mode, ",") {
		if _, err := urlpkg(strings.TrimSpace(rawRule)); err != nil {
			return fmt.Errorf("无效的 script_fetch 白名单 %q: %w", strings.TrimSpace(rawRule), err)
		}
	}
	return nil
}

func validateHTTPURL(raw string) error {
	_, err := urlpkg(raw)
	return err
}

func urlpkg(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return nil, fmt.Errorf("脚本 fetch 仅允许无凭据的 http/https URL")
	}
	return u, nil
}

func sameURLAuthority(a, b *url.URL) bool {
	return strings.EqualFold(a.Scheme, b.Scheme) &&
		strings.EqualFold(a.Hostname(), b.Hostname()) && a.Port() == b.Port()
}

var ConfInstance *Conf

func Init() error {
	ConfInstance = &Conf{}

	if err := loadDotEnv(".env"); err != nil {
		return err
	}

	err := xapp.InitConf(ConfInstance)
	if err != nil {
		return err
	}

	// 安全校验: 拒绝用默认占位 JWT 密钥启动 (否则任何人都能伪造登录 token)。
	if err := ConfInstance.validateJWTSecret(); err != nil {
		return err
	}

	return nil
}

func loadDotEnv(path string) error {
	if err := godotenv.Load(path); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("读取 .env 失败: %w", err)
	}
	return nil
}

// validateJWTSecret 校验 JWT 密钥: 为空或仍是默认占位值时返回错误。
func (c *Conf) validateJWTSecret() error {
	if s := strings.TrimSpace(c.Site.JWTSecret); s == "" || s == defaultJWTSecret {
		return fmt.Errorf("必须在配置 site.jwt_secret 设置一个非默认的密钥 (当前为空或仍是默认占位值); 可用 `openssl rand -hex 32` 生成")
	}
	return nil
}

func Get() *Conf {
	return ConfInstance
}

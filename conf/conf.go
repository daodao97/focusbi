package conf

import (
	"fmt"
	"os"
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

// EngineConf 是报表执行引擎配置。
type EngineConf struct {
	// QueryTimeout 单次数据源查询超时, Go duration 字符串, 如 "30s" / "3m"。
	// 为空或非法时使用默认值。
	QueryTimeout string `json:"query_timeout" yaml:"query_timeout" env:"QUERY_TIMEOUT"`
	// QueryConcurrency 同一报表内独立 SQL 区块的并发查询数 (worker 池大小)。
	// <=0 时回退默认值; 设为 1 即退回逐块串行执行。
	QueryConcurrency int `json:"query_concurrency" yaml:"query_concurrency" env:"QUERY_CONCURRENCY"`
	// ScriptFetch 控制报表脚本里 fetch() 的外呼权限 (SSRF 面):
	//   "" / "on"  -> 允许任意外呼 (兼容旧行为, 仅内网信任环境)
	//   "off"      -> 禁用 fetch
	//   其它       -> 逗号分隔的 URL 前缀白名单, 如 "https://api.example.com,https://open.feishu.cn"
	ScriptFetch string `json:"script_fetch" yaml:"script_fetch" env:"SCRIPT_FETCH"`
}

type Conf struct {
	Database  []xdb.Config     `json:"database" yaml:"database" envPrefix:"DATABASE"`
	Redis     []xredis.Options `json:"redis" yaml:"redis" envPrefix:"REDIS"`
	AI        AIConf           `json:"ai" yaml:"ai" envPrefix:"AI_"`
	Turnstile TurnstileConf    `json:"turnstile" yaml:"turnstile" envPrefix:"TURNSTILE_"`
	Site      SiteConf         `json:"site" yaml:"site" envPrefix:"SITE_"`
	Engine    EngineConf       `json:"engine" yaml:"engine" envPrefix:"ENGINE_"`
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
	switch strings.ToLower(mode) {
	case "", "on":
		return nil
	case "off":
		return fmt.Errorf("脚本 fetch 已禁用 (engine.script_fetch=off)")
	}
	for _, prefix := range strings.Split(mode, ",") {
		prefix = strings.TrimSpace(prefix)
		if prefix == "" || !strings.HasPrefix(url, prefix) {
			continue
		}
		// 前缀须止于边界, 防 "https://api.example.com" 放行 "https://api.example.com.evil.com"。
		rest := url[len(prefix):]
		if rest == "" || strings.HasSuffix(prefix, "/") ||
			rest[0] == '/' || rest[0] == '?' || rest[0] == '#' || rest[0] == ':' {
			return nil
		}
	}
	return fmt.Errorf("脚本 fetch 目标不在白名单内 (engine.script_fetch): %s", url)
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

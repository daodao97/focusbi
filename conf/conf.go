package conf

import (
	"fmt"
	"strings"

	"github.com/daodao97/xgo/xapp"
	"github.com/daodao97/xgo/xdb"
	"github.com/daodao97/xgo/xredis"
)

// defaultJWTSecret 是未配置时的占位密钥; 用它签名等于谁都能伪造 token,
// 故启动时会拒绝 (见 Init)。
const defaultJWTSecret = "focusbi_default_jwt_secret_change_me"

// AIConf 配置对话式报表修改使用的大模型服务。
// Provider 决定 Eino 使用的 provider: claude/anthropic (默认/优先) 或 openai。
type AIConf struct {
	Provider string `json:"provider" yaml:"provider"` // claude | anthropic | openai
	BaseURL  string `json:"base_url" yaml:"base_url"`
	APIKey   string `json:"api_key" yaml:"api_key"`
	Model    string `json:"model" yaml:"model"`
}

// TurnstileConf 配置 Cloudflare Turnstile 登录人机验证。
// 同时配置 site_key 与 secret_key 后启用; 未配置时本地开发不受影响。
type TurnstileConf struct {
	SiteKey   string `json:"site_key" yaml:"site_key"`
	SecretKey string `json:"secret_key" yaml:"secret_key"`
}

// SiteConf 是站点 / 服务级配置。
type SiteConf struct {
	// URL 站点对外可访问地址 (如 https://bi.example.com), 用于订阅推送里拼报表查看链接。
	// 留空时推送消息不带链接。
	URL string `json:"url" yaml:"url"`
	// JWTSecret 登录 token 的签名密钥 (必填)。为空或默认占位值时拒绝启动 (见 Init)。
	JWTSecret string `json:"jwt_secret" yaml:"jwt_secret"`
}

type Conf struct {
	Database  []xdb.Config     `json:"database" yaml:"database" envPrefix:"DATABASE"`
	Redis     []xredis.Options `json:"redis" yaml:"redis" envPrefix:"REDIS"`
	AI        AIConf           `json:"ai" yaml:"ai"`
	Turnstile TurnstileConf    `json:"turnstile" yaml:"turnstile"`
	Site      SiteConf         `json:"site" yaml:"site"`
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

var ConfInstance *Conf

func Init() error {
	ConfInstance = &Conf{}

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

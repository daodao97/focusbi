package subscription

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// webhook 地址校验 (防 SSRF):
// 订阅 webhook 由用户填写, 服务端会主动 POST 它, 因此必须限制目标, 防止被用于
// 探测内网 / 云元数据 / 环回服务。本期只支持飞书 / 企业微信, 采用"已知域名后缀白名单"
// 为主、内网 IP 黑名单兜底的策略。

// 允许的 webhook 域名后缀 (飞书国内/国际 + 企业微信)。
var allowedWebhookHosts = []string{
	"open.feishu.cn",
	"open.larksuite.com",
	"qyapi.weixin.qq.com",
}

// ValidateWebhook 校验 webhook URL 是否可安全请求。返回 nil 表示通过。
func ValidateWebhook(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fmt.Errorf("webhook 不能为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webhook 地址非法: %v", err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("webhook 必须使用 https")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("webhook 缺少主机名")
	}

	// 主防线: 域名后缀白名单 (飞书/企微官方域名)。
	if !hostAllowed(host) {
		return fmt.Errorf("webhook 域名不在白名单 (仅支持飞书 / 企业微信)")
	}

	// 兜底: 即便白名单内, 若主机被解析/填写为内网 IP 也拒绝 (防 DNS 重绑定/直填 IP)。
	if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
		return fmt.Errorf("webhook 不允许指向内网地址")
	}
	return nil
}

// hostAllowed 判断 host 是否命中白名单域名 (精确或子域)。
func hostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, allowed := range allowedWebhookHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// isPrivateIP 判断 IP 是否属于环回 / 私网 / 链路本地 / 未指定等不可对外的范围。
func isPrivateIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
		return true
	}
	return false
}

package schedule

import "testing"

func TestValidateWebhook(t *testing.T) {
	cases := []struct {
		name string
		url  string
		ok   bool
	}{
		{"飞书国内", "https://open.feishu.cn/open-apis/bot/v2/hook/abc", true},
		{"飞书国际", "https://open.larksuite.com/open-apis/bot/v2/hook/abc", true},
		{"企业微信", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x", true},
		{"空", "", false},
		{"http 非 https", "http://open.feishu.cn/hook/abc", false},
		{"非白名单域名", "https://evil.com/hook", false},
		{"环回", "https://127.0.0.1/hook", false},
		{"内网 IP", "https://192.168.1.1/hook", false},
		{"云元数据", "https://169.254.169.254/latest/meta-data/", false},
		{"白名单的钓鱼子域", "https://open.feishu.cn.evil.com/hook", false},
		{"非法 URL", "https://%zz", false},
	}
	for _, c := range cases {
		err := ValidateWebhook(c.url)
		if (err == nil) != c.ok {
			t.Errorf("%s: ValidateWebhook(%q) err=%v, want ok=%v", c.name, c.url, err, c.ok)
		}
	}
}

func TestResolveChannel(t *testing.T) {
	cases := []struct {
		channel, webhook, want string
	}{
		// 企微地址即便 channel 选了 lark, 也纠正为企微 (本 bug)
		{"lark", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x", ChannelWework},
		{"", "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=x", ChannelWework},
		{"wework", "https://open.feishu.cn/open-apis/bot/v2/hook/abc", ChannelLark},
		{"lark", "https://open.larksuite.com/open-apis/bot/v2/hook/abc", ChannelLark},
		// host 无法识别时按传入 channel
		{"wework", "https://example.com/hook", "wework"},
	}
	for _, c := range cases {
		if got := resolveChannel(c.channel, c.webhook); got != c.want {
			t.Errorf("resolveChannel(%q,%q)=%q, want %q", c.channel, c.webhook, got, c.want)
		}
	}
}

func TestHostAllowedSubdomain(t *testing.T) {
	if !hostAllowed("open.feishu.cn") {
		t.Error("精确域名应通过")
	}
	if hostAllowed("feishu.cn") {
		t.Error("父域名不应通过 (仅允许指定后缀)")
	}
	if hostAllowed("evilopen.feishu.cn.attacker.com") {
		t.Error("伪装子域不应通过")
	}
}

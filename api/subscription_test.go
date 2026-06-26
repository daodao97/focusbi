package api

import "testing"

func TestMaskWebhook(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", ""},
		{"short", "****"},
		{"https://open.feishu.cn/open-apis/bot/v2/hook/abcdef123456", "****123456"},
	}
	for _, c := range cases {
		if got := maskWebhook(c.in); got != c.want {
			t.Errorf("maskWebhook(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

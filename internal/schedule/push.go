package schedule

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// 渠道常量。
const (
	ChannelLark   = "lark"   // 飞书
	ChannelWework = "wework" // 企业微信
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

// push 按渠道构造文本消息体, 直接 POST 到用户配置的完整 webhook URL。
// 不依赖 xnotify (其飞书 host 硬编码为国际版 larksuite, 国内飞书发不出去)。
func push(channel, webhook, text string) error {
	webhook = strings.TrimSpace(webhook)
	// 纵深防御: 即便库中存了非法/内网地址 (历史数据或绕过 API), 推送前再校验一次。
	if err := ValidateWebhook(webhook); err != nil {
		return err
	}
	return postJSON(webhook, buildPayload(resolveChannel(channel, webhook), text))
}

// resolveChannel 以 webhook host 为准纠正渠道: 企微地址 (qyapi.weixin.qq.com) 一律
// 按企微发, 避免用户 channel 选错 (如默认 lark) 却填了企微地址, 导致消息体格式不符
// 被企微拒收 (invalid message type)。host 无法伪装, 比 channel 字段可靠。
func resolveChannel(channel, webhook string) string {
	if strings.Contains(webhook, "qyapi.weixin.qq.com") {
		return ChannelWework
	}
	if strings.Contains(webhook, "feishu.cn") || strings.Contains(webhook, "larksuite.com") {
		return ChannelLark
	}
	return channel
}

// buildPayload 按渠道构造文本消息体 (飞书 / 企业微信)。
func buildPayload(channel, text string) any {
	switch channel {
	case ChannelWework:
		return map[string]any{
			"msgtype": "text",
			"text":    map[string]any{"content": text},
		}
	default: // lark / 飞书
		return map[string]any{
			"msg_type": "text",
			"content":  map[string]any{"text": text},
		}
	}
}

// postJSON 发送 JSON 并校验飞书/企微的返回码 (两者成功码均为 0)。
func postJSON(url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook 返回 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// 飞书: {"code":0,...} 或 {"StatusCode":0}; 企微: {"errcode":0,...}
	var r struct {
		Code       *int   `json:"code"`
		StatusCode *int   `json:"StatusCode"`
		ErrCode    *int   `json:"errcode"`
		Msg        string `json:"msg"`
		ErrMsg     string `json:"errmsg"`
	}
	if json.Unmarshal(raw, &r) == nil {
		if code := firstNonNil(r.ErrCode, r.Code, r.StatusCode); code != 0 {
			msg := r.ErrMsg
			if msg == "" {
				msg = r.Msg
			}
			return fmt.Errorf("webhook 推送失败 (code=%d): %s", code, msg)
		}
	}
	return nil
}

func firstNonNil(vals ...*int) int {
	for _, v := range vals {
		if v != nil {
			return *v
		}
	}
	return 0
}

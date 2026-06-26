package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xproxy/conf"

	"github.com/gin-gonic/gin"
)

const turnstileSiteVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

type turnstileVerifyResp struct {
	Success    bool     `json:"success"`
	ErrorCodes []string `json:"error-codes"`
}

func turnstileConfig() conf.TurnstileConf {
	if conf.Get() == nil {
		return conf.TurnstileConf{}
	}
	return conf.Get().Turnstile
}

func turnstileEnabled() bool {
	cfg := turnstileConfig()
	return strings.TrimSpace(cfg.SiteKey) != "" && strings.TrimSpace(cfg.SecretKey) != ""
}

func turnstilePublicConfig() gin.H {
	cfg := turnstileConfig()
	if !turnstileEnabled() {
		return gin.H{"enabled": false, "site_key": ""}
	}
	return gin.H{"enabled": true, "site_key": strings.TrimSpace(cfg.SiteKey)}
}

func verifyTurnstile(ctx context.Context, token string, remoteIP string) error {
	cfg := turnstileConfig()
	if !turnstileEnabled() {
		return nil
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("请先完成人机验证")
	}

	form := url.Values{}
	form.Set("secret", strings.TrimSpace(cfg.SecretKey))
	form.Set("response", token)
	if strings.TrimSpace(remoteIP) != "" {
		form.Set("remoteip", strings.TrimSpace(remoteIP))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, turnstileSiteVerifyURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	client := &http.Client{Timeout: 8 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("人机验证服务暂时不可用")
	}
	defer res.Body.Close()

	var out turnstileVerifyResp
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return errors.New("人机验证响应解析失败")
	}
	if !out.Success {
		return errors.New("人机验证失败, 请重试")
	}
	return nil
}

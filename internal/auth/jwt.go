package auth

import (
	"fmt"
	"time"

	"xproxy/conf"

	"github.com/daodao97/xgo/xjwt"
	"github.com/golang-jwt/jwt/v5"
)

const tokenTTL = 7 * 24 * time.Hour // 7 天

// Claims 是 token 中携带的用户身份。
type Claims struct {
	UID      int    `json:"uid"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
}

// IssueToken 为用户签发 JWT。
func IssueToken(uid int, username string, isAdmin bool) (string, error) {
	now := time.Now()
	payload := jwt.MapClaims{
		"uid":      uid,
		"username": username,
		"is_admin": isAdmin,
		"iat":      now.Unix(),
		"exp":      now.Add(tokenTTL).Unix(),
	}
	return xjwt.GenHMacToken(payload, conf.Get().JWTSecretOrDefault())
}

// ParseToken 校验并解析 JWT。
func ParseToken(tokenStr string) (*Claims, error) {
	mc, err := xjwt.VerifyHMacToken(tokenStr, conf.Get().JWTSecretOrDefault())
	if err != nil {
		return nil, err
	}
	c := &Claims{
		Username: asString(mc["username"]),
		IsAdmin:  asBool(mc["is_admin"]),
	}
	c.UID = asInt(mc["uid"])
	if c.UID == 0 {
		return nil, fmt.Errorf("invalid token: missing uid")
	}
	return c, nil
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

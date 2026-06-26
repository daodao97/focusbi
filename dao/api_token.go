package dao

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/daodao97/xgo/xdb"
)

// APIToken 是供 MCP 等程序化访问使用的长期令牌模型。
var APIToken xdb.Model

// apiTokenPrefix 是明文令牌的固定前缀, 便于识别与按前缀路由鉴权。
const apiTokenPrefix = "fbt_"

// APITokenRecord 是一条 API 令牌; 只存明文的 SHA-256 哈希, 不存明文。
type APITokenRecord struct {
	Id          int        `json:"id"`
	UserID      int        `json:"user_id"`
	Name        string     `json:"name"`
	TokenHash   string     `json:"-"`            // 明文 SHA-256, 不下发
	TokenPrefix string     `json:"token_prefix"` // 前缀明文 (如 fbt_1a2b3c4d), 仅供辨识
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (t *APITokenRecord) FromRecord(record xdb.Record) {
	t.Id = record.GetInt("id")
	t.UserID = record.GetInt("user_id")
	t.Name = record.GetString("name")
	t.TokenHash = record.GetString("token_hash")
	t.TokenPrefix = record.GetString("token_prefix")
	t.LastUsedAt = record.GetTime("last_used_at")
	t.ExpiresAt = record.GetTime("expires_at")
	if ct := record.GetTime("created_at"); ct != nil {
		t.CreatedAt = *ct
	}
}

// HashAPIToken 计算明文令牌的存储哈希 (SHA-256 hex)。鉴权与建库共用。
func HashAPIToken(plain string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(plain)))
	return hex.EncodeToString(sum[:])
}

// CreateAPIToken 生成一个新令牌: 返回的明文 plain 仅此一次可见 (调用方需立即展示给用户)。
// ttl 为 nil 表示永不过期。
func CreateAPIToken(userID int, name string, ttl *time.Duration) (plain string, rec *APITokenRecord, err error) {
	if APIToken == nil {
		return "", nil, fmt.Errorf("api_token model not initialized")
	}
	if userID <= 0 {
		return "", nil, fmt.Errorf("invalid user id")
	}
	// 32 字节随机 -> 64 hex 字符, 前缀拼接成明文令牌。
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", nil, fmt.Errorf("generate token: %w", err)
	}
	plain = apiTokenPrefix + hex.EncodeToString(raw)
	prefix := plain
	if len(prefix) > 12 {
		prefix = prefix[:12] // 如 fbt_1a2b3c4d, 仅用于列表辨识
	}

	record := xdb.Record{
		"user_id":      userID,
		"name":         strings.TrimSpace(name),
		"token_hash":   HashAPIToken(plain),
		"token_prefix": prefix,
	}
	if ttl != nil {
		record["expires_at"] = time.Now().Add(*ttl)
	}
	id, err := APIToken.Insert(record)
	if err != nil {
		return "", nil, err
	}
	rec = &APITokenRecord{
		Id:          int(id),
		UserID:      userID,
		Name:        strings.TrimSpace(name),
		TokenPrefix: prefix,
		CreatedAt:   time.Now(),
	}
	if ttl != nil {
		exp := time.Now().Add(*ttl)
		rec.ExpiresAt = &exp
	}
	return plain, rec, nil
}

// GetAPITokenByHash 按明文哈希查令牌 (鉴权用)。未找到返回 xdb.ErrNotFound。
func GetAPITokenByHash(hash string) (*APITokenRecord, error) {
	if APIToken == nil {
		return nil, fmt.Errorf("api_token model not initialized")
	}
	record, err := APIToken.First(xdb.WhereEq("token_hash", hash))
	if err != nil {
		return nil, err
	}
	r := &APITokenRecord{}
	r.FromRecord(record)
	return r, nil
}

// ListAPITokensByUser 列出某用户的全部令牌 (不含哈希/明文)。
func ListAPITokensByUser(userID int) ([]*APITokenRecord, error) {
	if APIToken == nil {
		return nil, fmt.Errorf("api_token model not initialized")
	}
	records, err := APIToken.Selects(xdb.WhereEq("user_id", userID), xdb.OrderByDesc("id"))
	if err != nil {
		return nil, err
	}
	out := make([]*APITokenRecord, 0, len(records))
	for _, record := range records {
		r := &APITokenRecord{}
		r.FromRecord(record)
		out = append(out, r)
	}
	return out, nil
}

// DeleteAPIToken 删除令牌 (限本人: 带 user_id 条件防越权)。
func DeleteAPIToken(id, userID int) error {
	if APIToken == nil {
		return fmt.Errorf("api_token model not initialized")
	}
	_, err := APIToken.Delete(xdb.WhereEq("id", id), xdb.WhereEq("user_id", userID))
	return err
}

// TouchAPIToken 更新令牌的 last_used_at 为当前时间 (鉴权成功后尽力调用, 失败可忽略)。
func TouchAPIToken(id int) error {
	if APIToken == nil {
		return fmt.Errorf("api_token model not initialized")
	}
	_, err := APIToken.Update(xdb.Record{"last_used_at": time.Now()}, xdb.WhereEq("id", id))
	return err
}

// Expired 判断令牌是否已过期 (ExpiresAt 为 nil 表示永不过期)。
func (t *APITokenRecord) Expired() bool {
	return t.ExpiresAt != nil && time.Now().After(*t.ExpiresAt)
}

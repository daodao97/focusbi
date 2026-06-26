// Package auth 实现后台用户的认证 (JWT) 与授权 (RBAC 权限引擎),
// 权限模型移植自 dataddy: 角色继承树 + R 递归分段匹配 + r/rw 读写 + * 通配 + 转委校验。
package auth

import "golang.org/x/crypto/bcrypt"

// HashPassword 用 bcrypt (cost=12) 哈希密码。
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), 12)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword 校验明文与哈希是否匹配。
func CheckPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// Package auth 实现邮箱认证、服务端登录会话与租户成员权限。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/mail"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"supportdesk/backend/internal/platform"
)

const passwordMemory = 64 * 1024

// HashPassword 为受控平台初始化命令复用密码强度校验与Argon2id，禁止通过公开接口设置平台角色。
func HashPassword(password string) (string, error) { return hashPassword(password) }

// normalizeEmail 规范邮箱并拒绝显示名、控制字符及非标准地址。
func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 254 || strings.ContainsAny(value, "\r\n\x00") {
		return "", platform.Fail(400, "invalid_email", "请填写有效邮箱")
	}
	address, err := mail.ParseAddress(value)
	parts := strings.Split(value, "@")
	if err != nil || address.Address != value || len(parts) != 2 || !strings.Contains(parts[1], ".") {
		return "", platform.Fail(400, "invalid_email", "请填写有效邮箱")
	}
	return value, nil
}

// validatePassword 限制密码长度，允许空格、Unicode及密码管理器生成的密码。
func validatePassword(value string) error {
	length := utf8.RuneCountInString(value)
	if !utf8.ValidString(value) || length < 8 || length > 128 || len(value) > 512 || strings.TrimSpace(value) == "" {
		return platform.Fail(400, "invalid_password", "密码需要为8至128个字符")
	}
	return nil
}

// hashPassword 使用独立随机盐及带参数版本的Argon2id摘要保存密码。
func hashPassword(password string) (string, error) {
	if err := validatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, 3, passwordMemory, 1, 32)
	return fmt.Sprintf("$argon2id$v=19$m=65536,t=3,p=1$%s$%s", base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// verifyPassword 校验版本及资源上限后以恒定时间比较密码摘要。
func verifyPassword(encoded, password string) bool {
	if len(password) > 512 {
		return false
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	parameters := strings.Split(parts[3], ",")
	if len(parameters) != 3 {
		return false
	}
	values := make([]uint64, 3)
	for index, prefix := range []string{"m=", "t=", "p="} {
		if !strings.HasPrefix(parameters[index], prefix) {
			return false
		}
		value, err := strconv.ParseUint(strings.TrimPrefix(parameters[index], prefix), 10, 32)
		if err != nil {
			return false
		}
		values[index] = value
	}
	if values[0] < 8*1024 || values[0] > 128*1024 || values[1] < 1 || values[1] > 5 || values[2] < 1 || values[2] > 4 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) != 32 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(values[1]), uint32(values[0]), uint8(values[2]), 32)
	return subtle.ConstantTimeCompare(got, want) == 1
}

// newID 生成UUID第四版标识，避免依赖额外的UUID包。
func newID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = value[6]&0x0f | 0x40
	value[8] = value[8]&0x3f | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:], nil
}

// validID 检查外部UUID格式，避免非法类型值落入数据库错误路径。
func validID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	_, err := hex.DecodeString(strings.ReplaceAll(value, "-", ""))
	return err == nil
}

// newCode 使用安全随机数生成包含前导零的六位验证码。
func newCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

// digestToken 对高熵会话或邀请令牌计算不可逆摘要。
func digestToken(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

// keyedDigest 为验证码和限流键计算有服务端密钥保护的摘要。
func keyedDigest(secret []byte, values ...string) []byte {
	hash := hmac.New(sha256.New, secret)
	for _, value := range values {
		hash.Write([]byte(value))
		hash.Write([]byte{0})
	}
	return hash.Sum(nil)
}

// Package storage 提供 API Key 的加密与解密功能，使用 AES-256-GCM 算法保护敏感凭据。
// 注意: 当前密钥派生基于机器标识，建议后续引入用户主密码增强安全性。
package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
)

// deriveKey 从系统信息（hostname + uid）通过 SHA-256 派生 32 字节 AES-256 密钥
func deriveKey() []byte {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost" // 回退
	}
	uid := fmt.Sprintf("%d", os.Getuid())
	data := hostname + ":" + uid
	hash := sha256.Sum256([]byte(data))
	return hash[:]
}

// EncryptAPIKey 使用 AES-256-GCM 加密 API Key，返回 base64 编码的密文（含 nonce）
// 格式: base64(nonce + ciphertext)，其中 nonce 为 12 字节
func EncryptAPIKey(plaintext string) (string, error) {
	key := deriveKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize()) // 12 bytes for GCM
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("encrypt: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptAPIKey 解密 AES-256-GCM 加密的 API Key
// 如果解密失败（如密钥变更或传入的是未加密的旧数据），返回原始字符串并记录警告
func DecryptAPIKey(encoded string) (string, error) {
	key := deriveKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		log.Printf("[security] 解密 API Key 失败（创建 cipher 错误），回退到原始值: %v", err)
		return encoded, nil
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		log.Printf("[security] 解密 API Key 失败（创建 GCM 错误），回退到原始值: %v", err)
		return encoded, nil
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Printf("[security] 解密 API Key 失败（base64 解码错误，可能是未加密的旧数据），回退到原始值: %v", err)
		return encoded, nil
	}

	if len(ciphertext) < gcm.NonceSize() {
		log.Printf("[security] 解密 API Key 失败（密文太短），回退到原始值")
		return encoded, nil
	}

	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		log.Printf("[security] 解密 API Key 失败（GCM 解密错误，密钥可能已变更），回退到原始值: %v", err)
		return encoded, nil
	}

	return string(plaintext), nil
}

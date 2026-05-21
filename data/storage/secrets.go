// Package storage 提供 API Key 的加密与解密功能，使用 AES-256-GCM 算法保护敏感凭据。
// 加密密钥由机器标识（hostname+uid）与随机机器密钥（machineSecret）混合派生。
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
	"path/filepath"

	cerr "catcode/core/errors"
)

var machineSecret []byte

// InitMachineSecret 初始化或加载机器密钥。
// 密钥存储在 workspaceDir/.catcode/machine.key（权限 0600）。
// 首次运行时自动生成 32 字节随机密钥。
func InitMachineSecret(workspaceDir string) error {
	configDir := filepath.Join(workspaceDir, ".catcode")
	secretPath := filepath.Join(configDir, "machine.key")

	data, err := os.ReadFile(secretPath)
	if err == nil && len(data) >= 32 {
		machineSecret = data[:32]
		return nil
	}

	machineSecret = make([]byte, 32)
	if _, err := rand.Read(machineSecret); err != nil {
		return cerr.Wrap(err, "生成 machine.key 失败")
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return cerr.Wrap(err, "创建配置目录失败")
	}
	if err := os.WriteFile(secretPath, machineSecret, 0600); err != nil {
		return cerr.Wrap(err, "写入 machine.key 失败")
	}
	return nil
}

// deriveKey 从系统信息（hostname + uid）和机器密钥通过 SHA-256 派生 32 字节 AES-256 密钥
func deriveKey() []byte {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
	}
	uid := fmt.Sprintf("%d", os.Getuid())
	material := hostname + ":" + uid
	hash := sha256.Sum256([]byte(material))

	if len(machineSecret) > 0 {
		combined := append(hash[:], machineSecret...)
		finalHash := sha256.Sum256(combined)
		return finalHash[:]
	}
	return hash[:]
}

// deriveKeyLegacy 使用旧版密钥派生（仅 hostname+uid，无 machineSecret 混合）
// 用于解密旧版本加密的数据
func deriveKeyLegacy() []byte {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "localhost"
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
		return "", cerr.Wrap(err, "encrypt")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", cerr.Wrap(err, "encrypt")
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", cerr.Wrap(err, "encrypt")
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptAPIKey 解密 AES-256-GCM 加密的 API Key。
// 优先使用当前密钥解密，失败时尝试旧版密钥（无 machineSecret 混合），
// 以兼容旧格式数据。如果所有密钥均失败，返回原始字符串并记录警告。
func DecryptAPIKey(encoded string) (string, error) {
	plaintext, err := decryptWithKey(encoded, deriveKey)
	if err == nil {
		return plaintext, nil
	}

	if len(machineSecret) > 0 {
		plaintextLegacy, errLegacy := decryptWithKey(encoded, deriveKeyLegacy)
		if errLegacy == nil {
			log.Printf("[security] 使用旧版密钥解密成功，旧数据仍然兼容")
			return plaintextLegacy, nil
		}
	}

	log.Printf("[security] 解密 API Key 失败: %v", err)
	return "", err
}

// decryptWithKey 使用指定密钥解密 AES-256-GCM 加密的数据。
// 如果解密失败（如密钥变更或传入的是未加密的旧数据），返回空字符串并记录警告。
func decryptWithKey(encoded string, getKey func() []byte) (string, error) {
	key := getKey()
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", cerr.Wrap(err, "创建 cipher 错误")
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", cerr.Wrap(err, "创建 GCM 错误")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", cerr.Wrap(err, "base64 解码错误（可能是未加密的旧数据）")
	}

	if len(ciphertext) < gcm.NonceSize() {
		return "", cerr.Newf("密文太短")
	}

	nonce, ciphertext := ciphertext[:gcm.NonceSize()], ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", cerr.Wrap(err, "GCM 解密错误（密钥可能已变更）")
	}

	return string(plaintext), nil
}

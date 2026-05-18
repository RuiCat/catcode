# data/storage/secrets — API Key 加密模块

## 包概述

`data/storage/secrets` 提供 API Key 的对称加密保护。使用 **AES-256-GCM**（Galois/Counter Mode）认证加密算法加密敏感凭据，密钥从本机的 **hostname + uid** 组合通过 **SHA-256** 哈希派生而来。该模块仅使用 Go 标准库，无外部依赖。

### 安全模型

- **密钥派生**：`deriveKey()` 从 `os.Hostname()` 和 `os.Getuid()` 拼接字符串（格式 `hostname:uid`）计算 SHA-256 哈希，取前 32 字节作为 AES-256 密钥。同一台机器上同一用户运行的程序会派生出相同的密钥。
- **加密算法**：AES-256-GCM，nonce 为 12 字节（GCM 标准），通过 `crypto/rand` 生成随机 nonce 确保每次加密产生不同的密文。
- **密文格式**：`base64(nonce + ciphertext)` — 将 12 字节 nonce 与密文拼接后整体使用标准 base64 编码。
- **向后兼容**：`DecryptAPIKey` 在所有解密失败场景（密钥变更、base64 解码失败、GCM 认证失败等）下，均返回原始输入字符串并记录 `[security]` 前缀的警告日志，从而兼容未加密的旧数据。

### 依赖

- `crypto/aes` — AES 块加密
- `crypto/cipher` — GCM 认证加密模式
- `crypto/sha256` — SHA-256 哈希
- `encoding/base64` — 标准 base64 编解码
- `crypto/rand` — 密码学安全随机数
- `os` — 获取 hostname 和 uid
- `log` — 记录警告日志

---

## 导出函数

### `EncryptAPIKey`

```go
func EncryptAPIKey(plaintext string) (string, error)
```

使用 AES-256-GCM 加密 API Key，返回 base64 编码的密文。

**参数**：
- `plaintext` — 明文的 API Key 字符串

**返回值**：
- `string` — base64 编码的密文，格式为 `base64(nonce + ciphertext)`，nonce 为 12 字节
- `error` — 加密过程中任何步骤失败时返回的错误：
  - AES cipher 创建失败
  - GCM 实例创建失败
  - 随机 nonce 生成失败（`crypto/rand` 读取不足）

**功能**：调用 `deriveKey()` 派生 32 字节密钥，创建 AES cipher 和 GCM 实例，生成 12 字节随机 nonce，调用 `gcm.Seal` 执行加密并附加认证标签，最后将 nonce 前缀与密文整体 base64 编码返回。

**使用示例**：

```go
encrypted, err := EncryptAPIKey("sk-abc123...")
if err != nil {
    // 处理加密错误
}
// encrypted 形如: "hTq3KxZp9vLmN2...（base64 字符串）"
```

---

### `DecryptAPIKey`

```go
func DecryptAPIKey(encoded string) (string, error)
```

解密 AES-256-GCM 加密的 API Key。如果解密过程中任何步骤失败，返回原始输入字符串并记录警告日志，实现向后兼容。

**参数**：
- `encoded` — base64 编码的密文（由 `EncryptAPIKey` 生成），或未加密的原始 API Key 字符串

**返回值**：
- `string` — 解密后的明文 API Key，或解密失败时返回原始的 `encoded` 输入
- `error` — 始终为 `nil`（该函数不会返回错误，所有失败场景通过日志记录并回退到原始值）

**功能**：依次执行以下解密步骤，任一步骤失败即回退：

1. **`deriveKey()`** — 派生 32 字节 AES-256 密钥
2. **`aes.NewCipher(key)`** — 创建 AES cipher；失败时回退并记录 `"创建 cipher 错误"`
3. **`cipher.NewGCM(block)`** — 创建 GCM 实例；失败时回退并记录 `"创建 GCM 错误"`
4. **`base64.StdEncoding.DecodeString(encoded)`** — 解码 base64；失败时回退并记录 `"base64 解码错误，可能是未加密的旧数据"`
5. **长度校验** — 检查解码后数据长度 >= 12 字节（nonce 长度）；不足时回退并记录 `"密文太短"`
6. **分离 nonce 与密文** — 前 12 字节为 nonce，剩余部分为 GCM 密文（含认证标签）
7. **`gcm.Open(nil, nonce, ciphertext, nil)`** — GCM 解密并验证认证标签；失败时回退并记录 `"GCM 解密错误，密钥可能已变更"`

所有警告日志使用 `[security]` 前缀，格式为：
```
[security] 解密 API Key 失败（<原因>），回退到原始值: <错误详情>
```

**向后兼容说明**：如果 `encoded` 本身就是明文（如旧版本存储的未加密数据），base64 解码通常会失败，函数将直接返回原始值，确保旧数据无缝兼容。同理，如果密钥由于系统迁移（hostname 或 uid 变更）而发生变化，GCM 认证标签校验失败后也会安全回退。

**使用示例**：

```go
key, err := DecryptAPIKey(storedValue)
// err 始终为 nil
// key 为解密后的明文，或解密失败时的原始 storedValue
```

---

## 内部函数

### `deriveKey`

```go
func deriveKey() []byte
```

从系统信息（hostname + uid）通过 SHA-256 派生 32 字节 AES-256 密钥。

**参数**：无

**返回值**：`[]byte` — 32 字节的 AES-256 密钥（SHA-256 哈希的完整输出）

**功能**：
1. 调用 `os.Hostname()` 获取本机主机名（获取失败时回退到 `"localhost"`）
2. 调用 `os.Getuid()` 获取当前用户 ID
3. 拼接为 `"hostname:uid"` 格式的字符串
4. 计算 `sha256.Sum256()` 得到 32 字节哈希值作为密钥

**安全说明**：本函数为未导出函数（小写开头），仅供包内 `EncryptAPIKey` 和 `DecryptAPIKey` 调用。密钥基于机器标识（hostname:uid）确定性派生，同一主机同一用户总是生成相同密钥，无需额外存储或管理密钥材料。注意：hostname 获取失败时会回退到 `"localhost"`，此行为降低了密钥的唯一性，后续版本建议引入用户主密码增强安全性。

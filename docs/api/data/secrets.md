# data/storage/secrets — API Key 加密模块

## 包概述

`data/storage/secrets` 提供 API Key 的对称加密保护。使用 **AES-256-GCM**（Galois/Counter Mode）认证加密算法加密敏感凭据，密钥通过 **三重混合派生**（`deriveKey()`）生成：将 `SHA-256(hostname+":"+uid)` 的输出与 32 字节的机器密钥（`machineSecret`）拼接后再次 SHA-256 哈希。`deriveKeyLegacy()` 保留原 `SHA-256(hostname+":"+uid)` 派生方式用于向后兼容。该模块仅使用 Go 标准库，无外部依赖。

### 安全模型

- **密钥派生**：新的 `deriveKey()` 采用三重混合派生：`SHA-256(SHA-256(hostname+":"+uid) || machineSecret)`。`machineSecret` 为 32 字节的随机密钥，由 `InitMachineSecret(workspaceDir)` 在首次运行时生成并存储于 `.catcode/machine.key`（0600 权限）。同一工作区中同一用户运行的程序始终派生出相同的密钥。
- **加密算法**：AES-256-GCM，nonce 为 12 字节（GCM 标准），通过 `crypto/rand` 生成随机 nonce 确保每次加密产生不同的密文。
- **密文格式**：`base64(nonce + ciphertext)` — 将 12 字节 nonce 与密文拼接后整体使用标准 base64 编码。
- **向后兼容**：`DecryptAPIKey` 优先使用当前 `deriveKey()`（三重混合派生）尝试解密，失败后自动回退到 `deriveKeyLegacy()`（原始 SHA-256 派生）进行迁移解密；若两者均失败，返回原始输入字符串并记录 `[security]` 前缀的警告日志，从而兼容未加密的旧数据。

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

解密 AES-256-GCM 加密的 API Key。优先使用当前 `deriveKey()`（三重混合派生）尝试解密，失败后自动回退到 `deriveKeyLegacy()`（原始派生）进行迁移解密；若所有尝试均失败，返回原始输入字符串并记录警告日志，实现向后兼容。

**参数**：
- `encoded` — base64 编码的密文（由 `EncryptAPIKey` 生成），或未加密的原始 API Key 字符串

**返回值**：
- `string` — 解密后的明文 API Key，或解密失败时返回原始的 `encoded` 输入
- `error` — 始终为 `nil`（该函数不会返回错误，所有失败场景通过日志记录并回退到原始值）

**功能**：采用两阶段解密尝试：

**第一阶段：当前密钥（`deriveKey()`）**

1. **`deriveKey()`** — 使用三重混合派生获取当前 32 字节 AES-256 密钥
2. **`aes.NewCipher(key)`** — 创建 AES cipher
3. **`cipher.NewGCM(block)`** — 创建 GCM 实例
4. **`base64.StdEncoding.DecodeString(encoded)`** — 解码 base64；失败时进入第二阶段（也可能是未加密的旧数据）
5. **长度校验** — 检查解码后数据长度 >= 12 字节（nonce 长度）
6. **分离 nonce 与密文** — 前 12 字节为 nonce，剩余部分为 GCM 密文
7. **`gcm.Open(nil, nonce, ciphertext, nil)`** — GCM 解密并验证认证标签

**第二阶段：遗留密钥回退（`deriveKeyLegacy()`）**

若第一阶段任一步骤失败：
1. 使用 `deriveKeyLegacy()`（原始 SHA-256 派生）重新派生密钥
2. 重复步骤 2-7 尝试解密
3. 若成功：使用 `EncryptAPIKey` 将解密后的明文重新用当前密钥加密写入 DB（无缝迁移）
4. 若仍失败：返回原始 `encoded` 输入并记录 `[security]` 警告日志

所有警告日志使用 `[security]` 前缀，格式为：
```
[security] 解密 API Key 失败（<原因>），回退到原始值: <错误详情>
```

**向后兼容说明**：如果 `encoded` 本身就是明文（如旧版本存储的未加密数据），base64 解码通常在第二阶段也会失败，函数将直接返回原始值，确保旧数据无缝兼容。通过两阶段回退机制，旧版本使用 `deriveKeyLegacy()` 加密的数据可被自动解密并通过 `EncryptAPIKey` 迁移到新格式。

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

从系统标识（hostname + uid）和机器密钥通过双重 SHA-256 派生 32 字节 AES-256 密钥。

**参数**：无

**返回值**：`[]byte` — 32 字节的 AES-256 密钥

**功能**：
1. 调用 `InitMachineSecret()` 获取或生成 32 字节随机机器密钥 `machineSecret`
2. 计算 `hostKey = SHA-256(hostname + ":" + uid)`
3. 拼接 `hostKey || machineSecret` 后再次计算 `SHA-256` 得到最终 32 字节密钥

**安全说明**：三重混合派生使密钥同时依赖主机标识和随机机器密钥。即使 hostname:uid 被猜测，没有 `machine.key` 文件中的随机密钥也无法派生正确密钥。

---

### `deriveKeyLegacy`

```go
func deriveKeyLegacy() []byte
```

原始密钥派生函数，仅使用 `SHA-256(hostname + ":" + uid)` 派生 32 字节密钥，无机器密钥混合。保留此函数用于向后兼容旧数据。

**参数**：无

**返回值**：`[]byte` — 32 字节的 AES-256 密钥

---

### `InitMachineSecret`

```go
func InitMachineSecret(workspaceDir string) error
```

在首次运行时使用 `crypto/rand` 生成 32 字节随机密钥，写入 `<workspaceDir>/.catcode/machine.key`（权限 0600）。后续调用直接读取已有文件。

**参数**：
- `workspaceDir` — 工作区根目录（`machine.key` 的父目录）

**返回值**：`error` — 文件读取/生成/权限设置失败时返回错误

---

### `DecryptAPIKey`（迁移逻辑）

`DecryptAPIKey` 的解密流程已增强为两阶段尝试：

1. **优先尝试当前密钥**：使用 `deriveKey()`（三重混合派生）解密
2. **回退到遗留密钥**：若当前密钥解密失败，使用 `deriveKeyLegacy()`（原始 SHA-256 派生）尝试解密，成功后使用 `EncryptAPIKey` 自动将数据重新加密为当前格式（无缝迁移）
3. **最终回退**：若两种派生均失败，返回原始输入字符串并记录 `[security]` 警告日志

---

## 机器密钥文件（machine.key）

### 位置

```
<workspace>/.catcode/machine.key
```

### 属性

| 属性 | 值 |
|------|-----|
| 权限 | `0600`（仅拥有者可读写） |
| 大小 | 32 字节（原始随机数据，非 hex 编码） |
| 生成方式 | `crypto/rand.Read()` — 密码学安全随机数 |
| 生成时机 | 首次运行时自动生成，后续调用直接读取 |

### 丢失后果

若 `machine.key` 文件被删除或损坏，所有已加密的 API Key 将无法解密。解决方案：
- 迁移场景：恢复 `machine.key` 备份
- 新建场景：删除 `.catcode/machine.key` 后重启，系统将重新生成新密钥，但所有已加密的 API Key 需重新手动输入

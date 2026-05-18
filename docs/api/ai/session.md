# ai/session — LLM 会话管理

## 包概述

`session` 包实现 LLM 对话会话的完整生命周期管理。其核心设计借鉴 catai 项目，围绕两大性能优化展开：

1. **消息预编码 JSON 缓存**：每条消息在添加时即预编码为 JSON 字节切片（`cachedJSON`），后续请求构建时直接引用现成数据，避免反复序列化。
2. **`buffer.Buffer` 零拷贝拼接**：`BuildRequestReader()` 方法使用 `buffer.Buffer` 将预编码的消息片段和工具定义通过 `[]*[]byte` 指针切片串联，以闭包形式生成 `io.Reader`，全程零内存拷贝。

包内定义了核心接口 `SessionInterface` 及其默认实现 `Session`，以及 `Message`、`FileBlock` 等辅助类型。
### 文件结构

包实现已按功能拆分为 4 个源文件，遵循接口隔离原则：

| 文件 | 说明 |
|------|------|
| `session.go` | 核心类型定义（`Session`、`Message`、`FileBlock`）、6 个子接口（`MessageAccessor`/`ToolAccessor`/`RequestBuilder`/`SessionSerializer`/`SessionConfig`/`SessionStats`）、组合接口 `SessionInterface`、构造函数（`New`、`FromConversationRow`） |
| `session_message.go` | 消息管理（`AddMessage`、`AddToolResult`、`AddAssistantWithTools`）、完整性验证与清理（`CleanOrphanedToolCalls`、`ValidateMessages`）、推理管理（`ClearReasoning`）、上下文管理（`Clear`、`TrimMessages`、`LastMessage`） |
| `session_request.go` | 工具管理（`AddTool`、`GetTool`、`ToolNames`、`ToolCount`）、请求构建（`BuildRequest`、`BuildCleanRequest`、`BuildRequestReader`）、辅助方法（`TokenCount`、`MessageCount`、`extractKeyInfo`）、文件块管理（`UpsertFileBlock`） |
| `session_serialize.go` | 上下文压缩（`NeedsCompression`、`SetSummary`、`SetLastReasoning`）、DB 持久化（`ToConversationRow`、`ToMessageRows`）、字段访问器（17 个 Get/Set 方法）、锁控制（`LockMessages`/`UnlockMessages`） |


## 类型

### `MessageAccessor`

```go
type MessageAccessor interface {
    AddMessage(role, content string) error
    AddToolResult(toolCallID, name, content string) error
    AddAssistantWithTools(content string, toolCalls []llm.ToolCall) error
    CleanOrphanedToolCalls()
    ValidateMessages() string
    Clear()
    ClearReasoning()
}
```

**功能描述**：消息读写子接口，包含消息添加（3 个方法）、完整性验证与清理（2 个方法）、上下文重置（`Clear`）、推理管理（`ClearReasoning`）。

---

### `ToolAccessor`

```go
type ToolAccessor interface {
    AddTool(t *tool.Tool) error
    GetTool(name string) (*tool.Tool, bool)
    ToolCount() int
}
```

**功能描述**：工具注册子接口，管理会话中注册的工具及其查询与统计。

---

### `RequestBuilder`

```go
type RequestBuilder interface {
    BuildRequest() (*llm.ChatRequest, error)
    BuildCleanRequest(question string, maxTokens int) *llm.ChatRequest
}
```

**功能描述**：请求构建子接口，负责将会话状态组装为 LLM API 请求。

---

### `SessionSerializer`

```go
type SessionSerializer interface {
    ToConversationRow() *storage.ConversationRow
    ToMessageRows() []*storage.MessageRow
}
```

**功能描述**：持久化序列化子接口，将会话转换为 DB 行记录。

---

### `SessionConfig`

```go
type SessionConfig interface {
    GetID() string
    GetModel() string
    GetSystemPrompt() string
    SetSystemPrompt(p string)
    GetTemperature() float64
    SetTemperature(t float64)
    GetMaxTokens() int
    SetMaxTokens(m int)
    GetMemoryIndex() string
    SetMemoryIndex(idx string)
    GetSummary() string
    SetSummary(summary string)
    SetLastReasoning(content string)
    GetMaxToolResultLen() int
    SetMaxToolResultLen(l int)
    GetCompressThreshold() int
    SetCompressThreshold(t int)
    GetInstructionsContent() string
    SetInstructionsContent(c string)
    LockMessages()
    UnlockMessages()
}
```

**功能描述**：配置访问器子接口，包含 17 个字段 Get/Set 方法、2 个锁控制方法、以及 `SetSummary`、`SetLastReasoning`。

---

### `SessionStats`

```go
type SessionStats interface {
    TokenCount() int
    MessageCount() int
}
```

**功能描述**：统计子接口，提供 token 与消息计数。

---

### `SessionInterface`

```go
type SessionInterface interface {
    MessageAccessor
    ToolAccessor
    RequestBuilder
    SessionSerializer
    SessionConfig
    SessionStats
}
```

**功能描述**：`SessionInterface` 是 6 个子接口的组合接口，遵循接口隔离原则保持向后兼容。嵌入 `MessageAccessor`、`ToolAccessor`、`RequestBuilder`、`SessionSerializer`、`SessionConfig`、`SessionStats`，共涵盖 31 个方法。暴露的单个方法中，`SetSummary`、`SetLastReasoning` 通过 `SessionConfig` 提供；`BuildRequestReader`、`NeedsCompression`、`ToolNames`、`LastMessage`、`TrimMessages`、`UpsertFileBlock` 仅 `*Session` 提供，不属于任何接口。上层代码可面向 `SessionInterface`（完整兼容）或单个子接口（最小依赖原则）编程，便于替换实现（如 mock 测试）。

---

### `Session`

```go
type Session struct {
    ID                string              // 会话唯一标识
    Model             string              // 使用的模型
    SystemPrompt      string              // 系统提示词
    Temperature       float64             // 默认请求 temperature
    MaxTokens         int                 // 默认请求 max_tokens
    Messages          []*Message          // 对话历史
    Tools             []*tool.Tool         // 已注册工具
    ToolsMap          map[string]int      // 工具名 → Tools 切片索引
    CreatedAt         time.Time
    UpdatedAt         time.Time
    Metadata          map[string]string   // 元数据

    // 上下文压缩相关
    CompressThreshold   int                  // token 压缩阈值（默认 900000）
    Summary             string               // 压缩摘要
    MemoryIndex         string               // 记忆索引（global + workspace）
    InstructionsContent string               // 指令文件内容
    MaxToolResultLen    int                  // 工具结果传给 LLM 的截断长度（默认 4000）
    FileBlocks          map[string]*FileBlock // 文件路径 → 文件块（替换式读取）

    // 预编码 buffer（零拷贝关键）
    msgBuf  buffer.Buffer  // 消息拼接 buffer
    toolBuf buffer.Buffer  // 工具拼接 buffer

    mu sync.RWMutex
}
```

**功能描述**：`Session` 是 `SessionInterface` 的默认实现，包含对话历史、工具注册表、上下文压缩配置及优化用缓冲区。所有公开方法均通过 `sync.RWMutex` 保证线程安全。

---

### `Message`

```go
type Message struct {
    Role       string `json:"role"`
    Content    string `json:"content"`
    Name       string `json:"name,omitempty"`
    ToolCallID string `json:"tool_call_id"` // 必须字段，空值不应被省略

    // thinking 模式：推理内容必须回传给 API
    ReasoningContent string `json:"reasoning_content,omitempty"`

    // 预编码 JSON 缓存（零拷贝关键）
    cachedJSON []byte        `json:"-"`
    Enable     bool          `json:"-"` // 是否在上下文中启用
    ToolCalls  []llm.ToolCall `json:"tool_calls,omitempty"`
}
```

**功能描述**：`Message` 表示对话历史中的一条消息。字段设计要点：

- `ToolCallID` 不使用 `omitempty`，即使为空也序列化为 `"tool_call_id":""`，便于暴露调用方遗漏该字段的 bug。
- `cachedJSON` 为预编码的完整 JSON 字节切片，`Update()` 方法生成，`CachedJSON()` 方法读取。标记为 `json:"-"` 不参与序列化。
- `Enable` 控制该消息是否在构建请求时被包含。当 `CleanOrphanedToolCalls()` 发现孤立的 tool_calls 时，会将对应消息标记为 `Enable = false`（非破坏性禁用，而非截断数组）。

---

### `FileBlock`

```go
type FileBlock struct {
    Path        string
    Offset      int
    EndLine     int
    Content     string
    TotalLines  int
    TotalBytes  int
    PrevSummary string
    MsgIndex    int
}
```

**功能描述**：`FileBlock` 记录一次文件读取操作的结果元信息，用于替换式读取场景：当同一文件被再次读取时，`UpsertFileBlock()` 会替换原有消息内容，避免历史中堆积重复的文件块。

---

## 构造函数

### `New`

```go
func New(id, model, systemPrompt string) *Session
```

| 参数 | 说明 |
|------|------|
| `id` | 会话唯一标识（通常为 UUID） |
| `model` | 使用的 LLM 模型名称 |
| `systemPrompt` | 系统提示词内容 |

**返回值**：初始化完毕的 `*Session`，包含默认配置：
- `CompressThreshold`: 900000（1M 上下文下约 90 万 token 触发压缩）
- `MaxToolResultLen`: 4000（工具结果传给 LLM 的截断长度）
- `Messages`、`Tools`、`ToolsMap`、`Metadata`、`FileBlocks` 均已初始化为空集合
- `msgBuf`、`toolBuf` 已创建 `buffer.Buffer` 实例

---

### `FromConversationRow`

```go
func FromConversationRow(conv *storage.ConversationRow, msgs []*storage.MessageRow) *Session
```

| 参数 | 说明 |
|------|------|
| `conv` | 数据库中读取的 conversation 行记录 |
| `msgs` | 数据库中读取的消息行记录列表 |

**返回值**：从 DB 记录重建的 `*Session`。

**功能描述**：先通过 `New()` 创建基础 Session，再遍历 `msgs` 逐条重建 `Message`（含预编码），最后调用 `CleanOrphanedToolCalls()` 清理可能遗留的不完整 tool_calls。这是从持久化存储恢复会话的唯一入口。

---

## Message 方法

### `(*Message).Update`

```go
func (m *Message) Update() error
```

**功能描述**：将当前消息序列化为 JSON 并缓存到 `m.cachedJSON`。使用原地缓冲区策略（复用 `cachedJSON` 的底层数组），调用 `json.Encoder` 且关闭 HTML 转义。尾部自动去除 `\n`（`json.Encoder` 默认追加）。此方法在添加消息、替换工具结果、重建会话时均被调用。

**返回值**：编码出错时返回错误；成功返回 `nil`。

---

### `(*Message).CachedJSON`

```go
func (m *Message) CachedJSON() []byte
```

**功能描述**：返回预编码的 JSON 字节切片。若尚未调用 `Update()` 则为 `nil`。`BuildRequestReader()` 通过 `buffer.Buffer.AddPtr()` 直接引用此切片，实现零拷贝。

---

## 消息管理方法

### `AddMessage`

```go
func (s *Session) AddMessage(role, content string) error
```

| 参数 | 说明 |
|------|------|
| `role` | 角色：`"user"`、`"assistant"`、`"system"` |
| `content` | 消息正文 |

**功能描述**：向对话历史追加一条纯文本消息，自动预编码并更新 `UpdatedAt`。

---

### `AddToolResult`

```go
func (s *Session) AddToolResult(toolCallID, name, content string) error
```

| 参数 | 说明 |
|------|------|
| `toolCallID` | 工具调用 ID（**不能为空**，否则返回错误） |
| `name` | 工具名称 |
| `content` | 工具执行结果文本 |

**功能描述**：添加工具执行结果消息。具有**幂等性**：若已存在相同 `toolCallID` 的 `role="tool"` 消息，则原地替换其 `Content` 和 `Name`（保留最新的结果），而非重复追加。

**防御**：`toolCallID` 为空时立即返回错误，及早暴露调用方 bug。

---

### `AddAssistantWithTools`

```go
func (s *Session) AddAssistantWithTools(content string, toolCalls []llm.ToolCall) error
```

| 参数 | 说明 |
|------|------|
| `content` | assistant 的文本回复（可为空字符串） |
| `toolCalls` | 工具调用列表（若为空则等同于 `AddMessage`） |

**功能描述**：添加包含工具调用的 assistant 消息。若 `toolCalls` 非空，直接使用 `json.Marshal` 构建 `cachedJSON`（需要额外字段映射，不走标准 `Update()` 路径）；若为空则退化为 `Update()` 编码。

---

## 工具管理方法

### `AddTool`

```go
func (s *Session) AddTool(t *tool.Tool) error
```

| 参数 | 说明 |
|------|------|
| `t` | 要注册的工具对象 |

**功能描述**：将会话中注册工具，设置 `t.Enable = true`，调用 `t.Update()` 预编码。若工具名已存在则返回错误。

---

### `GetTool`

```go
func (s *Session) GetTool(name string) (*tool.Tool, bool)
```

| 参数 | 说明 |
|------|------|
| `name` | 工具名称 |

**返回值**：
- `*tool.Tool`：找到的工具指针；未找到时为 `nil`
- `bool`：是否找到

---

### `ToolNames`

```go
func (s *Session) ToolNames() []string
```

**返回值**：已注册工具的名称列表（按注册顺序）。

> 注意：此方法不属于 `SessionInterface`，仅 `*Session` 可用。

---

## 请求构建方法

### 上下文分层机制

`BuildRequest()` 构建的消息列表采用五层结构（均为 `system` 角色注入的上下文），从高到低优先级为：

1. **SystemPrompt** — 系统提示词（必选，定义智能体角色与行为）
2. **MemoryIndex** — 记忆索引（可选，包含全局记忆 + 工作区记忆，注入为独立 system 消息）
3. **InstructionsContent** — 指令文件内容（可选，项目级 / 工作区级指令）
4. **Summary** — 上下文压缩摘要（可选，以 `"[上下文索引]\n"` 前缀注入）
5. **对话上下文** — 最近的消息历史 + 工具调用历史（`role` 为 user/assistant/tool 的消息）

---

### `BuildRequest`

```go
func (s *Session) BuildRequest() (*llm.ChatRequest, error)
```

**返回值**：完整的 `*llm.ChatRequest`，包含模型名、分层消息列表、启用的工具定义、`Stream: true`、及 temperature / max_tokens（若值大于 0）。

**功能描述**：
- 遍历 `Messages` 并跳过 `Enable = false` 的消息
- 对 `role="tool"` 的消息，若内容超过 `MaxToolResultLen` 则截断（DB 层仍保留完整数据）
- 仅包含 `Enable = true` 的工具

---

### `BuildCleanRequest`

```go
func (s *Session) BuildCleanRequest(question string, maxTokens int) *llm.ChatRequest
```

| 参数 | 说明 |
|------|------|
| `question` | 用户问题正文（构造为最后一条 user 消息） |
| `maxTokens` | 最大生成 token 数（若为 0 则回退到 `s.MaxTokens`） |

**功能描述**：构建一个不受工具调用链污染的"干净"请求。跳过所有包含 `tool_calls` 的 assistant 消息和所有 `tool` 角色消息，仅提取最近 6 条正常消息作为上下文。适用于子智能体双向通信等需要独立上下文但不依赖完整会话状态的场景。

**消息结构**：`SystemPrompt → MemoryIndex → 最近 6 条正常消息 → 用户问题`。`Stream` 设为 `false`。

---

### `BuildRequestReader`

```go
func (s *Session) BuildRequestReader() io.Reader
```

**功能描述**：使用 `buffer.Buffer` 零拷贝拼接构建完整请求体，返回 `io.Reader`。直接操作 `msgBuf`，通过 `AddBytes` 添加 JSON 结构片段，通过 `AddPtr` 引用消息和工具的预编码 `cachedJSON`，避免任何内存复制和字符串拼接。是性能关键路径。

> 注意：此方法不属于 `SessionInterface`，仅 `*Session` 可用。

---

## 完整性验证与清理

### `CleanOrphanedToolCalls`

```go
func (s *Session) CleanOrphanedToolCalls()
```

**功能描述**：两轮扫描，防止将不完整的 tool_calls 发送给 LLM（会导致 API 400 错误）：

**第一轮**：从后向前遍历所有包含 `tool_calls` 的 assistant 消息，对每个 `tool_call.id` 检查其后是否存在对应的 `role="tool"` 且 `Enable=true` 的消息。若存在未闭合的 tool_call：
- 将该 assistant 消息的 `Enable` 设为 `false`（非破坏性，不删除）
- 将该 assistant 对应的所有 tool 结果也设为 `Enable=false`
- 继续检查其他 assistant（不做截断）

**第二轮**：从后向前去重 tool 结果。对每个 `tool_call_id`，仅保留最后一次出现的 `role="tool"` 消息为启用，更早的同 ID 结果均设为 `Enable=false`。

**设计要点**：使用 `Enable` 标记而非数组截断，避免因移除中间元素而牵连下游有效消息。

---

### `ValidateMessages`

```go
func (s *Session) ValidateMessages() string
```

**返回值**：发现问题时返回描述字符串（第一个发现的问题）；无问题时返回空字符串 `""`。

**检查项**：
1. 所有 `role="tool"` 且 `Enable=true` 的消息必须有非空 `tool_call_id`
2. 所有启用且包含 `tool_calls` 的 assistant 消息，其每个 `tool_call.id` 都必须有对应的启用 tool 结果

---

## 上下文与统计

### `TokenCount`

```go
func (s *Session) TokenCount() int
```

**返回值**：当前会话的估算 token 数（`SystemPrompt` + 所有 `Messages` 的 `Content`），使用 `llm.EstimateTokens()` 估算。

---

### `NeedsCompression`

```go
func (s *Session) NeedsCompression() bool
```

**返回值**：`TokenCount() > CompressThreshold` 时返回 `true`。

> 注意：此方法不属于 `SessionInterface`，仅 `*Session` 可用。

---

### `MessageCount`

```go
func (s *Session) MessageCount() int
```

**返回值**：消息总数（含 `Enable=false` 的消息）。

---

### `ToolCount`

```go
func (s *Session) ToolCount() int
```

**返回值**：已注册工具总数（含 `Enable=false` 的工具）。

---

## 摘要与推理

### `SetSummary`

```go
func (s *Session) SetSummary(summary string)
```

**功能描述**：设置上下文压缩摘要，并更新 `UpdatedAt`。摘要会在 `BuildRequest()` 中以 `"[上下文索引]\n"` 前缀注入为 system 消息。

---

### `SetLastReasoning`

```go
func (s *Session) SetLastReasoning(content string)
```

**功能描述**：设置最后一条消息的 `ReasoningContent`（thinking 模式下的推理链内容）。该字段必须回传给 API 以维持 thinking 模式的连续性。

---

### `ClearReasoning`

```go
func (s *Session) ClearReasoning()
```

**功能描述**：清空所有消息的 `ReasoningContent` 字段。用于切换模式或重新开始推理时。

---

## 重置与会话管理

### `Clear`

```go
func (s *Session) Clear()
```

**功能描述**：完整清理 `Messages`、`MemoryIndex`、`Summary`，保留 `ID`、`Model`、`SystemPrompt`、工具注册等会话元信息。清空操作线程安全。

---

### `TrimMessages`

```go
func (s *Session) TrimMessages(keep int)
```

| 参数 | 说明 |
|------|------|
| `keep` | 保留的消息条数（从末尾计数） |

**功能描述**：保留最近 `keep` 条消息，丢弃更早的。若 `len(Messages) <= keep` 则无操作。

> 注意：此方法不属于 `SessionInterface`，仅 `*Session` 可用。

---

### `LastMessage`

```go
func (s *Session) LastMessage() *Message
```

**返回值**：最后一条消息的指针；若无消息则返回 `nil`。

> 注意：此方法不属于 `SessionInterface`，仅 `*Session` 可用。

---

## 文件块管理

### `UpsertFileBlock`

```go
func (s *Session) UpsertFileBlock(toolCallID, path, content string, offset, endLine, totalLines, totalBytes int) bool
```

| 参数 | 说明 |
|------|------|
| `toolCallID` | 工具调用 ID |
| `path` | 文件路径 |
| `content` | 文件内容 |
| `offset` | 起始行号 |
| `endLine` | 结束行号 |
| `totalLines` | 文件总行数 |
| `totalBytes` | 文件总字节数 |

**返回值**：`true` 表示替换了已有的文件块（在原有 tool 消息上原地替换）；`false` 表示新增了 tool 消息。

**功能描述**：
- 若 `FileBlocks` 中已存在相同 `path` 的块，则提取旧块的 `extractKeyInfo()` 摘要，将新内容格式化为 `"[上一块 ... 摘要]\n[当前块 ...]\n{内容}"`，原地替换对应 `Messages[fb.MsgIndex].Content`，并更新 `FileBlock` 元信息。
- 若不存在，则新增一条 `role="tool"`、`name="read"` 的 tool 消息，格式为 `"[文件: {path}, 行{offset}-{endLine}/{totalLines}, {totalBytes}字节]\n{content}"`，并注册到 `FileBlocks`。

> 注意：此方法不属于 `SessionInterface`，仅 `*Session` 可用。

---

## DB 持久化

### `ToConversationRow`

```go
func (s *Session) ToConversationRow() *storage.ConversationRow
```

**返回值**：将 Session 的核心字段映射为 `storage.ConversationRow`，`MetadataJSON` 由 `Metadata` map 序列化生成，`TokenCount` 实时计算。

---

### `ToMessageRows`

```go
func (s *Session) ToMessageRows() []*storage.MessageRow
```

**返回值**：遍历 `Messages`，仅导出 `Enable=true` 的消息为 `storage.MessageRow` 列表。每条消息的 `ToolCalls` 序列化为 `ToolCallsJSON`，`Seq` 为原始索引。

---

## 字段访问器

以下方法构成 `SessionInterface` 的字段 getter/setter 集合，均为线程安全的直接读写（不含额外逻辑）。

| 方法 | 签名 | 说明 |
|------|------|------|
| `GetID` | `func (s *Session) GetID() string` | 返回会话 ID |
| `GetModel` | `func (s *Session) GetModel() string` | 返回模型名称 |
| `GetSystemPrompt` | `func (s *Session) GetSystemPrompt() string` | 返回系统提示词 |
| `SetSystemPrompt` | `func (s *Session) SetSystemPrompt(p string)` | 设置系统提示词 |
| `GetTemperature` | `func (s *Session) GetTemperature() float64` | 返回默认 temperature |
| `SetTemperature` | `func (s *Session) SetTemperature(t float64)` | 设置默认 temperature |
| `GetMaxTokens` | `func (s *Session) GetMaxTokens() int` | 返回默认 max_tokens |
| `SetMaxTokens` | `func (s *Session) SetMaxTokens(m int)` | 设置默认 max_tokens |
| `GetMemoryIndex` | `func (s *Session) GetMemoryIndex() string` | 返回记忆索引 |
| `SetMemoryIndex` | `func (s *Session) SetMemoryIndex(idx string)` | 设置记忆索引 |
| `GetSummary` | `func (s *Session) GetSummary() string` | 返回压缩摘要 |
| `GetMaxToolResultLen` | `func (s *Session) GetMaxToolResultLen() int` | 返回工具结果截断长度 |
| `SetMaxToolResultLen` | `func (s *Session) SetMaxToolResultLen(l int)` | 设置工具结果截断长度 |
| `GetCompressThreshold` | `func (s *Session) GetCompressThreshold() int` | 返回压缩触发阈值 |
| `SetCompressThreshold` | `func (s *Session) SetCompressThreshold(t int)` | 设置压缩触发阈值 |
| `GetInstructionsContent` | `func (s *Session) GetInstructionsContent() string` | 返回指令文件内容 |
| `SetInstructionsContent` | `func (s *Session) SetInstructionsContent(c string)` | 设置指令文件内容 |

---

## 锁控制

### `LockMessages` / `UnlockMessages`

```go
func (s *Session) LockMessages()
func (s *Session) UnlockMessages()
```

**功能描述**：对外暴露内部 `sync.RWMutex`，允许调用方在批量操作时手动锁定以提升性能（避免多次加锁/解锁开销）。典型用法：

```go
sess.LockMessages()
sess.AddMessage("user", "问题1")
sess.AddMessage("assistant", "回答1")
sess.UnlockMessages()
```

> **警告**：调用方必须确保 `LockMessages()` 与 `UnlockMessages()` 成对调用，且锁定期间不能调用 Session 内部已加锁的方法（会死锁）。

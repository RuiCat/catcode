# mcp — MCP 协议实现

## 包概述

`mcp` 包实现了 [MCP (Model Context Protocol)](https://modelcontextprotocol.io) 客户端，遵循 JSON-RPC 2.0 规范。该包提供：

- **Transport 接口**：抽象传输层，定义 `Send`/`Receive`/`Close` 三个方法
- **StdioTransport**：通过子进程标准输入/输出通信的传输实现
- **HTTPTransport**：通过 HTTP + SSE（Server-Sent Events）通信的传输实现
- **Client**：MCP 协议客户端，封装初始化握手、工具列表查询、工具调用等功能
- **工具适配**：`AdaptTool` 函数将 MCP 工具定义转换为 catcode 的 `tool.Tool`
- **Manager**：多服务器连接管理器，支持同时连接多个 MCP 服务器并聚合工具

---

## JSON-RPC 消息类型

### JSONRPCMessage

JSON-RPC 2.0 消息基类，同时用于请求和响应。

```go
type JSONRPCMessage struct {
    JSONRPC string          `json:"jsonrpc"`
    ID      int64           `json:"id,omitempty"`
    Method  string          `json:"method,omitempty"`
    Params  json.RawMessage `json:"params,omitempty"`
    Result  json.RawMessage `json:"result,omitempty"`
    Error   *JSONRPCError   `json:"error,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `JSONRPC` | `string` | 固定值 `"2.0"` |
| `ID` | `int64` | 请求标识符，响应回显对应的请求 ID。通知类型没有 ID |
| `Method` | `string` | 请求的方法名（请求消息使用） |
| `Params` | `json.RawMessage` | 方法参数（请求消息使用） |
| `Result` | `json.RawMessage` | 方法返回结果（响应消息使用） |
| `Error` | `*JSONRPCError` | 错误信息（响应消息使用，成功时为 nil） |

### JSONRPCError

JSON-RPC 错误信息。

```go
type JSONRPCError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Code` | `int` | 错误码（参见 [错误码常量](#错误码常量)） |
| `Message` | `string` | 错误描述 |

---

## MCP 协议类型

### InitializeParams

初始化请求参数，由客户端发送给服务器。

```go
type InitializeParams struct {
    ProtocolVersion string             `json:"protocolVersion"`
    Capabilities    ClientCapabilities `json:"capabilities"`
    ClientInfo      Implementation     `json:"clientInfo"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ProtocolVersion` | `string` | 协议版本，固定 `"2024-11-05"` |
| `Capabilities` | `ClientCapabilities` | 客户端能力声明 |
| `ClientInfo` | `Implementation` | 客户端实现信息 |

### ClientCapabilities

客户端能力声明。

```go
type ClientCapabilities struct {
    Roots    *RootsCapability    `json:"roots,omitempty"`
    Sampling *SamplingCapability `json:"sampling,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Roots` | `*RootsCapability` | 根目录能力（支持文件系统根列表变更通知） |
| `Sampling` | `*SamplingCapability` | 采样能力（LLM 采样请求） |

### RootsCapability

根目录能力。

```go
type RootsCapability struct {
    ListChanged bool `json:"listChanged,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ListChanged` | `bool` | 是否支持根列表变更通知 |

### SamplingCapability

采样能力（占位结构体，无字段）。

```go
type SamplingCapability struct{}
```

### Implementation

客户端或服务器的实现信息。

```go
type Implementation struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 实现名称 |
| `Version` | `string` | 实现版本 |

### ServerInfo

服务器信息（初始化响应中返回）。

```go
type ServerInfo struct {
    Name    string `json:"name"`
    Version string `json:"version"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 服务器名称 |
| `Version` | `string` | 服务器版本 |

### InitializeResult

初始化响应结果。

```go
type InitializeResult struct {
    ProtocolVersion string             `json:"protocolVersion"`
    Capabilities    ServerCapabilities `json:"capabilities"`
    ServerInfo      ServerInfo         `json:"serverInfo"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ProtocolVersion` | `string` | 协商后的协议版本 |
| `Capabilities` | `ServerCapabilities` | 服务器能力声明 |
| `ServerInfo` | `ServerInfo` | 服务器信息 |

### ServerCapabilities

服务器能力声明。

```go
type ServerCapabilities struct {
    Tools     *ToolsCapability     `json:"tools,omitempty"`
    Resources *ResourcesCapability `json:"resources,omitempty"`
    Prompts   *PromptsCapability   `json:"prompts,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Tools` | `*ToolsCapability` | 工具能力（非 nil 表示服务器提供工具） |
| `Resources` | `*ResourcesCapability` | 资源能力（非 nil 表示服务器提供资源） |
| `Prompts` | `*PromptsCapability` | 提示词能力（非 nil 表示服务器提供提示词模板） |

### ToolsCapability

工具能力声明。

```go
type ToolsCapability struct {
    ListChanged bool `json:"listChanged,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ListChanged` | `bool` | 是否支持工具列表变更通知 |

### ResourcesCapability

资源能力声明。

```go
type ResourcesCapability struct {
    Subscribe   bool `json:"subscribe,omitempty"`
    ListChanged bool `json:"listChanged,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Subscribe` | `bool` | 是否支持资源订阅 |
| `ListChanged` | `bool` | 是否支持资源列表变更通知 |

### PromptsCapability

提示词能力声明。

```go
type PromptsCapability struct {
    ListChanged bool `json:"listChanged,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ListChanged` | `bool` | 是否支持提示词列表变更通知 |

---

## 工具类型

### ToolDef

MCP 工具定义（由服务器在 `tools/list` 响应中返回）。

```go
type ToolDef struct {
    Name        string          `json:"name"`
    Description string          `json:"description,omitempty"`
    InputSchema json.RawMessage `json:"inputSchema"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 工具名称 |
| `Description` | `string` | 工具描述文本 |
| `InputSchema` | `json.RawMessage` | 输入参数的 JSON Schema 定义 |

### ListToolsResult

工具列表响应结果。

```go
type ListToolsResult struct {
    Tools []ToolDef `json:"tools"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Tools` | `[]ToolDef` | 服务器提供的所有工具定义 |

### CallToolParams

工具调用请求参数。

```go
type CallToolParams struct {
    Name      string         `json:"name"`
    Arguments map[string]any `json:"arguments,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 要调用的工具名称 |
| `Arguments` | `map[string]any` | 传入工具的参数键值对 |

### CallToolResult

工具调用响应结果。

```go
type CallToolResult struct {
    Content []ContentItem `json:"content"`
    IsError bool          `json:"isError,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Content` | `[]ContentItem` | 工具返回的内容列表（可包含多个内容项） |
| `IsError` | `bool` | 标记此次调用是否出错 |

### ContentItem

工具结果中的单个内容项。

```go
type ContentItem struct {
    Type     string `json:"type"`
    Text     string `json:"text,omitempty"`
    Data     string `json:"data,omitempty"`
    MimeType string `json:"mimeType,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Type` | `string` | 内容类型：`"text"`、`"image"` 或 `"resource"` |
| `Text` | `string` | 文本内容（Type 为 `"text"` 时使用） |
| `Data` | `string` | 二进制数据（Base64 编码，Type 为 `"image"` 或 `"resource"` 时使用） |
| `MimeType` | `string` | 数据的 MIME 类型 |

---

## 通知类型

### Notification

服务器推送给客户端的通知消息。

```go
type Notification struct {
    Method string          `json:"method"`
    Params json.RawMessage `json:"params,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Method` | `string` | 通知方法名（如 `"notifications/tools/list_changed"`） |
| `Params` | `json.RawMessage` | 通知携带的参数 |

---

## 错误码常量

遵循 JSON-RPC 2.0 标准错误码定义。

```go
const (
    ErrParse          = -32700  // 解析错误
    ErrInvalidReq     = -32600  // 无效请求
    ErrMethodNotFound = -32601  // 方法未找到
    ErrInvalidParams  = -32602  // 无效参数
    ErrInternal       = -32603  // 内部错误
)
```

| 常量 | 值 | 说明 |
|------|------|------|
| `ErrParse` | `-32700` | 解析 JSON 失败 |
| `ErrInvalidReq` | `-32600` | 请求对象不是有效的 JSON-RPC 请求 |
| `ErrMethodNotFound` | `-32601` | 请求的方法不存在 |
| `ErrInvalidParams` | `-32602` | 方法参数无效 |
| `ErrInternal` | `-32603` | 服务器内部错误 |

---

## 传输层接口

### Transport

MCP 传输层抽象接口，定义通信的三个基本操作。

```go
type Transport interface {
    Send(msg *JSONRPCMessage) error
    Receive() (*JSONRPCMessage, error)
    Close() error
}
```

#### Send(msg *JSONRPCMessage) error

发送一条 JSON-RPC 消息到对端。

| 参数 | 类型 | 说明 |
|------|------|------|
| `msg` | `*JSONRPCMessage` | 要发送的 JSON-RPC 消息（请求或通知） |

| 返回值 | 说明 |
|------|------|
| `error` | 发送失败时返回错误 |

#### Receive() (*JSONRPCMessage, error)

从对端接收一条 JSON-RPC 消息。该方法是阻塞的，直到有消息到达或传输关闭。

| 返回值 | 说明 |
|------|------|
| `*JSONRPCMessage` | 接收到的 JSON-RPC 消息（响应或通知） |
| `error` | 接收失败或连接关闭时返回错误（关闭时返回 `io.EOF`） |

#### Close() error

关闭传输连接，释放相关资源。

| 返回值 | 说明 |
|------|------|
| `error` | 关闭失败时返回错误 |

---

## StdioTransport — 子进程 stdio 传输

### StdioTransport

通过子进程标准输入/输出进行通信的传输实现。每行一个 JSON 消息，以换行符 `\n` 分隔。

```go
type StdioTransport struct {
    // 未导出字段
}
```

### NewStdioTransport(command string, args []string, env map[string]string) (Transport, error)

创建 stdio 传输，启动子进程并通过其 stdin/stdout 通信。

| 参数 | 类型 | 说明 |
|------|------|------|
| `command` | `string` | 要执行的命令路径 |
| `args` | `[]string` | 命令参数列表 |
| `env` | `map[string]string` | 额外的环境变量（key=value），与父进程环境合并 |

| 返回值 | 说明 |
|------|------|
| `Transport` | 创建好的传输实例（满足 `Transport` 接口） |
| `error` | 启动子进程或建立管道失败时返回错误 |

功能描述：
1. 创建 `exec.Cmd` 子进程
2. 设置环境变量（合并传入的 `env` 与父进程环境）
3. 获取 stdin pipe（写端）和 stdout pipe（读端）
4. stderr 捕获到 bytes.Buffer（子进程启动失败或非零退出码时 stderr 内容会附加到错误消息中）
5. 启动子进程并返回传输实例

### (*StdioTransport) Send(msg *JSONRPCMessage) error

将 JSON-RPC 消息序列化后写入子进程 stdin。消息以换行符 `\n` 结尾。调用由互斥锁保护，线程安全。

| 参数 | 类型 | 说明 |
|------|------|------|
| `msg` | `*JSONRPCMessage` | 要发送的消息 |

| 返回值 | 说明 |
|------|------|
| `error` | 序列化或写入失败时返回错误 |

### (*StdioTransport) Receive() (*JSONRPCMessage, error)

从子进程 stdout 读取一行 JSON 并解析为消息。

| 返回值 | 说明 |
|------|------|
| `*JSONRPCMessage` | 解析后的消息 |
| `error` | 读取失败、JSON 解析失败或 stdout 关闭（EOF）时返回错误 |

### (*StdioTransport) Close() error

关闭 stdin 并等待子进程退出，带有 5 秒超时强制终止机制。

流程：
1. 关闭 stdin pipe
2. 启动 goroutine 调用 `cmd.Wait()` 等待子进程退出
3. 使用 `select` 等待子进程退出或 5 秒超时
4. 若正常退出：返回子进程的退出错误（非零退出码时，错误消息附带 stderr 内容）
5. 若 5 秒超时：调用 `cmd.Process.Kill()` 强制终止子进程，回收进程资源后返回超时错误

| 返回值 | 说明 |
|------|------|
| `error` | 子进程非零退出码（附 stderr 内容）、超时被强制终止、或其他等待错误时返回错误 |

> **超时强制终止**：防止子进程挂起导致 `Close()` 永久阻塞。超时时间固定为 5 秒。

---

## HTTPTransport — HTTP + SSE 传输

### HTTPTransport

通过 HTTP POST + SSE（Server-Sent Events）进行通信的传输实现。请求通过 `POST /message` 发送，响应和通知通过 `GET /sse` 的持久连接接收。

```go
type HTTPTransport struct {
    // 未导出字段
}
```

### NewHTTPTransport(baseURL string, headers map[string]string) (Transport, error)

创建 HTTP + SSE 传输。构造函数会自动连接 SSE 端点（`GET /sse`）并读取初始 `endpoint` 事件以获取 session ID。

| 参数 | 类型 | 说明 |
|------|------|------|
| `baseURL` | `string` | MCP 服务器的基础 URL（如 `http://localhost:8080`） |
| `headers` | `map[string]string` | 附加的 HTTP 请求头（key=value） |

| 返回值 | 说明 |
|------|------|
| `Transport` | 创建好的传输实例（满足 `Transport` 接口） |
| `error` | SSE 连接失败时返回错误 |

功能描述：
1. 创建 HTTP 客户端和消息通道（缓冲 32 条消息）
2. 调用 `connectSSE()` 建立 SSE 长连接
3. 从初始 SSE 事件中提取 session ID
4. 启动后台 goroutine 持续读取 SSE 事件流

### (*HTTPTransport) Send(msg *JSONRPCMessage) error

通过 HTTP POST 请求发送 JSON-RPC 消息到 `/message` 端点。如果已获取 session ID，会自动附加 `?sessionId=xxx` 查询参数。调用由互斥锁保护，线程安全。

| 参数 | 类型 | 说明 |
|------|------|------|
| `msg` | `*JSONRPCMessage` | 要发送的消息 |

| 返回值 | 说明 |
|------|------|
| `error` | 传输已关闭、序列化失败、HTTP 请求失败、或 HTTP 状态码非 2xx 时返回错误 |

### (*HTTPTransport) Receive() (*JSONRPCMessage, error)

从内部 SSE 消息通道中接收一条消息。阻塞直到有消息到达或通道被关闭。

| 返回值 | 说明 |
|------|------|
| `*JSONRPCMessage` | 接收到的消息 |
| `error` | 通道关闭时返回 `io.EOF` |

### (*HTTPTransport) Close() error

关闭 SSE 连接和内部通道，标记传输为已关闭。重复调用安全（幂等）。

| 返回值 | 说明 |
|------|------|
| `error` | 始终返回 `nil` |

---

## Client — MCP 客户端

### Client

MCP 协议客户端，封装 MCP 初始化握手、工具发现和工具调用。

```go
type Client struct {
    // 未导出字段
}
```

内部维护 `transport`（传输实例）、`serverInfo`（服务器信息）、`capabilities`（服务器能力）、`reqID`（自增请求 ID）和 `initialized`（初始化状态）。

### NewClient(transport Transport) *Client

创建一个新的 MCP 客户端实例。不会自动发起初始化，需要调用 `Initialize()`。

| 参数 | 类型 | 说明 |
|------|------|------|
| `transport` | `Transport` | 已建立的传输连接 |

| 返回值 | 说明 |
|------|------|
| `*Client` | 客户端实例 |

### (*Client) Initialize(ctx context.Context) (*InitializeResult, error)

执行 MCP 初始化握手流程。先发送 `initialize` 请求，解析服务器返回的能力和服务器信息，然后发送 `notifications/initialized` 通知完成握手。调用由互斥锁保护，线程安全。

| 参数 | 类型 | 说明 |
|------|------|------|
| `ctx` | `context.Context` | 上下文（当前实现中未使用，为后续扩展预留） |

| 返回值 | 说明 |
|------|------|
| `*InitializeResult` | 服务器返回的初始化结果，包含协议版本、服务器能力和服务器信息 |
| `error` | 调用失败或解析失败时返回错误 |

### (*Client) ListTools() ([]ToolDef, error)

获取服务器上可用的工具列表，发送 `tools/list` 请求。调用由互斥锁保护。

| 返回值 | 说明 |
|------|------|
| `[]ToolDef` | MCP 工具定义列表 |
| `error` | 调用失败或解析失败时返回错误 |

### (*Client) CallTool(name string, args map[string]any) (*CallToolResult, error)

调用服务器上的指定工具，发送 `tools/call` 请求。调用由互斥锁保护。

内部通过未导出的 `call()` 方法实现请求-响应匹配：发送请求后循环调用 `Receive()` 接收响应，通过 `ID` 字段匹配对应的请求。接收循环最多执行 **100 次迭代**（常量 `maxRecvLoops = 100`），超过后返回错误，防止因对端无响应导致无限阻塞。循环中忽略不匹配 ID 的消息（如通知）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `name` | `string` | 要调用的工具名称 |
| `args` | `map[string]any` | 传递给工具的参数 |

| 返回值 | 说明 |
|------|------|
| `*CallToolResult` | 工具执行结果，包含内容列表和错误标记 |
| `error` | 调用失败或解析结果失败时返回错误 |

### (*Client) Close() error

关闭客户端，释放底层传输连接。

| 返回值 | 说明 |
|------|------|
| `error` | 关闭失败时返回错误 |

### (*Client) ServerName() string

返回 MCP 服务器的名称（在 `Initialize` 成功后填充）。

| 返回值 | 说明 |
|------|------|
| `string` | 服务器名称。初始化前调用返回空字符串 |

---

## ToolAdapter — MCP 工具适配为 catcode Tool

### AdaptTool(mcpTool ToolDef, serverName string, client *Client) *tool.Tool

将 MCP 工具定义转换为 catcode 的 `tool.Tool` 对象。转换后的工具包装了 MCP 调用逻辑，可以在 catcode 的工具注册表中使用。

**命名规则**：生成 catcode 工具名格式为 `mcp__{服务器名}__{工具名}`，所有名称中的 `-` 和空格被替换为 `_` 并转为小写。

| 参数 | 类型 | 说明 |
|------|------|------|
| `mcpTool` | `ToolDef` | MCP 工具定义（来自 `tools/list` 响应） |
| `serverName` | `string` | MCP 服务器名称，用于命名前缀和描述标注 |
| `client` | `*Client` | 与此 MCP 服务器连接的客户端实例 |

| 返回值 | 说明 |
|------|------|
| `*tool.Tool` | catcode 工具对象，包含函数定义和调用回调 |

返回的 `*tool.Tool` 结构：
- **Function.Name**：`"mcp__{serverName}__{toolName}"`（通过 `sanitizeName` 清理）
- **Function.Description**：`"[MCP:{serverName}] {原始描述}"`
- **Function.Parameters**：来自 MCP 工具的 `InputSchema`（JSON Schema），或当为空时回退为 `{"type":"object","properties":{}}`
- **Call**：闭包函数，接收 `*tool.Context` 和 `map[string]any` 参数，内部调用 `client.CallTool(mcpTool.Name, args)`，并通过 `formatToolResult` 格式化返回结果

### convertInputSchema(raw json.RawMessage) json.RawMessage

（未导出）将 MCP 的输入 Schema 转换为 catcode 兼容格式。若传入空值，返回一个空的 object Schema `{"type":"object","properties":{}}`。

### formatToolResult(result *CallToolResult) string

（未导出）将 `CallToolResult` 格式化为纯文本字符串：
- `text` 类型内容项直接拼接文本
- 其他类型（`image`、`resource`）格式化为 `[{type}: {data}]`
- result 为 nil 时返回空字符串

### sanitizeName(name string) string

（未导出）清理名称中的特殊字符：将 `-` 和空格替换为 `_`，全部转为小写。

---

## Manager — 多服务器连接管理

### ServerConfig

MCP 服务器配置，通常从 catcode 配置文件中加载。

```go
type ServerConfig struct {
    Name      string            `json:"name"`
    Transport string            `json:"transport"`
    Command   string            `json:"command,omitempty"`
    Args      []string          `json:"args,omitempty"`
    Env       map[string]string `json:"env,omitempty"`
    URL       string            `json:"url,omitempty"`
    Enabled   bool              `json:"enabled"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 服务器名称（用于工具命名前缀） |
| `Transport` | `string` | 传输类型：`"stdio"` 或 `"http"`/`"sse"` |
| `Command` | `string` | stdio 模式下要执行的命令 |
| `Args` | `[]string` | stdio 模式下的命令参数 |
| `Env` | `map[string]string` | 环境变量（stdio 模式下传给子进程；HTTP 模式下作为请求头） |
| `URL` | `string` | HTTP 模式下的服务器基础 URL |
| `Enabled` | `bool` | 是否启用此服务器连接 |

### Manager

MCP 多服务器连接管理器，负责管理多个 MCP 服务器连接并聚合所有工具。

```go
type Manager struct {
    // 未导出字段
}
```

内部维护 `servers`（名称到客户端的映射）和 `tools`（所有已注册工具的聚合列表）。

### NewManager() *Manager

创建 MCP 管理器实例。

| 返回值 | 说明 |
|------|------|
| `*Manager` | 管理器实例 |

### (*Manager) ConnectServer(cfg ServerConfig) ([]*tool.Tool, error)

根据配置连接一个 MCP 服务器，初始化连接，获取工具列表，并将所有工具适配为 catcode Tool 后注册到管理器中。

| 参数 | 类型 | 说明 |
|------|------|------|
| `cfg` | `ServerConfig` | 服务器配置 |

| 返回值 | 说明 |
|------|------|
| `[]*tool.Tool` | 此服务器提供的所有 catcode 工具列表 |
| `error` | 服务器未启用（返回 nil, nil）或连接/初始化/获取工具失败时返回错误 |

流程：
1. 若 `cfg.Enabled` 为 false，返回 nil, nil
2. 根据 `cfg.Transport` 创建对应传输（`"stdio"` → `NewStdioTransport`，`"http"`/`"sse"` → `NewHTTPTransport`）
3. 创建 Client 并调用 `Initialize()`
4. 调用 `ListTools()` 获取服务器工具
5. 使用 `AdaptTool()` 将所有工具转换为 catcode Tool
6. 存入内部 `servers` 和 `tools` 映射

### (*Manager) DisconnectAll()

断开所有已连接的 MCP 服务器，关闭传输并清空内部状态。

### (*Manager) ServerCount() int

返回当前已连接的 MCP 服务器数量。

| 返回值 | 说明 |
|------|------|
| `int` | 已连接的服务器数量 |

### (*Manager) ToolCount() int

返回当前所有 MCP 服务器提供的工具总数。

| 返回值 | 说明 |
|------|------|
| `int` | 已注册的工具总数 |

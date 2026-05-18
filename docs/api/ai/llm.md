# ai/llm — LLM 提供商抽象层

## 包概述

`ai/llm` 包提供了 LLM（大语言模型）提供商的统一抽象层，核心组件包括：

- **Provider 接口**：定义与 LLM 交互的标准契约，支持流式和非流式两种对话模式
- **OpenAIClient 实现**：基于 OpenAI 兼容 API 的默认实现，内置指数退避重试和 SSE（Server-Sent Events）流式解析
- **ProviderRegistry**：多 Provider 管理器，支持按名称注册和查找不同 Provider 实例，通过模型名中的前缀（如 `deepseek:deepseek-chat`）自动路由到对应后端
- **模型名解析工具**：`provider:modelname` 格式的解析与组合函数
- **流式事件系统**：完整的 SSE 流式响应处理，包含空闲超时检测和工具调用增量累积

## 核心类型

### ChatRequest

统一对话请求结构体。

```go
type ChatRequest struct {
    Model       string          `json:"model"`
    Messages    []Message       `json:"messages"`
    Tools       []ToolDef       `json:"tools,omitempty"`
    Temperature float64         `json:"temperature,omitempty"`
    MaxTokens   int             `json:"max_tokens,omitempty"`
    Stream      bool            `json:"stream"`
    Thinking    *ThinkingConfig `json:"thinking,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Model` | `string` | 模型名称，支持 `provider:modelname` 格式 |
| `Messages` | `[]Message` | 对话消息列表 |
| `Tools` | `[]ToolDef` | 可用的工具定义列表，为空时序列化省略 |
| `Temperature` | `float64` | 采样温度，控制输出随机性；为 0 时省略 |
| `MaxTokens` | `int` | 最大生成 token 数；为 0 时省略 |
| `Stream` | `bool` | 是否启用流式输出 |
| `Thinking` | `*ThinkingConfig` | thinking 模式配置（深度推理），为 nil 时省略 |

### ThinkingConfig

thinking（深度推理）模式配置。

```go
type ThinkingConfig struct {
    Type         string `json:"type"` // "enabled"
    BudgetTokens int    `json:"budget_tokens"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Type` | `string` | 模式类型，当前仅支持 `"enabled"` |
| `BudgetTokens` | `int` | 推理阶段的 token 预算上限 |

### Message

对话中的单条消息。

```go
type Message struct {
    Role             string     `json:"role"`
    Content          string     `json:"content"`
    ReasoningContent string     `json:"reasoning_content,omitempty"`
    Name             string     `json:"name,omitempty"`
    ToolCallID       string     `json:"tool_call_id,omitempty"`
    ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Role` | `string` | 消息角色：`"system"`、`"user"`、`"assistant"`、`"tool"` |
| `Content` | `string` | 消息正文 |
| `ReasoningContent` | `string` | thinking 模式下的推理链内容，序列化时省略空值 |
| `Name` | `string` | 发送者名称（可选） |
| `ToolCallID` | `string` | 工具调用 ID，用于 `role=tool` 类型消息关联回工具调用 |
| `ToolCalls` | `[]ToolCall` | 助手工具调用列表，序列化时省略空值 |

### ToolDef

发送给 LLM 的工具定义。

```go
type ToolDef struct {
    Type     string  `json:"type"`
    Function FuncDef `json:"function"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Type` | `string` | 工具类型，通常为 `"function"` |
| `Function` | `FuncDef` | 函数详细信息 |

### FuncDef

函数定义详情。

```go
type FuncDef struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 函数名称 |
| `Description` | `string` | 函数功能描述 |
| `Parameters` | `json.RawMessage` | JSON Schema 格式的函数参数定义 |

### ToolCall

LLM 返回的工具调用。

```go
type ToolCall struct {
    ID       string       `json:"id"`
    Type     string       `json:"type"`
    Function ToolCallFunc `json:"function"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `string` | 工具调用的唯一标识 |
| `Type` | `string` | 调用类型，通常为 `"function"` |
| `Function` | `ToolCallFunc` | 函数调用信息 |

### ToolCallFunc

工具调用的函数部分。

```go
type ToolCallFunc struct {
    Name      string `json:"name"`
    Arguments string `json:"arguments"` // JSON 字符串
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 要调用的函数名 |
| `Arguments` | `string` | 函数参数的 JSON 字符串 |

### ChatResponse

非流式对话的完整响应。

```go
type ChatResponse struct {
    ID      string   `json:"id"`
    Model   string   `json:"model"`
    Choices []Choice `json:"choices"`
    Usage   Usage    `json:"usage"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `string` | 响应唯一标识 |
| `Model` | `string` | 实际使用的模型名称 |
| `Choices` | `[]Choice` | 生成的候选回复列表 |
| `Usage` | `Usage` | token 使用统计 |

### Choice

响应中的单个候选回复。

```go
type Choice struct {
    Index   int     `json:"index"`
    Message Message `json:"message"`
    Reason  string  `json:"finish_reason"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Index` | `int` | 候选序号 |
| `Message` | `Message` | 助手回复消息 |
| `Reason` | `string` | 结束原因：`"stop"`、`"tool_calls"`、`"length"` 等 |

### Usage

Token 使用统计。

```go
type Usage struct {
    PromptTokens     int `json:"prompt_tokens"`
    CompletionTokens int `json:"completion_tokens"`
    TotalTokens      int `json:"total_tokens"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `PromptTokens` | `int` | 输入（prompt）消耗的 token 数 |
| `CompletionTokens` | `int` | 输出（completion）消耗的 token 数 |
| `TotalTokens` | `int` | 总 token 消耗 |

---

## Provider 接口

```go
type Provider interface {
    Name() string
    Chat(ctx context.Context, req *ChatRequest) (<-chan *StreamEvent, error)
    ChatSync(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
    io.Closer
}
```

`Provider` 是模型提供商的统一抽象接口，允许在不同后端（OpenAI 兼容 API、Anthropic 等）之间无缝切换。任何实现了此接口的类型都可被 `ProviderRegistry` 管理。

### Name() string

返回提供商名称（例如 `"openai"`、`"deepseek"`）。

### Chat(ctx context.Context, req *ChatRequest) (<-chan *StreamEvent, error)

发送**流式**对话请求。

- **参数**：
  - `ctx`：上下文，用于取消请求
  - `req`：对话请求，方法内部会将 `req.Stream` 强制设为 `true`
- **返回值**：
  - `<-chan *StreamEvent`：SSE 事件流 channel，缓冲区大小为 32；调用方通过 `range` 遍历接收事件
  - `error`：发送请求阶段的错误（连接/网络/序列化错误），流读取阶段错误通过 `StreamError` 事件传递

调用方必须消费 channel 直到关闭，否则会泄漏 goroutine。

### ChatSync(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

发送**非流式**对话请求，等待并返回完整的响应。

- **参数**：
  - `ctx`：上下文，用于取消请求
  - `req`：对话请求，方法内部会将 `req.Stream` 强制设为 `false`
- **返回值**：
  - `*ChatResponse`：完整的对话响应
  - `error`：请求或解析错误

### io.Closer

接口嵌套了标准库 `io.Closer`，要求实现 `Close() error` 方法以释放 Provider 持有的资源（如 HTTP 连接）。

---

## 流式事件系统

### StreamEventType

```go
type StreamEventType int
```

流式事件类型的枚举。

#### 常量

| 常量 | 值 | 含义 |
|------|-----|------|
| `StreamTextDelta` | `iota` (0) | 文本增量事件，表示模型输出的一段文本 |
| `StreamReasoning` | `iota` (1) | 推理内容事件，表示 thinking 模式下的推理链增量 |
| `StreamToolCall` | `iota` (2) | 工具调用事件，表示模型请求调用一个工具 |
| `StreamDone` | `iota` (3) | 流结束事件，表示正常完成 |
| `StreamError` | `iota` (4) | 错误事件，表示流处理中发生错误 |

#### func (StreamEventType) String() string

返回 `StreamEventType` 的可读字符串表示：

| 常量 | 返回值 |
|------|--------|
| `StreamTextDelta` | `"TextDelta"` |
| `StreamToolCall` | `"ToolCall"` |
| `StreamDone` | `"Done"` |
| `StreamError` | `"Error"` |
| 未知值 | `"Unknown"` |

### StreamEvent

```go
type StreamEvent struct {
    Type             StreamEventType
    Content          string    // TextDelta 时的文本增量
    ReasoningContent string    // Reasoning 时的推理增量
    Tool             *ToolCall // ToolCall 时的工具调用
    Usage            *Usage    // Done 时的 token 统计
    Error            error     // Error 时的错误
}
```

流式事件，承载 SSE 流解析出的各类数据。不同 `Type` 下有效字段不同：

| Type 值 | 有效字段 |
|---------|----------|
| `StreamTextDelta` | `Content`：本次增量文本 |
| `StreamReasoning` | `ReasoningContent`：推理链增量内容 |
| `StreamToolCall` | `Tool`：完整的工具调用对象 |
| `StreamDone` | `Usage`（可选）：最终 token 统计 |
| `StreamError` | `Error`：错误信息 |

---

## OpenAIClient 实现

```go
type OpenAIClient struct {
    // 未导出字段：name, baseURL, apiKey, client
}
```

`OpenAIClient` 是 OpenAI 兼容 API 的 `Provider` 接口实现，支持任何遵循 OpenAI Chat Completions 协议的 API 端点（如 OpenAI、DeepSeek、Qwen 等）。

**内部结构（不导出）：**
- `name`：提供商名称
- `baseURL`：API 基础 URL（末尾不含 `/`）
- `apiKey`：API 认证密钥
- `client`：`*http.Client`，超时时间 10 分钟

### NewOpenAI(name, baseURL, apiKey string) Provider

```go
func NewOpenAI(name, baseURL, apiKey string) Provider
```

创建 OpenAI 兼容客户端实例。

- **参数**：
  - `name`：提供商名称，用于标识和 ProviderRegistry 路由
  - `baseURL`：API 基础地址，如 `"https://api.openai.com/v1"`，末尾 `/` 会被自动去除
  - `apiKey`：API 认证密钥
- **返回值**：`Provider` 接口实例，可传入 `ProviderRegistry.Register()`

### (*OpenAIClient).Name() string

返回创建时指定的提供商名称。

### (*OpenAIClient).Close() error

关闭客户端。当前实现为空操作（no-op），HTTP 客户端无需显式关闭。返回 `nil`。

### (*OpenAIClient).Chat(ctx context.Context, req *ChatRequest) (<-chan *StreamEvent, error)

发送流式对话请求（含自动重试）。

**重试策略**：
- 最多重试 **3 次**
- 指数退避：第 1 次重试间隔 1 秒，第 2 次间隔 4 秒
- 可重试的错误：HTTP 429（限流）或 >= 500（服务端错误）
- 不可重试的错误：HTTP 4xx（除 429 外）和请求构建阶段的错误直接返回
- 重试前会检查 `ctx` 是否已取消
- 3 次重试均失败后返回包装了最后一次错误的 `error`

**请求流程**：
1. 强制设置 `req.Stream = true`
2. 调用 `buildRequestBody` 用零拷贝 Buffer 构建 JSON 请求体，并解析模型名（去除 provider 前缀）
3. 发送 POST 请求到 `{baseURL}/chat/completions`
4. 成功时调用 `parseSSE` 解析流式响应并返回事件 channel
5. 429/5xx 错误时进入重试循环

**SSE 解析特点**（`parseSSE`，内部函数）：
- 解耦设计：使用独立的行读取 goroutine 从阻塞的 `bufio.Scanner` 中读取，避免主事件循环被阻塞
- **空闲超时**：`streamIdleTimeout = 180 秒`，若在此期间未收到任何 SSE 数据，发送 `StreamError` 事件并关闭 channel
- 工具调用增量累积：流式 API 对 tool_calls 按 index 逐片段下发，解析器在内部 `accumulatedTools` map 中累积各字段（ID、Type、Name、Arguments），流结束时统一发送完整的 `StreamToolCall` 事件
- 识别 `data: [DONE]` 标记并优雅结束
- JSON 解析失败的单行数据会被静默跳过（continue）

### (*OpenAIClient).ChatSync(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

发送非流式对话请求（含自动重试）。

与 `Chat` 区别：
- 强制设置 `req.Stream = false`
- 不启动 SSE 解析，直接使用 `json.NewDecoder` 将整个响应体反序列化为 `ChatResponse`
- 重试策略与 `Chat` 完全相同（3 次，指数退避）

### (*OpenAIClient).buildRequestBody(req *ChatRequest) (io.Reader, error)

（内部方法，不导出）

构建请求体。使用 `catcode/core/buffer` 零拷贝 Buffer 拼接 JSON，包含以下处理：

1. 通过 `ParseModelName` 提取纯模型名（去除 provider 前缀）
2. 序列化 Messages 和 Tools
3. 仅当值非零时才包含 `temperature`、`max_tokens` 字段
4. 仅当 `Thinking.Type == "enabled"` 时才包含 thinking 配置

---

## ProviderRegistry

```go
type ProviderRegistry struct {
    // 未导出字段：mu, providers, default_
}
```

多 Provider 管理器，允许不同智能体使用不同的 LLM 后端。

### NewProviderRegistry(defaultProvider Provider) *ProviderRegistry

```go
func NewProviderRegistry(defaultProvider Provider) *ProviderRegistry
```

创建 Provider 注册表。

- **参数**：`defaultProvider`——默认 Provider，在未匹配到特定 Provider 时被使用
- **返回值**：初始化后的注册表

### (*ProviderRegistry).Register(name string, p Provider)

```go
func (pr *ProviderRegistry) Register(name string, p Provider)
```

向注册表中注册一个 Provider，与 `ParseModelName` 提取的 provider 名称对应。

- 并发安全：内部使用 `sync.RWMutex` 写锁保护
- 若已存在同名 Provider，会覆盖

### (*ProviderRegistry).Get(name string) Provider

```go
func (pr *ProviderRegistry) Get(name string) Provider
```

按名称精确查找 Provider。

- 若 `name` 为空字符串，直接返回默认 Provider
- 若未找到，返回默认 Provider（不会返回 nil）
- 并发安全：使用读锁

### (*ProviderRegistry).Default() Provider

```go
func (pr *ProviderRegistry) Default() Provider
```

返回默认 Provider，无锁开销。

### (*ProviderRegistry).Names() []string

```go
func (pr *ProviderRegistry) Names() []string
```

返回所有已注册 Provider 的名称列表。

- 返回顺序不确定（map 遍历）
- 不包含默认 Provider（除非它同时也被显式注册）
- 并发安全：使用读锁

### (*ProviderRegistry).ResolveModel(fullName string) (Provider, string)

```go
func (pr *ProviderRegistry) ResolveModel(fullName string) (Provider, string)
```

从完整模型名中解析对应的 Provider 实例和纯模型名。这是将用户指定的模型名路由到具体后端的关键方法。

**解析规则**：

| 输入 | 返回 (Provider, modelName) |
|------|---------------------------|
| `"deepseek:deepseek-chat"` | `(deepseek-provider, "deepseek-chat")` |
| `"openai:gpt-4"` | `(openai-provider, "gpt-4")` |
| `"deepseek-chat"` | `(default-provider, "deepseek-chat")` |

- 内部调用 `ParseModelName` 拆分 provider 前缀和模型名
- 若无前缀，直接返回默认 Provider
- 若前缀对应的 Provider 未注册，回退到默认 Provider
- 并发安全

---

## 模型名工具

### ParseModelName(fullName string) (provider, model string)

```go
func ParseModelName(fullName string) (provider, model string)
```

解析 `"provider:modelname"` 格式的完整模型名，按第一个冒号分割。

**示例**：
- `"deepseek:deepseek-chat"` → `("deepseek", "deepseek-chat")`
- `"openai:gpt-4"` → `("openai", "gpt-4")`
- `"deepseek-chat"` → `("", "deepseek-chat")`

当字符串中无冒号时，`provider` 返回空字符串，`model` 返回原字符串，表示使用默认 Provider。

### BuildModelName(provider, model string) string

```go
func BuildModelName(provider, model string) string
```

将 provider 名称和模型名称组合为完整的 `"provider:modelname"` 格式。

- 若 `provider` 为空字符串，直接返回 `model`（不添加前缀冒号）
- 示例：`BuildModelName("deepseek", "deepseek-chat")` → `"deepseek:deepseek-chat"`
- 示例：`BuildModelName("", "deepseek-chat")` → `"deepseek-chat"`

---

## 工具函数

### EstimateTokens(text string) int

```go
func EstimateTokens(text string) int
```

估算文本的 token 数量，使用启发式方法：**约每 4 个 Unicode 字符算 1 个 token**。

- 适用于中英文混合文本的粗略估算
- 注意：这只是近似估算，实际 token 数取决于具体模型的分词器

### ConvertToolCalls

- **签名**: `func ConvertToolCalls(calls []*ToolCall) []ToolCall`
- **参数**: `calls` — `*ToolCall` 指针切片
- **返回值**: `[]ToolCall` — 值拷贝切片
- **功能**: 将 `[]*ToolCall` 转换为 `[]ToolCall`（值拷贝），避免跨 goroutine 共享指针导致的并发问题。使用 `*tc` 解引用方式实现，自动适配所有字段变更。
- **注意**: 这是浅拷贝，嵌套的切片/映射字段仍共享底层数据。由于 `ToolCall` 的所有字段都是值类型（string/struct），浅拷贝在此场景下足够安全。


### CollectStreamContent(ch <-chan *StreamEvent) (string, []*ToolCall, error)

```go
func CollectStreamContent(ch <-chan *StreamEvent) (string, []*ToolCall, error)
```

收集流式事件 channel 中的所有内容，直到 channel 关闭。

- **参数**：`ch`——SSE 事件流，通常来自 `Provider.Chat()` 的返回值
- **返回值**：
  - `string`：累积的所有文本增量（`StreamTextDelta` 拼接结果）
  - `[]*ToolCall`：收集的所有工具调用（来自 `StreamToolCall` 事件）
  - `error`：遇到 `StreamError` 事件时返回对应错误，正常结束（`StreamDone`）返回 `nil`
- channel 一直消费直到关闭，返回时可能已读取了 `StreamError` 或 `StreamDone` 之后 channel 中剩余的后续事件

---

## 典型用法

```go
// 1. 创建 Provider
provider := llm.NewOpenAI("deepseek", "https://api.deepseek.com/v1", "sk-xxx")

// 2. 注册到 Registry
registry := llm.NewProviderRegistry(provider)
registry.Register("openai", llm.NewOpenAI("openai", "https://api.openai.com/v1", "sk-yyy"))

// 3. 解析模型名并路由
p, model := registry.ResolveModel("deepseek:deepseek-chat")

// 4. 发送流式请求
req := &llm.ChatRequest{
    Model:    model,
    Messages: []llm.Message{{Role: "user", Content: "你好"}},
    Stream:   true,
}
ch, err := p.Chat(ctx, req)
if err != nil {
    // 处理连接/序列化错误
}
for evt := range ch {
    switch evt.Type {
    case llm.StreamTextDelta:
        fmt.Print(evt.Content)
    case llm.StreamToolCall:
        // 处理工具调用
    case llm.StreamError:
        // 处理流读取错误
    case llm.StreamDone:
        // 流结束
    }
}
```

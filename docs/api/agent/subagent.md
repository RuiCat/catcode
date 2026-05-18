# agent/subagent — 子智能体接口与实现

## 包概述

`agent/subagent` 包定义了子智能体（SubAgent）的抽象接口及其基础实现 `BaseAgent`。子智能体是 catcode 多智能体系统中承担特定子任务的独立智能体单元，每种子智能体类型（如 `explore`、`plan`、`general`、`reviewer`、`verifier`、`guard`）在独立的 LLM 会话中执行任务，并通过事件总线与主智能体通信。

### 核心设计

- **SubAgent 接口**：统一抽象，所有子智能体必须实现该接口。
- **BaseAgent 结构体**：子智能体的通用实现，包含完整的工具调用循环、权限检查、流式处理、上下文压缩等能力。
- **GuardReviewer 接口**：将 guard 审查逻辑与 pool 包解耦，打破循环依赖。
- **ask_architect**：每个子智能体都内置该工具，实现子智能体到主智能体的双向通信。
- **ContextBuilder**：可插拔的上下文构建器接口，Hook 系统通过此接口注入，实现子智能体行为的动态扩展。

### 文件拆分

| 文件 | 说明 |
|------|------|
| `interface.go` | 接口定义（SubAgent, GuardReviewer, ContextBuilder, ContextBuildInput/Result, AgentSnapshot） |
| `config.go` | `Config` 配置结构体定义 |
| `base.go` | `BaseAgent` 核心实现：构造器、session 管理、Execute 入口、状态快照、Hook 上下文构建集成 |
| `base_stream.go` | 流式响应处理：`processStream`、`runToolLoop`、`prepareNextRound`、上下文压缩 |
| `base_guard.go` | Guard 审查：`reviewWithGuard`、LRU 缓存（`guardLRUCache`）、审查结果判定 |
| `base_tools.go` | 工具调用执行：`executeToolCalls`、权限检查、错误收集（`collectToolError`） |



---

## GuardReviewer 循环依赖打破模式

`GuardReviewer` 接口是专门为解决 `agent/subagent` 与 `agent/pool` 之间的循环依赖而设计的：

- `agent/subagent` 的 `BaseAgent` 在执行 `bash` 命令前，需要调用 guard 子智能体进行 LLM 级安全审查。
- guard 子智能体本身也是通过 `agent/pool` 管理的 `SubAgent` 实例。
- 如果在 `agent/subagent` 中直接导入 `agent/pool`，将形成循环引用。

**解决方案**：在 `agent/subagent` 中定义 `GuardReviewer` 抽象接口，仅声明 `Execute` 和 `GetOrCreate` 两个方法；`agent/pool` 在创建子智能体时注入实现了该接口的具体审查器。这样 `agent/subagent` 只依赖接口，不依赖具体实现。

同时，`SetGuardReviewer` 内部会判断当前智能体类型是否为 `"guard"`，若是则拒绝设置审查器，防止 guard 审查 guard 自身的无限递归。

---

## 导出类型

### `AgentSnapshot`

子智能体状态快照，用于外部（如 TUI）监控子智能体运行状态。

```go
type AgentSnapshot struct {
    Name        string
    ID          string
    Status      string
    Task        string
    FullTask    string
    CurrentTool string
    ToolCount   int
    StartTime   time.Time
    Duration    time.Duration
    ErrorMsg    string
    FullOutput  string
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 子智能体类型名（如 `"explore"`） |
| `ID` | `string` | 子智能体唯一标识（如 `"subagent-explore"`） |
| `Status` | `string` | 当前状态：`"idle"` / `"pending"` / `"running"` / `"completed"` / `"error"` |
| `Task` | `string` | 当前任务描述（截断至 60 字符） |
| `FullTask` | `string` | 当前任务完整描述（未截断） |
| `CurrentTool` | `string` | 当前正在执行的工具名称 |
| `ToolCount` | `int` | 本次任务中已执行的工具调用次数 |
| `StartTime` | `time.Time` | 本次任务开始时间 |
| `Duration` | `time.Duration` | 本次任务耗时（任务完成时有效） |
| `ErrorMsg` | `string` | 错误信息（任务失败时有效） |
| `FullOutput` | `string` | 格式化完整输出，包含文本内容和工具调用记录 |

快照通过 `BaseAgent.Snapshot()` 方法获取，内部使用读锁保证线程安全。

---

### `Config`

子智能体初始化配置。

```go
type Config struct {
    Type         string
    Model        string
    SystemPrompt string
    Temperature  float64
    MaxTokens    int
    Tools        []string
    ProviderName string
    Permissions  []tool.PermissionRule
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Type` | `string` | 子智能体类型，如 `"explore"`、`"plan"`、`"guard"` 等 |
| `Model` | `string` | 使用的 LLM 模型名 |
| `SystemPrompt` | `string` | 系统提示词，定义子智能体的角色和行为 |
| `Temperature` | `float64` | LLM 采样温度 |
| `MaxTokens` | `int` | 单次 LLM 响应的最大 token 数 |
| `Tools` | `[]string` | 需要注册的工具名列表（如 `["bash", "read", "write"]`） |
| `ProviderName` | `string` | LLM 提供商名称，从 `ProviderRegistry` 中查找对应的 Provider |
| `Permissions` | `[]tool.PermissionRule` | 工具的权限规则列表 |

---

### `ContextBuildInput` — 上下文构建输入

Hook 系统上下文构建的输入参数，同时定义于 `agent/subagent` 和 `agent/subagent/hook` 两个包中。

```go
type ContextBuildInput struct {
    Task           string
    ContextSummary string
    AgentType      string
    Extra          map[string]any
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Task` | `string` | 子智能体需要执行的任务描述 |
| `ContextSummary` | `string` | 来自主智能体的上下文摘要 |
| `AgentType` | `string` | 子智能体类型标识符（如 `"explore"`） |
| `Extra` | `map[string]any` | 扩展字段，传递额外的自定义数据 |

---

### `ContextBuildResult` — 上下文构建输出

```go
type ContextBuildResult struct {
    SystemPrompt        string
    MemoryIndex         string
    ExtraSystemMessages []string
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `SystemPrompt` | `string` | 覆盖系统提示词（非空时替换原有提示词） |
| `MemoryIndex` | `string` | 覆盖记忆索引（非空时替换原有索引） |
| `ExtraSystemMessages` | `[]string` | 附加的系统消息列表，以 system 角色注入会话 |

---

---

## 导出接口

### `GuardReviewer`

Guard 审查器接口，用于打破 `agent/subagent` 与 `agent/pool` 之间的循环依赖。

```go
type GuardReviewer interface {
    Execute(ctx context.Context, agentType, task, contextSummary string) (<-chan string, error)
    GetOrCreate(agentType string) (SubAgent, error)
}
```

#### `Execute(ctx, agentType, task, contextSummary) (<-chan string, error)`

执行一次审查任务。

| 参数 | 类型 | 说明 |
|------|------|------|
| `ctx` | `context.Context` | 上下文，用于超时控制（通常设置 60 秒超时） |
| `agentType` | `string` | 被调用的子智能体类型，固定为 `"guard"` |
| `task` | `string` | 审查任务描述，包含需要审查的命令 |
| `contextSummary` | `string` | 上下文摘要，包含当前正在执行的任务信息 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `ch` | `<-chan string` | 审查结果流式文本通道 |
| `err` | `error` | 启动审查失败时返回错误 |

#### `GetOrCreate(agentType) (SubAgent, error)`

获取或创建指定类型的子智能体实例。审查完成后用于获取 guard 子智能体的会话对象以清理消息。

| 参数 | 类型 | 说明 |
|------|------|------|
| `agentType` | `string` | 子智能体类型，固定为 `"guard"` |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `inst` | `SubAgent` | 获取到的子智能体实例 |
| `err` | `error` | 获取失败时返回错误 |

---

### `SubAgent`

子智能体核心接口，定义了所有子智能体必须实现的能力。

```go
type SubAgent interface {
    ID() string
    Type() string
    Execute(ctx context.Context, task, contextSummary string) (<-chan string, error)
    IsBusy() bool
    Snapshot() AgentSnapshot
    SetWorkspaceDB(wdb storage.WorkspaceDB)
    SetMemoryService(ms storage.MemoryService)
    SetGuardReviewer(reviewer GuardReviewer)
    SetAskArchitectCallback(fn func(question string) string)
    SetContextLimit(limit int)
    RegisterTool(t *tool.Tool) error
    ResetSession()
    GetSession() *session.Session
}
```

#### `ID() string`

返回子智能体的唯一标识符，格式为 `"subagent-{Type}"`。

**返回值**：`string` — 唯一 ID，如 `"subagent-explore"`。

---

#### `Type() string`

返回子智能体的类型名。类型决定系统提示词和行为模式。

**返回值**：`string` — 类型名，如 `"explore"`、`"plan"`、`"guard"` 等。

---

#### `Execute(ctx, task, contextSummary) (<-chan string, error)`

执行子任务。这是子智能体的核心入口，启动完整的工具调用循环。

| 参数 | 类型 | 说明 |
|------|------|------|
| `ctx` | `context.Context` | 上下文，可通过取消信号中断执行 |
| `task` | `string` | 子任务描述，作为 user 消息发送给 LLM |
| `contextSummary` | `string` | 上下文摘要，作为 system 消息注入，帮助子智能体理解任务背景 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `ch` | `<-chan string` | 流式文本响应通道。包含 LLM 推理输出、工具调用过程、最终结果。当任务完成或出错时关闭 |
| `err` | `error` | 启动失败时返回错误（如正在执行其他任务、构建请求失败、LLM 连接失败） |

**行为**：
1. 检查状态：若当前为 `"running"` 或 `"pending"`，拒绝执行并返回错误。
2. 从 DB 恢复历史会话（若已配置 `WorkspaceDB`）。
3. 将任务作为 user 消息添加到会话中。
4. 构建 LLM 请求并通过 Provider 发起流式调用。
5. 启动 goroutine 运行 `runToolLoop`，驱动工具调用循环。
6. 任务完成后将会话持久化到 DB。

---

#### `IsBusy() bool`

检查子智能体是否正在忙碌（状态为 `"pending"` 或 `"running"`）。

**返回值**：`bool` — 忙碌时返回 `true`。

**线程安全**：使用读锁保护。

---

#### `Snapshot() AgentSnapshot`

获取子智能体当前状态快照，供外部监控（如 TUI 状态面板）。

**返回值**：`AgentSnapshot` — 包含当前所有状态字段的快照。

**线程安全**：使用读锁保护。

---

#### `SetWorkspaceDB(wdb)`

设置工作区数据库引用，启用会话持久化能力。

| 参数 | 类型 | 说明 |
|------|------|------|
| `wdb` | `storage.WorkspaceDB` | 工作区数据库接口实例。设为 `nil` 可关闭持久化 |

**附加行为**：从 DB 读取 `tool.max_result_length` 配置，设置工具结果截断长度。

---

#### `SetMemoryService(ms)`

设置记忆服务引用，用于在任务执行前构建语义索引。

| 参数 | 类型 | 说明 |
|------|------|------|
| `ms` | `storage.MemoryService` | 记忆服务接口实例 |

---

#### `SetGuardReviewer(reviewer)`

设置 Guard 审查器。设置后，子智能体在执行 bash 命令前会调用 guard 子智能体进行 LLM 级安全审查。

| 参数 | 类型 | 说明 |
|------|------|------|
| `reviewer` | `GuardReviewer` | Guard 审查器实例。设为 `nil` 可关闭审查 |

**特殊行为**：若当前智能体类型为 `"guard"`，该方法静默忽略（防止 guard 审查自身）。

---

#### `SetAskArchitectCallback(fn)`

设置向主智能体提问的回调函数。与内置的 `ask_architect` 工具配合使用，实现子智能体到主智能体的双向通信。

| 参数 | 类型 | 说明 |
|------|------|------|
| `fn` | `func(question string) string` | 回调函数，接收问题文本，返回主智能体的回答文本 |

---

#### `SetContextLimit(limit)`

设置上下文窗口大小（token 数），用于判断是否需要进行上下文压缩。

| 参数 | 类型 | 说明 |
|------|------|------|
| `limit` | `int` | 上下文窗口大小（token 数）。若 `<= 0` 则忽略 |

默认值：`65536`（64K）。

---

#### `SetContextBuilder(builder)`

设置上下文构建器（Hook 系统入口）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `builder` | `ContextBuilder` | 上下文构建器实例。设为 `nil` 可关闭 Hook 注入 |

**调用时机**：在创建子智能体后、首次执行前调用。通常由 `agent/pool` 在初始化时注入 `YaegiContextBuilder` 实例。

**行为**：若已设置，`Execute()` 会调用 `builder.BuildContext()` 获取自定义的 SystemPrompt、MemoryIndex 和 ExtraSystemMessages，并应用到会话中。若未设置（`nil`），跳过 Hook，使用默认行为。

---

#### `RegisterTool(t) error`

向子智能体的独立会话注册一个工具。

| 参数 | 类型 | 说明 |
|------|------|------|
| `t` | `*tool.Tool` | 要注册的工具实例 |

| 返回值 | 说明 |
|--------|------|
| `error` | 注册失败时返回错误（如工具名冲突） |

---

#### `ResetSession()`

重置子智能体会话。清空所有消息历史，但保留已注册的工具。

---

#### `GetSession() *session.Session`

获取子智能体的独立 LLM 会话对象。调用者可通过该对象直接操作会话消息、摘要等。

**返回值**：`*session.Session` — 子智能体的 LLM 会话实例。

---

### `ContextBuilder` — 上下文构建器接口

Hook 系统通过此接口注入到子智能体的执行生命周期。定义于 `agent/subagent` 包，由 `agent/subagent/hook` 包实现。

```go
type ContextBuilder interface {
    Name() string
    BuildContext(ctx context.Context, sa *session.Session, input *ContextBuildInput) (*ContextBuildResult, error)
}
```

#### `Name() string`

返回构建器名称标识，用于日志和调试。

**返回值**：`string` — 构建器名称（如 `"hook-explore"`）

---

#### `BuildContext(ctx, sa, input) (*ContextBuildResult, error)`

构建子智能体执行上下文。在 `BaseAgent.Execute()` 中被调用，Hook 系统可通过此方法动态修改系统提示词、记忆索引和附加系统消息。

| 参数 | 类型 | 说明 |
|------|------|------|
| `ctx` | `context.Context` | 上下文，用于超时控制 |
| `sa` | `*session.Session` | 子智能体的 LLM 会话对象 |
| `input` | `*ContextBuildInput` | 上下文构建输入 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `result` | `*ContextBuildResult` | 构建结果，可为 `nil`（表示无自定义） |
| `err` | `error` | 构建失败时返回错误 |

**调用时机**：在 `Execute()` 方法中，于记忆索引构建之后、系统消息和用户消息注入之前调用。若返回错误或 `nil`，Hook 被跳过，不影响正常流程。

---

---

## 导出函数

### `New(cfg, providers, bus, toolFactory) SubAgent`

创建并返回一个新的子智能体实例。

```go
func New(cfg Config, providers *llm.ProviderRegistry, bus event.EventBus, toolFactory func(string) *tool.Tool) SubAgent
```

| 参数 | 类型 | 说明 |
|------|------|------|
| `cfg` | `Config` | 子智能体配置，定义类型、模型、系统提示词、工具列表等 |
| `providers` | `*llm.ProviderRegistry` | LLM 提供商注册表，用于根据 `cfg.ProviderName` 查找 Provider |
| `bus` | `event.EventBus` | 事件总线，用于发布状态变更、任务开始/完成/失败等事件 |
| `toolFactory` | `func(string) *tool.Tool` | 工具工厂函数，根据工具名创建工具实例。若为 `nil` 则不注册配置中的工具 |

| 返回值 | 说明 |
|--------|------|
| `SubAgent` | 新创建的子智能体实例（具体类型为 `*BaseAgent`） |

**初始化行为**：
1. 生成唯一 ID（格式：`"subagent-{Type}"`）。
2. 创建独立的 LLM 会话（`session.New`）。
3. 初始化工具注册表、权限检查器。
4. 设置默认值：`maxToolCallRounds=10`、`toolErrors 上限=3`、`contextLimit=65536`、`maxToolResultLen=4000`。
5. 通过 `toolFactory` 注册配置中指定的工具。
6. 注册内置的 `ask_architect` 工具（子智能体到主智能体双向通信）。

---

## BaseAgent 核心工作流

### 执行流程总览

```
Execute()
  ├─ 状态检查（拒绝并发执行）
  ├─ 从 DB 恢复历史会话（若配置了 wdb）
  ├─ 注入上下文摘要和任务消息
  ├─ 发起 LLM 流式请求
  └─ runToolLoop() goroutine
       ├─ prepareNextRound() — 上下文压缩判断
       ├─ processStream() — 处理流式响应，收集文本和工具调用
       │    ├─ StreamReasoning → 转发推理内容
       │    ├─ StreamToolCall → 收集工具调用
       │    ├─ StreamTextDelta → 转发文本增量
       │    ├─ StreamDone → 返回 streamResult
       │    └─ StreamError → 发布失败事件，返回错误
       ├─ 无工具调用 → 任务完成，发布完成事件，关闭通道
       ├─ executeToolCalls() — 执行工具调用
       │    ├─ ask_architect → 回调主智能体
       │    ├─ 权限检查（Allow/Deny/Ask）
       │    ├─ bash + guardReviewer → reviewWithGuard() 审查
       │    ├─ 调用 t.Call()
       │    └─ collectToolError() — 错误收集与自纠正
       └─ 构建下一轮请求，继续循环（直到无工具调用或达到轮次上限）
```

### `processStream` — 流式响应处理

处理 LLM 返回的流式事件，类型包括：

| 事件类型 | 处理方式 |
|----------|----------|
| `StreamReasoning` | 转发推理内容（以 `🧠>` 前缀标记），累积到 reasoningBuilder |
| `StreamTextDelta` | 转发文本增量，累积到最终内容 |
| `StreamToolCall` | 记录当前工具名，递增 `toolCount`，累积到 toolCalls 列表 |
| `StreamDone` | 将最终内容和工具调用写入 session，返回 `streamResult` |
| `StreamError` | 更新状态为 error，发布 `EventTaskFailed`，返回错误 |

若流通道异常关闭（未收到 `StreamDone` 事件），视为错误并返回。

### `executeToolCalls` — 工具调用执行

对每个 LLM 请求的工具调用：

1. **解析参数**：将 JSON 参数反序列化为 `map[string]any`。
2. **ask_architect 特殊处理**：若工具名为 `ask_architect`，通过 `askArchitectFn` 回调同步获取主智能体回复。
3. **权限检查**：调用 `PermissionChecker.Check(toolName, toolPath)`，返回 `Allow`/`Deny`/`Ask`。
   - `Deny`：拒绝执行，收集权限错误。
4. **guard 审查**：若工具名为 `bash` 且已设置 `guardReviewer`，向 `toolCtx` 注入 `GuardReviewer` 闭包。
5. **执行工具**：调用 `t.Call(toolCtx, args)`。
6. **错误收集**：若执行失败，通过 `toolErrors` 收集错误。连续错误达上限（默认 3 次）后停止自动纠正，生成反馈消息注入 session。
7. **轮次检查**：`toolCallRounds` 递增，超过 `maxToolCallRounds`（默认 10）则强制终止。

### `reviewWithGuard` — Guard 审查

bash 命令执行前的安全审查流程：

1. **缓存检查**：将命令（取前 200 字符）作为缓存键，通过 `guardLRUCache.get(key)` 查询。命中时调用 `MoveToFront` 将条目移至链表头部（标记为最近使用），直接返回缓存结果；未命中则继续审查流程。
2. **命令截断**：若命令超过 1000 字符，截断后再发送审查。
3. **构建审查任务**：优先使用 `embed.GetPrompt("guard_review")` 模板；若不可用则使用默认描述。
4. **调用 guard 子智能体**：通过 `GuardReviewer.Execute(ctx, "guard", taskDesc, contextSummary)` 发起，设置 60 秒超时。
5. **结果判定**：若审查结果中包含 `"level":"critical"` 或 `"level":"high"`，判定为不通过（`approved=false`）；否则放行。
6. **缓存结果**：审查结果通过 `guardLRUCache.set(key, value)` 写入缓存——若键已存在则更新值并 `MoveToFront`；若不存在则 `PushFront` 插入新条目。插入后若超过 `guardCacheMaxSize`（100），自动 `Remove(keys.Back())` 淘汰链表尾部（最久未使用）的条目。
7. **清理 guard 会话**：审查完成后通过 `inst.GetSession().Clear()` 方法清理 guard 会话，而非直接操作 `sess.Messages = sess.Messages[:0]`，防止消息无限累积。`Clear()` 方法已增强，现在同时清空 `MemoryIndex` 和 `Summary`，且线程安全。
8. **容错**：若 guard 子智能体不可用（`Execute` 返回错误），默认放行（`approved=true`）。

**缓存实现**：`BaseAgent.guardCache` 字段类型为 `*guardLRUCache`（替代旧有的 `map` + `sync.RWMutex` 组合）。该结构体使用 `container/list` 双向链表 + `map[string]*list.Element` 实现标准 LRU 策略：

- `keys *list.List`：双向链表，维护条目访问顺序（头部为最近使用，尾部为最久未使用）。
- `entries map[string]*list.Element`：键到链表节点的映射，提供 O(1) 查找。
- `get(key)`：`MoveToFront` 标记命中条目为最近使用。
- `set(key, value)`：`PushFront` 插入/更新条目；超过 `maxSize` 时 `Remove(Back)` + `delete(entries)` 淘汰最旧条目。
- 内置 `sync.Mutex`（`mu` 字段），所有操作线程安全，`BaseAgent` 无需外部加锁。

### `prepareNextRound` — 上下文压缩

在每轮工具调用后、下一轮 LLM 请求前执行：

- 调用 `compact.ShouldCompact` 检查是否超过 `contextLimit`。
- 若需要压缩（`decision.Needed=true`）：
  - `full` 级别：构建压缩结果并应用到 session。
  - 自动创建快照存档（若已配置 `wdb`）。
  - 裁剪过旧的工具输出。

### `runToolLoop` — 工具调用循环

驱动子智能体完成任务的完整循环：

1. 非首轮时调用 `prepareNextRound` 进行上下文压缩。
2. 调用 `processStream` 处理 LLM 流式响应（支持最多 1 次重试）。
3. 若无工具调用且流成功关闭 → 任务完成，标记 `status="completed"`，发布 `EventTaskCompleted`。
4. 若有工具调用 → 调用 `executeToolCalls` 执行。
5. 检查 `toolCallRounds` 是否超过上限，超限则终止。
6. 构建新一轮请求，继续循环。

---

## 内部辅助类型

### `streamResult`

`processStream` 的返回值类型（未导出）。

```go
type streamResult struct {
    content   string
    toolCalls []*llm.ToolCall
    reasoning string
}
```

| 字段 | 说明 |
|------|------|
| `content` | LLM 生成的文本内容 |
| `toolCalls` | LLM 请求执行的工具调用列表 |
| `reasoning` | LLM 的推理内容（思维链） |

### `guardReviewResult`

Guard 审查结果（未导出）。

```go
type guardReviewResult struct {
    approved bool
    reason   string
}
```

| 字段 | 说明 |
|------|------|
| `approved` | 命令是否通过审查 |
| `reason` | 审查理由 |

---

## 内部辅助函数

| 函数 | 签名 | 说明 |
|------|------|------|
| `publishAgentStatus` | `func (sa *BaseAgent) publishAgentStatus()` | 发布 `EventAgentStatusChanged` 事件到总线，供 TUI 订阅 |
| `collectToolError` | `func (sa *BaseAgent) collectToolError(responseCh chan<- string, category string, err error, context string) string` | 收集工具执行错误。若未超过错误上限，发送自纠正提示到 responseCh；超限则停止纠正 |
| `extractToolPath` | `func extractToolPath(toolName string, args map[string]any) string` | 从工具参数中提取文件路径或命令，供权限检查使用 |
| `convertToolCalls` | `func convertToolCalls(calls []*llm.ToolCall) []llm.ToolCall` | 将指针切片转换为值切片（避免并发问题） |
| `loadMessages` | `func (sa *BaseAgent) loadMessages(messages []*storage.MessageRow)` | 从存储层恢复子智能体对话历史到 session |
| `logError` | `func (sa *BaseAgent) logError(category, severity, message string)` | 将错误持久化到数据库 |
| `getStack` | `func getStack() string` | 获取当前 goroutine 的堆栈跟踪 |
| `truncateStr` | `func truncateStr(s string, maxLen int) string` | 按字符（rune）截断字符串到指定长度 |
| `truncateTask` | `func truncateTask(task string) string` | 截断任务描述到 60 字符（用于快照显示） |

---

## 事件发布

`BaseAgent` 在执行过程中通过事件总线发布以下事件：

| 事件 | 触发时机 | 携带数据 |
|------|----------|----------|
| `EventAgentStatusChanged` | 状态变更（idle/running/completed/error/reset） | `type`, `id`, `status`, `task`, `current_tool`, `tool_count`, `start_time`, `duration`, `error_msg` |
| `EventTaskStarted` | 任务开始执行 | `agent`, `id`, `task` |
| `EventTaskCompleted` | 任务成功完成 | `agent`, `id`, `task`, `result`, `duration`, `tools` |
| `EventTaskFailed` | 任务执行失败 | `agent`, `id`, `task`, `error` |
| `EventAgentToolStart` | LLM 请求发起工具调用 | `agent`, `id`, `tool` |

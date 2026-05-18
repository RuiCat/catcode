# agent/orchestrator — 主智能体编排器

## 包概述

`agent/orchestrator` 包实现了 CatCode 项目的核心编排逻辑。包内的源文件定义了 **Architect**（主智能体），它是整个系统的顶层协调者，负责：

1. **理解用户需求**：接收用户输入并驱动 LLM 对话循环。
2. **编排侧加载角色与子智能体**：通过事件总线和智能体池协调角色（Role）与子智能体（SubAgent）的执行。
3. **全局工作流协调**：通过 `EventBus` 发布和订阅事件，实现解耦的组件间通信。
4. **工具调用循环**：管理 LLM 发起的工具调用（tool call），支持多轮迭代、重试、上下文压缩和错误自纠正。
5. **记忆索引注入**：通过统一的记忆服务（`MemoryService`）为 LLM 提供上下文感知的记忆索引。

---

## 文件拆分

| 文件 | 说明 |
|------|------|
| `architect.go` | Architect 核心：结构体定义、构造器 `NewArchitect`、接口方法（ProcessInput/BuildSubAgentContext/GetSession/RegisterTool/SetWorkDir/SetPlanEngine/InjectMemoryIndex/LoadHistory/SetSystemPrompt）、事件回调（onRoleResult/onSubAgentResult） |
| `architect_stream.go` | 流式响应处理：`processStream`、`runToolLoop`、`prepareNextRound`、上下文压缩 |
| `architect_tools.go` | 工具调用执行：`executeToolCalls`、权限检查、子智能体委派（dispatchSubAgent）、错误收集（collectToolError/handleError）、计划模式限制（isRestrictedInPlanMode） |
| `architect_subagent.go` | 子智能体管理：`dispatchSubAgent`（同步等待子智能体完成，10分钟空闲超时）、`handleSubAgentCompletion` |
| `architect_context.go` | 上下文构建：`buildSubAgentContext`（内部）、`injectMemoryIndex`、指令文件加载 |

## 依赖关系

本包依赖以下内部包：

| 包路径 | 用途 |
|--------|------|
| `catcode/agent/plan` | 规划引擎接口（`PlanEngineInterface`），支持计划模式（plan_enter/plan_exit） |
| `catcode/agent/pool` | 子智能体池接口（`PoolInterface`），管理子智能体的创建与执行 |
| `catcode/agent/role` | 角色注册表接口（`RegistryInterface`），管理侧加载角色定义 |
| `catcode/ai/compact` | 上下文压缩模块，提供 token 估算、压缩决策和压缩执行 |
| `catcode/ai/llm` | LLM Provider 接口（`Provider`）及流式事件、工具调用等类型定义 |
| `catcode/ai/session` | 会话管理（`Session`），封装消息列表、工具注册、请求构建 |
| `catcode/core/errors` | 统一错误处理（`ErrorCollector`、`CatError`），支持错误收集与自纠正 |
| `catcode/core/event` | 事件总线（`EventBus`），发布/订阅架构的核心 |
| `catcode/data/embed` | 嵌入资源读取，用于加载 `architect.yaml` 配置 |
| `catcode/data/storage` | 工作区数据库（`WorkspaceDB`）和记忆服务（`MemoryService`） |
| `catcode/tool` | 工具定义（`Tool`）、权限检查（`PermissionChecker`）和执行上下文 |

**外部依赖**：`context`、`encoding/json`、`fmt`、`runtime`、`strings`、`time`（均为 Go 标准库）。

---

## 常量

### `maxToolCallRounds`（未导出）

```go
const maxToolCallRounds = 20
```

单次用户请求中允许的最大工具调用轮次。当 LLM 连续调用工具超过此次数时，强制终止并通知 LLM 基于已有结果给出最终回答，防止无限递归。

---

## 导出类型

### `StreamResult` — 流处理结果

```go
type StreamResult struct {
    Content   string          // 累积的文本内容
    ToolCalls []*llm.ToolCall // LLM 请求的工具调用列表
    Reasoning string          // thinking 推理内容
}
```

`processStream` 方法的返回值，封装了一次 LLM 流式响应的完整处理结果。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Content` | `string` | LLM 输出的累积文本内容（不含推理） |
| `ToolCalls` | `[]*llm.ToolCall` | LLM 请求执行的工具调用列表，为 `nil` 或空切片时表示纯文本回复 |
| `Reasoning` | `string` | 当 LLM 处于 thinking/推理模式时，累积的推理链文本 |

---

### `Config` — Architect 配置

```go
type Config struct {
    Model        string  // LLM 模型标识符
    SystemPrompt string  // 系统提示词
    Temperature  float64 // LLM 采样温度
    ContextLimit int     // 上下文窗口大小（从角色 model.limit.context 读取）
    MaxOutput    int     // 最大输出 token（从角色 model.limit.output 读取）
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Model` | `string` | 模型标识符，格式如 `"deepseek:deepseek-chat"`。默认值由嵌入的 `architect.yaml` 提供 |
| `SystemPrompt` | `string` | 系统提示词，定义主智能体的行为准则和能力边界 |
| `Temperature` | `float64` | LLM 采样温度（0.0~1.0）。默认 `0.3`，偏向确定性输出 |
| `ContextLimit` | `int` | 上下文窗口大小（总 token 数上限）。用于触发自动压缩（阈值为此值的 50%）。默认 `65536` |
| `MaxOutput` | `int` | 单次 LLM 回复的最大输出 token 数。默认 `8192` |

---

### `ArchitectInterface` — 主智能体编排接口

```go
type ArchitectInterface interface {
    ProcessInput(ctx context.Context, userInput string) (<-chan string, error)
    BuildSubAgentContext(taskDesc, subagentType string) string
    GetSession() *session.Session
    RegisterTool(t *tool.Tool) error
    SetWorkDir(dir string)
    SetPlanEngine(pe plan.PlanEngineInterface)
    InjectMemoryIndex()
    LoadHistory(messages []*storage.MessageRow)
}
```

定义了主智能体的公共行为契约，所有对外交互均通过此接口进行。

**方法列表：**

| 方法 | 说明 |
|------|------|
| `ProcessInput` | 处理用户输入，驱动 LLM 对话与工具调用循环 |
| `BuildSubAgentContext` | 为子智能体构建完整的上下文摘要 |
| `GetSession` | 获取底层会话对象，供外部读取/调试 |
| `RegisterTool` | 向主会话注册工具定义 |
| `SetWorkDir` | 设置工作区路径并加载指令文件 |
| `SetPlanEngine` | 注入规划引擎引用，启用计划模式支持 |
| `InjectMemoryIndex` | 注入记忆索引到会话（供会话恢复后调用） |
| `LoadHistory` | 从存储层消息行恢复对话历史 |

---

### `Architect` — 主智能体结构体

```go
type Architect struct {
    // 所有字段均为未导出
}
```

`Architect` 是主智能体的具体实现，实现了 `ArchitectInterface` 接口。其内部包含：

- **LLM 交互**：`provider`（LLM Provider）、`mainSession`（主会话）
- **角色与智能体**：`roleReg`（角色注册表）、`agentPool`（子智能体池）
- **事件系统**：`bus`（事件总线）
- **存储与记忆**：`wdb`（工作区数据库）、`memoryService`（记忆服务）
- **规划引擎**：`planEngine`（可选，启用计划模式）
- **错误处理**：`toolErrors`（错误收集器，最多 3 次自纠正）
- **配置缓存**：`config`、`originalSystemPrompt`、`instructions`、`workDir`
- **运行时状态**：`planStatus`、`toolCallRounds`、`maxToolResultLen`

> 注意：`Architect` 结构体本身为导出类型（可被外部引用），但其所有字段均为未导出（小写开头），外部只能通过接口方法访问其行为。

---

## 导出函数

### `DefaultArchitectConfig()`

```go
func DefaultArchitectConfig() *Config
```

返回一组默认的 `Config` 实例。

**功能描述**：
1. 以硬编码默认值初始化配置（模型 `deepseek:deepseek-chat`，温度 `0.3`，上下文 `65536`，最大输出 `8192`）。
2. 尝试从嵌入资源 `architect.yaml` 中读取覆盖配置：`SystemPrompt`、`ModelName`、`Temperature`、`ContextLimit`、`OutputLimit`。若嵌入资源可读且对应字段非零值，则覆盖默认值。

**返回值**：
- `*Config`：初始化完成的配置指针。

**注意事项**：
- 该函数依赖 `embed.GetAgentPrompt("architect")`，若嵌入资源不可用，则回退到硬编码默认值。
- 通常在 `NewArchitect` 中当 `cfg` 为 `nil` 时自动调用。

---

### `NewArchitect()`

```go
func NewArchitect(
    cfg *Config,
    provider llm.Provider,
    roleReg role.RegistryInterface,
    bus event.EventBus,
    agentPool agent.PoolInterface,
    wdb storage.WorkspaceDB,
    memoryService storage.MemoryService,
) ArchitectInterface
```

创建主智能体实例。

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `cfg` | `*Config` | Architect 配置。传入 `nil` 则自动调用 `DefaultArchitectConfig()` |
| `provider` | `llm.Provider` | LLM Provider 实例，负责实际的模型调用（流式/同步） |
| `roleReg` | `role.RegistryInterface` | 角色注册表，管理侧加载角色定义及权限 |
| `bus` | `event.EventBus` | 事件总线，用于发布/订阅编排相关事件 |
| `agentPool` | `agent.PoolInterface` | 子智能体池，管理子智能体的生命周期和执行 |
| `wdb` | `storage.WorkspaceDB` | 工作区数据库，用于持久化快照、日志和配置 |
| `memoryService` | `storage.MemoryService` | 记忆服务，提供智能记忆索引构建 |

**返回值**：
- `ArchitectInterface`：新创建的 Architect 实例（以接口形式返回）。

**功能描述（初始化步骤）**：
1. 若 `cfg` 为 `nil`，调用 `DefaultArchitectConfig()` 获取默认配置。
2. 创建主会话 `mainSession`（通过 `session.New("architect-main", ...)`）。
3. 从 `wdb` 读取 `tool.max_result_length` 配置，设置工具结果截断长度。
4. 若 `memoryService` 非空，注入智能记忆选择器 `compact.SelectRelevantMemories`。
5. 根据 `cfg.ContextLimit` 设置会话压缩阈值（`CompressThreshold = ContextLimit * 50%`）。
6. 订阅两个事件：
   - `event.EventRoleResult` → `onRoleResult`：角色执行结果回调。
   - `event.EventSubAgentResult` → `onSubAgentResult`：子智能体异步完成回调。

**注意事项**：
- 返回的是 `ArchitectInterface` 接口，而非具体 `*Architect` 指针。外部代码应通过接口方法操作。
- `wdb` 可以为 `nil`（此时跳过数据库相关的配置读取和日志记录）。
- `memoryService` 可以为 `nil`（此时记忆索引功能不生效）。

---

## 导出方法（`*Architect` 上）

### `ProcessInput()`

```go
func (a *Architect) ProcessInput(ctx context.Context, userInput string) (<-chan string, error)
```

处理用户输入，是主智能体最核心的入口方法。驱动整个 LLM 对话与工具调用循环。

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `ctx` | `context.Context` | 上下文，用于取消和超时控制 |
| `userInput` | `string` | 用户的原始输入文本 |

**返回值**：
- `<-chan string`：流式响应通道。调用方通过遍历此通道获取 LLM 逐步生成的文本（包括工具调用提示、思考过程、最终回复等）。
- `error`：仅当构建初始请求或发起 LLM 调用失败时返回。流式处理过程中的错误通过 `responseCh` 通道以文本形式返回。

**功能描述**：
1. 将用户消息添加到主会话。
2. 发布 `EventUserRequestReceived` 事件。
3. 清理上次可能遗留的孤儿 `tool_calls`。
4. 注入记忆索引到 SystemPrompt。
5. 构建 LLM 请求并通过 `provider.Chat` 发起流式调用。
6. 在后台 goroutine 中调用 `runToolLoop`，处理流式响应、执行工具调用、管理多轮迭代和错误重试。

**注意事项**：
- 返回的 channel 会在内部 goroutine 完成时被关闭，调用方应使用 `for range` 遍历。
- 方法内部使用 `recover()` 捕获 panic，防止 goroutine 崩溃导致 channel 永远不关闭。
- 每次调用会自动重置错误收集器和工具调用轮次计数器。

---

### `BuildSubAgentContext()`

```go
func (a *Architect) BuildSubAgentContext(taskDesc, subagentType string) string
```

为子智能体构建完整的上下文摘要，供 `@` 命令等外部调用方使用。

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `taskDesc` | `string` | 子智能体需要执行的任务描述 |
| `subagentType` | `string` | 子智能体类型标识符（如 `"code-explorer"`） |

**返回值**：
- `string`：构建好的上下文字符串，包含以下层次（按顺序）：
  1. **[主会话上下文]**：最近的主会话用户/助手消息摘要（最多 2000 字符）
  2. **[记忆索引]**：当前会话的记忆索引（最多 1500 字符后截断）
  3. **[指令文件]**：工作区指令文件格式化内容（最多 4000 字符）
  4. **[环境信息]**：工作目录、子智能体类型、主会话模型等

**注意事项**：
- 该方法是内部 `buildSubAgentContext` 的公开包装，两者逻辑完全相同。
- 上下文构建会从主会话消息列表从后往前提取最近的用户和助手消息。

---

### `GetSession()`

```go
func (a *Architect) GetSession() *session.Session
```

返回主会话对象。

**返回值**：
- `*session.Session`：主智能体的会话实例。外部可通过此对象读取消息历史、工具列表、压缩状态等。

**注意事项**：
- 返回的是内部 `mainSession` 的直接指针，外部修改会影响主智能体行为，操作需谨慎。

---

### `RegisterTool()`

```go
func (a *Architect) RegisterTool(t *tool.Tool) error
```

向主会话注册一个新工具。

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `t` | `*tool.Tool` | 工具定义，包含名称、描述、参数 Schema、回调函数等 |

**返回值**：
- `error`：注册失败时返回错误（如工具名冲突）。

**注意事项**：
- 工具注册会直接影响 LLM 的函数调用能力。注册后的工具会出现在 LLM 请求的 `tools` 字段中。
- 实际调用委托给 `a.mainSession.AddTool(t)`。

---

### `SetWorkDir()`

```go
func (a *Architect) SetWorkDir(dir string)
```

设置工作区路径并加载指令文件。

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `dir` | `string` | 工作区目录的绝对路径 |

**功能描述**：
1. 保存工作区路径到 `a.workDir`。
2. 调用 `storage.LoadInstructions(dir)` 加载目录下的指令文件（如 `AGENTS.md`、`RULES.md` 等）。
3. 若指令文件非空，将其格式化内容（最多 8000 字符）注入到 `mainSession.InstructionsContent`。

**注意事项**：
- 如果目录中不存在指令文件，`a.instructions` 将为 `nil` 或 `IsEmpty()` 为 `true`，不影响正常功能。**无返回值**。

---

### `SetPlanEngine()`

```go
func (a *Architect) SetPlanEngine(pe plan.PlanEngineInterface)
```

设置规划引擎引用，用于支持计划模式（`plan_enter` / `plan_exit`）。

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `pe` | `plan.PlanEngineInterface` | 规划引擎实例。传入 `nil` 可禁用计划模式 |

**功能描述**：
- 当规划引擎启用且处于计划模式时，工具执行循环会调用 `isRestrictedInPlanMode`，该函数基于包级变量 `planModeAllowedTools`（`map[string]bool`）判断工具是否被禁止（不在白名单或 `false` 则禁止），防止在规划阶段执行破坏性操作。

**注意事项**：
- 若未调用此方法（`planEngine` 为 `nil`），所有工具均正常可用，无计划模式限制。

---

### `InjectMemoryIndex()`

```go
func (a *Architect) InjectMemoryIndex()
```

公开的记忆索引注入方法，供会话恢复后手动调用。

**功能描述**：
- 内部委托给 `injectMemoryIndex()`。从最近消息中提取上下文提示，通过 `memoryService.BuildIndex` 构建记忆索引字符串，并存入 `mainSession.MemoryIndex`。

**注意事项**：
- 通常在 `LoadHistory` 恢复对话历史后调用，使恢复的会话立即获得上下文相关的记忆索引。
- 若 `memoryService` 为 `nil`，此方法为空操作。**无返回值**。

---

### `LoadHistory()`

```go
func (a *Architect) LoadHistory(messages []*storage.MessageRow)
```

从存储层的消息行恢复对话历史到主会话。

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `messages` | `[]*storage.MessageRow` | 存储层的消息行列表，通常从数据库查询得到 |

**功能描述**：
1. 清空 `mainSession.Messages`。
2. 逐条将 `MessageRow` 转换为 `session.Message`：
   - `ToolCallsJSON` → 反序列化为 `[]llm.ToolCall`
   - `Enabled` 字段映射到 `Enable`
   - `ReasoningContent` 保留
3. 对每条消息调用 `msg.Update()` 更新内部 token 计数。

**注意事项**：
- 仅替换消息列表，**不修改**已注册的工具集合。**无返回值**。
- 通常在应用启动时从数据库恢复上次会话状态时调用。

---

### `SetSystemPrompt()`

```go
func (a *Architect) SetSystemPrompt(prompt string)
```

更新系统提示词（同时更新原始缓存）。

**参数说明**：

| 参数 | 类型 | 说明 |
|------|------|------|
| `prompt` | `string` | 新的系统提示词文本 |

**功能描述**：
- 同时更新 `mainSession.SystemPrompt`（运行时使用的提示词）和 `originalSystemPrompt`（原始备份，用于记忆索引注入时的回退基准）。

**注意事项**：
- 外部通常不需要直接调用此方法，除非需要动态替换系统提示词。正常流程中提示词由 `DefaultArchitectConfig` 从嵌入的 `architect.yaml` 加载。**无返回值**。

---

## 内部方法（关键未导出方法说明）

以下方法为未导出（小写开头），但对理解架构至关重要：

### `runToolLoop`
驱动工具调用迭代循环。使用 `for` 循环（非递归）替代原递归模式，消除递归栈累积。每轮迭代包括：上下文准备 → LLM 流式响应处理（含最多 2 次重试）→ 工具调用执行 → 下一轮请求构建。

### `processStream`
处理 LLM 流式响应事件。解析 `StreamTextDelta`（文本增量）、`StreamReasoning`（推理内容增量）、`StreamToolCall`（工具调用）、`StreamDone`（流结束）、`StreamError`（错误）五种事件类型。

### `executeToolCalls`
执行 LLM 请求的工具调用。包括：参数解析、计划模式限制检查、子智能体委派（`task` 工具）、角色调度（未知工具回退）、本地工具执行与权限检查、错误收集与延迟注入。

### `dispatchSubAgent`
分发子智能体任务并同步等待结果。设置 10 分钟空闲超时，使用 `idleTimer` 在每次收到输出时重置。

### `prepareNextRound`
准备下一轮工具调用迭代前的上下文：上下文压缩（全量/微压缩）、孤儿 tool_call 清理、记忆索引注入。

### `injectMemoryIndex`
从最近消息提取上下文提示，调用 `memoryService.BuildIndex` 构建记忆索引，注入到 `mainSession.MemoryIndex`。

### `checkPermission`
从主角色定义的权限映射中检查工具权限等级（Allow/Ask/Deny）。

### `handleError` / `collectToolError`
统一错误处理入口。将错误通过 `ErrorCollector` 收集，达到上限（3 次）后停止自纠正。`collectToolError` 为延迟注入版本，返回错误描述字符串而非直接写入会话。

### `planModeAllowedTools`（包级变量）

`map[string]bool` 类型的包级变量，定义计划模式下工具白名单。值为 `true` 表示允许，`false` 表示明确禁止，不在 map 中则默认禁止。按功能分为五类：

| 分类 | 工具 | 说明 |
|------|------|------|
| 信息获取类 | `read`、`glob`、`grep`、`webfetch` | 只读信息收集 |
| 计划管理类 | `skill`、`plan_enter`、`plan_exit`、`todo` | 计划模式自身管理 |
| 交互类 | `question`、`send_message`、`companion_talk` | 与用户交互 |
| 架构类 | `ask_architect`、`log_issue` | 架构相关操作 |
| 明确禁止 | `task` | 子智能体委派，禁止以防止绕过只读限制 |

> 新增工具时需在此变量中评估是否允许在计划模式下使用。

### `isRestrictedInPlanMode`

判断工具在计划模式下是否被禁止（`true`=禁止）。内部逻辑：`return !planModeAllowedTools[toolName]`，即查询上述包级白名单变量，不在白名单或值为 `false` 均返回 `true`（禁止）。

---

### 文件信息

- `agent/orchestrator/architect.go`
- `agent/orchestrator/architect_stream.go`
- `agent/orchestrator/architect_tools.go`
- `agent/orchestrator/architect_subagent.go`
- `agent/orchestrator/architect_context.go`

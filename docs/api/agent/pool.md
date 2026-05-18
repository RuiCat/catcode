# agent/pool — 子智能体并发执行池

## 包概述

`agent/pool` 包实现了子智能体（SubAgent）的并发执行池，借鉴 opencat 的 WorkerPool + SafeGo 设计。每个子智能体拥有独立的 LLM 会话、权限集和工具集，通过全局信号量控制并发数量，支持同步阻塞和异步事件两种执行模式。

该包作为子智能体的生命周期管理者，负责实例的创建、复用、状态追踪和优雅关闭，同时提供从数据库或嵌入式配置读取子智能体定义的能力。

## 并发控制机制

池通过以下机制实现并发控制：

1. **信号量（Semaphore）** — `Pool.semaphore` 是一个带缓冲的 channel，容量由 `PoolConfig.MaxConcurrent` 决定。每次执行任务前必须获取信号量，完成后释放。
2. **超时保护** — `Execute` 在等待信号量时设置 120 秒超时；`ExecuteAsync` 将结果通过 EventBus 异步推送，避免调用方阻塞。
3. **Panic 恢复** — 所有 goroutine 内部均包含 `recover` 逻辑，捕获 panic 后通过 EventBus 广播错误事件或写入 `resultCh`，并持久化错误日志。
4. **实例复用** — `GetOrCreate` 优先返回空闲（`IsBusy() == false`）的已有实例，避免重复创建和 LLM 会话浪费。
5. **读写锁** — `sync.RWMutex` 保护 `agents` 映射的并发访问。

---

## 类型

### `PoolInterface`

子智能体池的公共接口，定义了池对外暴露的所有行为。

```go
type PoolInterface interface {
    Execute(ctx context.Context, agentType, task, contextSummary string) (<-chan string, error)
    ExecuteAsync(ctx context.Context, agentType, task, contextSummary string)
    GetOrCreate(agentType string) (subagent.SubAgent, error)
    Snapshot() []subagent.AgentSnapshot
    ActiveCount() int
    TotalCount() int
    Shutdown()
    SetWorkspaceDB(wdb storage.WorkspaceDB, contextLimit int)
    SetMemoryService(ms storage.MemoryService)
    SetGuardReviewer()
    GetAll(agentType string) ([]subagent.SubAgent, bool)
    GetAllAgents() []subagent.SubAgent
}
```

**方法列表：**

| 方法 | 描述 |
|------|------|
| `Execute` | 同步提交任务，阻塞等待信号量，返回结果 channel |
| `ExecuteAsync` | 异步提交任务，通过 EventBus 返回结果 |
| `GetOrCreate` | 获取或创建指定类型的子智能体实例 |
| `Snapshot` | 获取所有子智能体的状态快照 |
| `ActiveCount` | 返回当前活跃（正在执行）的子智能体数量 |
| `TotalCount` | 返回池中子智能体总数 |
| `Shutdown` | 关闭池，清空所有子智能体 |
| `SetWorkspaceDB` | 设置数据库引用并应用于所有子智能体 |
| `SetMemoryService` | 设置记忆服务并传播给所有子智能体 |
| `SetGuardReviewer` | 为所有子智能体设置 guard 审查器 |
| `GetAll` | 获取指定类型的所有子智能体实例 |
| `GetAllAgents` | 获取所有子智能体实例（所有类型） |

---

### `Pool`

子智能体池的具体实现。

```go
type Pool struct {
    // 未导出字段
}
```

**字段（不导出）：**

| 字段 | 类型 | 描述 |
|------|------|------|
| `agents` | `map[string][]subagent.SubAgent` | 按类型分组的子智能体实例 |
| `providers` | `*llm.ProviderRegistry` | 多 Provider 注册表 |
| `bus` | `event.EventBus` | 事件总线 |
| `configs` | `map[string]subagent.Config` | 类型到配置的映射 |
| `toolFactory` | `func(string) *tool.Tool` | 工具工厂函数 |
| `semaphore` | `chan struct{}` | 全局信号量，控制最大并发数 |
| `maxConcurrent` | `int` | 最大并发子智能体数 |
| `wdb` | `storage.WorkspaceDB` | 数据库引用 |
| `contextLimit` | `int` | 默认上下文限制 |
| `memoryService` | `storage.MemoryService` | 记忆服务 |
| `mu` | `sync.RWMutex` | 读写锁 |

---

### `PoolConfig`

创建子智能体池的配置结构体。

```go
type PoolConfig struct {
    MaxConcurrent int                        // 最大并发子智能体数
    AgentConfigs  map[string]subagent.Config // 类型 → 配置
    ToolFactory   func(string) *tool.Tool    // 工具工厂（可选）
}
```

**字段说明：**

| 字段 | 类型 | 描述 |
|------|------|------|
| `MaxConcurrent` | `int` | 最大并发子智能体数。若 ≤ 0，`NewPool` 自动设为 3 |
| `AgentConfigs` | `map[string]subagent.Config` | 子智能体类型到配置的映射，key 为类型名（如 "explore", "plan"） |
| `ToolFactory` | `func(string) *tool.Tool` | 可选工具工厂函数，用于为子智能体动态创建工具 |

**注意事项：**
- `ToolFactory` 可以为 `nil`，子智能体将使用配置中的静态工具列表。
- `AgentConfigs` 中的 key 即为后续 `Execute`/`GetOrCreate` 中使用的 `agentType`。

---

### `subagent.Config`

子智能体配置（定义于 `agent/subagent` 包）。

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

### `subagent.AgentSnapshot`

子智能体状态快照（定义于 `agent/subagent` 包）。

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

---

## 构造函数

### `NewPool`

```go
func NewPool(cfg PoolConfig, providers *llm.ProviderRegistry, bus event.EventBus) PoolInterface
```

创建并返回一个新的子智能体池实例。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `cfg` | `PoolConfig` | 池配置，包括最大并发数和各类型子智能体配置 |
| `providers` | `*llm.ProviderRegistry` | LLM Provider 注册表，为子智能体提供模型访问 |
| `bus` | `event.EventBus` | 事件总线，用于发布子智能体结果和错误事件 |

**返回值：** `PoolInterface` — 池的接口实例。

**功能描述：**
- 初始化 `agents` 映射、信号量 channel 及其他内部字段。
- 若 `cfg.MaxConcurrent <= 0`，自动设置为 3。

**注意事项：**
- 返回的是 `PoolInterface` 接口而非 `*Pool`，调用方仅能使用接口定义的方法。

---

## 方法

### `SetGuardReviewer`

```go
func (p *Pool) SetGuardReviewer()
```

为池中所有现有子智能体设置 guard 审查器（将 `Pool` 自身作为 `GuardReviewer` 传入）。

**功能描述：**
遍历所有类型的所有实例，调用每个实例的 `SetGuardReviewer(p)`，使 guard 子智能体可以回调池来执行审查逻辑。

**注意事项：**
- 该方法使用读锁（`RLock`），因此与写操作可并发执行。
- 仅在已有实例上设置；后续 `GetOrCreate` 创建的新实例会在创建时自动设置。

---

### `SetWorkspaceDB`

```go
func (p *Pool) SetWorkspaceDB(wdb storage.WorkspaceDB, contextLimit int)
```

设置数据库引用和上下文限制。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `wdb` | `storage.WorkspaceDB` | 工作区数据库引用 |
| `contextLimit` | `int` | 默认上下文 token 限制 |

**功能描述：**
将 `wdb` 和 `contextLimit` 存储到 Pool 内部。后续通过 `GetOrCreate` 创建的新实例会自动继承这些设置。

**注意事项：**
- 该方法仅存储引用，不会主动传播到已有实例。已存在的实例需通过 `GetOrCreate` 重新获取时才会应用。
- 区别于 `SetMemoryService`，此方法不持有锁，调用方需自行保证并发安全。

---

### `SetMemoryService`

```go
func (p *Pool) SetMemoryService(ms storage.MemoryService)
```

设置记忆服务并立即传播给所有已有子智能体实例。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `ms` | `storage.MemoryService` | 记忆服务实例 |

**功能描述：**
- 存储 `ms` 到 Pool 内部字段。
- 遍历所有已有子智能体实例，调用 `SetMemoryService(ms)` 进行传播。
- 后续新创建的实例也会自动获得该服务。

**注意事项：**
- 该方法持有写锁（`Lock`），会阻塞其他写操作。

---

### `GetOrCreate`

```go
func (p *Pool) GetOrCreate(agentType string) (subagent.SubAgent, error)
```

获取或创建指定类型的子智能体实例。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `agentType` | `string` | 子智能体类型名（如 "explore", "plan"） |

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `subagent.SubAgent` | 接口 | 获取或新创建的子智能体实例 |
| `error` | error | 若类型未知则返回错误 |

**功能描述：**
1. 查找 `p.configs` 中是否存在该类型的配置，若无则返回错误。
2. 遍历该类型的已有实例，返回第一个空闲（`IsBusy() == false`）的实例。
3. 若无空闲实例，则创建新实例，自动注入 `WorkspaceDB`、`ContextLimit`、`MemoryService` 和 `GuardReviewer`，并加入池中。

**注意事项：**
- 该方法持有写锁（`Lock`），并发调用会串行化。
- 实例复用可减少 LLM 会话创建开销，但需确保 `IsBusy()` 状态在任务完成后正确重置。

---

### `Execute`

```go
func (p *Pool) Execute(ctx context.Context, agentType, task, contextSummary string) (<-chan string, error)
```

同步提交任务到子智能体池，阻塞等待信号量可用后执行。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `ctx` | `context.Context` | 上下文，用于取消等待和传递截止时间 |
| `agentType` | `string` | 子智能体类型名 |
| `task` | `string` | 要执行的任务描述 |
| `contextSummary` | `string` | 上下文摘要（可为空字符串） |

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `<-chan string` | 只读字符串 channel | 结果流，容量为 32 |
| `error` | error | 信号量获取失败、类型未知或执行错误 |

**功能描述：**
1. 尝试获取全局信号量，期间响应 `ctx.Done()` 和 120 秒超时。
2. 调用 `GetOrCreate` 获取子智能体实例。
3. 启动 goroutine 包装执行：调用 `inst.Execute`，将结果转发到 `resultCh`。
4. goroutine 通过 `defer` 确保信号量释放和 channel 关闭，并包含 panic 恢复。

**注意事项：**
- **必须消费 `resultCh` 直到关闭**，否则会导致 goroutine 泄漏（写入阻塞）和信号量永久占用。
- 返回的 channel 容量为 32，允许子智能体在消费方读取前缓冲一定量输出。
- 若信号量获取失败（超时/取消），返回错误且不占用槽位。
- 若 `GetOrCreate` 失败，信号量会被立即释放。

---

### `ExecuteAsync`

```go
func (p *Pool) ExecuteAsync(ctx context.Context, agentType, task, contextSummary string)
```

异步提交任务，通过 EventBus 返回结果。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `ctx` | `context.Context` | 上下文 |
| `agentType` | `string` | 子智能体类型名 |
| `task` | `string` | 任务描述 |
| `contextSummary` | `string` | 上下文摘要 |

**返回值：** 无 — 结果通过 EventBus 异步推送。

**功能描述：**
1. 启动 goroutine 调用 `p.Execute`。
2. 若执行出错，发布 `event.EventSubAgentError` 事件。
3. 若执行成功，收集所有输出文本，发布 `event.EventSubAgentResult` 事件。
4. goroutine 包含 panic 恢复，panic 时发布 `event.EventSubAgentError` 事件。

**事件格式：**

`EventSubAgentResult` 的事件数据：
```go
map[string]any{
    "type":   agentType,
    "result": result,   // 完整输出文本
    "task":   task,
}
```

`EventSubAgentError` 的事件数据：
```go
map[string]any{
    "type":  agentType,
    "error": err.Error(),
}
```

**注意事项：**
- 调用方需订阅 EventBus 的 `subagent.result` 和 `subagent.error` 事件来接收结果。
- 由于内部调用 `Execute`，同样受信号量控制。

---

### `ActiveCount`

```go
func (p *Pool) ActiveCount() int
```

返回当前活跃（正在执行）的子智能体数量。

**返回值：** `int` — 活跃实例数。

**功能描述：**
遍历所有类型的所有实例，统计 `IsBusy() == true` 的数量。

**注意事项：**
- 该方法持有读锁（`RLock`），结果反映调用时刻的快照，不保证实时精确。

---

### `TotalCount`

```go
func (p *Pool) TotalCount() int
```

返回池中子智能体实例总数（所有类型，包含空闲和忙碌）。

**返回值：** `int` — 实例总数。

**功能描述：**
遍历所有类型，累加每个类型的实例数量。

**注意事项：**
- 该方法持有读锁（`RLock`）。
- 实例总数会随 `GetOrCreate` 调用的增加而单调增长（不会被 `Shutdown` 之外的途径减少）。

---

### `GetAll`

```go
func (p *Pool) GetAll(agentType string) ([]subagent.SubAgent, bool)
```

获取指定类型的所有子智能体实例（包含空闲和忙碌）。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `agentType` | `string` | 子智能体类型名 |

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `[]subagent.SubAgent` | 切片 | 该类型的所有实例 |
| `bool` | bool | 该类型是否存在 |

**注意事项：**
- 返回的切片是 `agents` 映射中该类型切片的直接引用（非拷贝），外部修改切片元素属性可能影响池内部状态。
- 该方法持有读锁（`RLock`）。

---

### `GetAllAgents`

```go
func (p *Pool) GetAllAgents() []subagent.SubAgent
```

获取所有子智能体实例（所有类型，包含空闲和忙碌）。

**返回值：** `[]subagent.SubAgent` — 新分配的切片，包含所有实例。

**功能描述：**
遍历所有类型，将所有实例追加到一个新切片中返回。

**注意事项：**
- 返回的是新切片（不同于 `GetAll` 直接返回内部引用），但切片中的指针仍指向原实例。
- 该方法持有读锁（`RLock`）。

---

### `Snapshot`

```go
func (p *Pool) Snapshot() []subagent.AgentSnapshot
```

获取所有子智能体的状态快照。

**返回值：** `[]subagent.AgentSnapshot` — 所有实例的状态快照切片。

**功能描述：**
调用 `GetAllAgents()` 获取所有实例，然后对每个实例调用 `Snapshot()` 收集状态信息（名称、ID、状态、任务、耗时等）。

**注意事项：**
- 快照包含 `Status` 字段，常见值为 `"running"`、`"completed"`、`"idle"` 等。
- 内部调用 `GetAllAgents()`，因此快照反映调用时刻的状态。

---

### `ResetAllSessions`

```go
func (p *Pool) ResetAllSessions()
```

重置所有子智能体的 LLM 会话。

**功能描述：**
遍历所有类型的所有实例，调用每个实例的 `ResetSession()`。

**注意事项：**
- 该方法持有读锁（`RLock`）。
- 通常用于会话切换或上下文清理场景。

---

### `Shutdown`

```go
func (p *Pool) Shutdown()
```

关闭池，清空所有子智能体实例。

**功能描述：**
持有写锁（`Lock`），将 `p.agents` 重新初始化为空映射，丢弃所有现有实例的引用。

**注意事项：**
- 该方法不会等待正在执行的任务完成，也不会主动取消正在进行的 LLM 调用。
- 丢弃的实例将由 Go GC 回收（前提是外部无其他引用）。
- 调用后池可继续使用（通过 `GetOrCreate` 创建新实例）。

---

## 配置函数

### `AgentConfigsFromDB`

```go
func AgentConfigsFromDB(wdb storage.WorkspaceDB) map[string]subagent.Config
```

从数据库的 `agent_definitions` 表构建子智能体配置映射。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `wdb` | `storage.WorkspaceDB` | 工作区数据库引用 |

**返回值：** `map[string]subagent.Config` — 类型名到配置的映射。

**功能描述：**
1. 调用 `wdb.GetAllAgentDefinitions()` 获取所有智能体定义。
2. 过滤出 `mode == "subagent"` 且 `enabled == true` 的记录。
3. 解析各字段（工具列表、权限规则、输出限制等）构建 `subagent.Config`。
4. 对工具列表为空的配置，回退到 `DefaultAgentConfigs()` 中的默认工具列表。
5. 若数据库查询失败，回退到 `DefaultAgentConfigs()`。

**注意事项：**
- 若数据库不可用或查询出错，自动回退到嵌入式默认配置，不会返回错误。
- 工具列表的 JSON 反序列化失败时静默处理（使用空列表）。

---

### `DefaultAgentConfigs`

```go
func DefaultAgentConfigs() map[string]subagent.Config
```

返回从嵌入式 YAML 角色文件读取的默认子智能体配置。

**返回值：** `map[string]subagent.Config` — 类型名到配置的映射。

**功能描述：**
1. 遍历预定义的类型列表：`"explore"`, `"plan"`, `"general"`, `"reviewer"`, `"verifier"`, `"guard"`, `"lean4"`。
2. 对每个类型调用 `embed.GetAgentPrompt(name)` 读取系统提示词、模型名和温度参数。
3. 若嵌入文件中未指定模型名，按类型使用默认模型：
   - `explore` → `"deepseek:deepseek-v4-flash"`
   - `guard` → `"deepseek:deepseek-chat"`
   - 其他 → `"deepseek:deepseek-v4-pro"`
4. 调用 `embed.GetAgentTools(name)` 读取工具列表。

**注意事项：**
- 若某类型的嵌入提示词不存在或为空，该类型将被静默跳过。
- 返回的映射可直接作为 `PoolConfig.AgentConfigs` 使用。

---

## 测试辅助（导出）

以下类型和函数在 `pool_test.go` 中定义，供外部测试包（如集成测试）使用。

### `MockProvider`

```go
type MockProvider struct {
    // 未导出字段
}
```

模拟 LLM Provider，用于测试中替代真实的 LLM 调用。

### `NewMockProvider`

```go
func NewMockProvider(name, response string, delay time.Duration) *MockProvider
```

创建一个新的 MockProvider 实例。

**参数：**

| 参数 | 类型 | 描述 |
|------|------|------|
| `name` | `string` | Provider 名称 |
| `response` | `string` | 模拟的 LLM 响应文本 |
| `delay` | `time.Duration` | 模拟的响应延迟 |

**返回值：** `*MockProvider` — 模拟 Provider 实例。

### `MockProvider` 方法

#### `Name()`

```go
func (m *MockProvider) Name() string
```

返回 Provider 名称。

#### `Chat(ctx context.Context, req *llm.ChatRequest) (<-chan *llm.StreamEvent, error)`

模拟流式 LLM 调用。

**功能描述：**
- 启动 goroutine，在经过 `delay` 延迟后发送一个文本增量事件（`StreamTextDelta`），然后发送完成事件（`StreamDone`）。
- 在延迟期间响应 `ctx.Done()` 取消。
- channel 容量为 8。

**返回值：**

| 返回值 | 类型 | 描述 |
|------|------|------|
| `<-chan *llm.StreamEvent` | 只读事件 channel | 流式事件流 |
| `error` | error | 始终为 nil |

#### `ChatSync(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error)`

模拟同步 LLM 调用。

**返回值：** 始终返回 `nil, nil`。当前测试中不使用。

#### `Close() error`

关闭 Provider。

**返回值：** 始终返回 `nil`。

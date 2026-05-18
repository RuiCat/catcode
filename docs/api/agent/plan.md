# agent/plan — 规划追踪引擎

## 包概述

`agent/plan` 实现了 catcode 项目中的规划追踪引擎，提供任务分解、依赖管理、状态追踪以及工作流状态机等功能。该包通过事件总线（`event.EventBus`）与其他模块解耦通信，支持 Plan 模式的进入/退出（用于限制工具可用性），并能够基于事件自动推进任务状态流转。

---

## 类型定义

### `Status` — 任务状态

```go
type Status string
```

表示一个任务项的当前状态。

**常量：**

| 常量 | 值 | 说明 |
|---|---|---|
| `StatusPending` | `"pending"` | 待处理 |
| `StatusInProgress` | `"in_progress"` | 进行中 |
| `StatusCompleted` | `"completed"` | 已完成 |
| `StatusCancelled` | `"cancelled"` | 已取消 |
| `StatusFailed` | `"failed"` | 已失败 |

---

### `Priority` — 优先级

```go
type Priority string
```

表示任务项的优先级等级。

**常量：**

| 常量 | 值 | 说明 |
|---|---|---|
| `PriorityHigh` | `"high"` | 高优先级 |
| `PriorityMedium` | `"medium"` | 中优先级 |
| `PriorityLow` | `"low"` | 低优先级 |

---

### `TodoItem` — 任务项

```go
type TodoItem struct {
    ID          string     `json:"id"`
    Content     string     `json:"content"`
    Status      Status     `json:"status"`
    Priority    Priority   `json:"priority"`
    CreatedAt   time.Time  `json:"created_at"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}
```

表示规划中的一个子任务。

| 字段 | 类型 | 说明 |
|---|---|---|
| `ID` | `string` | 任务唯一标识 |
| `Content` | `string` | 任务描述内容 |
| `Status` | `Status` | 当前任务状态 |
| `Priority` | `Priority` | 优先级 |
| `CreatedAt` | `time.Time` | 创建时间 |
| `CompletedAt` | `*time.Time` | 完成时间（仅当状态为 completed/failed 时设置） |

---

### `Plan` — 规划

```go
type Plan struct {
    ID          string      `json:"id"`
    Description string      `json:"description"`
    Todos       []*TodoItem `json:"todos"`
    CreatedAt   time.Time   `json:"created_at"`
    UpdatedAt   time.Time   `json:"updated_at"`
}
```

表示一个完整的执行规划，包含一组子任务。

| 字段 | 类型 | 说明 |
|---|---|---|
| `ID` | `string` | 规划唯一标识 |
| `Description` | `string` | 规划总体描述 |
| `Todos` | `[]*TodoItem` | 子任务列表 |
| `CreatedAt` | `time.Time` | 创建时间 |
| `UpdatedAt` | `time.Time` | 最后更新时间 |

---

### `WorkflowState` — 工作流状态

```go
type WorkflowState string
```

表示工作流状态机的各个阶段。

**常量：**

| 常量 | 值 | 说明 |
|---|---|---|
| `StateIdle` | `"idle"` | 空闲状态，等待任务 |
| `StateExploring` | `"exploring"` | 探索阶段，分析代码库 |
| `StatePlanning` | `"planning"` | 规划阶段，制定执行计划 |
| `StateExecuting` | `"executing"` | 执行阶段，按计划实施 |
| `StateReviewing` | `"reviewing"` | 审查阶段，检查执行结果 |
| `StateVerifying` | `"verifying"` | 验证阶段，运行测试确认 |
| `StateCompleted` | `"completed"` | 已完成 |

---

### `Engine` — 规划引擎结构体

```go
type Engine struct {
    // 未导出字段
}
```

规划引擎的核心实现结构体。内部维护规划映射表（`plans`）、当前活跃规划 ID（`activePlanID`）、事件总线引用（`bus`）以及 Plan 模式状态。

**字段（未导出）：**

| 字段 | 类型 | 说明 |
|---|---|---|
| `plans` | `map[string]*Plan` | 以 ID 索引的所有规划 |
| `activePlanID` | `string` | 当前活跃规划的 ID |
| `bus` | `event.EventBus` | 事件总线实例 |
| `mu` | `sync.RWMutex` | 读写锁，保证并发安全 |
| `subscriptions` | `[]*event.Subscriber` | 事件订阅列表，用于 Close 时取消 |
| `closed` | `bool` | 引擎是否已关闭 |
| `planMode` | `bool` | 是否处于 Plan 模式 |
| `planModeReason` | `string` | 进入 Plan 模式的原因 |

---

### `PlanEngineInterface` — 规划引擎接口

```go
type PlanEngineInterface interface {
    CreatePlan(description string, todos []TodoItem) *Plan
    CreatePlanFromJSON(description, todosJSON string) (*Plan, error)
    GetActivePlan() *Plan
    Progress(planID string) float64
    EnterPlanMode(reason string) (string, error)
    ExitPlanMode(response string) (string, error)
    ListTodos(planID string) string
    IsPlanMode() bool
    Close()
}
```

规划引擎的公共接口，供外部模块通过依赖注入方式使用，便于测试和替换实现。

---

## 构造函数

### `NewEngine` — 创建规划引擎

```go
func NewEngine(bus event.EventBus) PlanEngineInterface
```

创建一个新的 `Engine` 实例，并自动订阅以下事件：

- `task.started` → 自动将下一个 pending 任务切换为 in_progress
- `task.completed` → 自动将当前 in_progress 任务标记为 completed
- `task.failed` → 自动将当前 in_progress 任务标记为 failed

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `bus` | `event.EventBus` | 事件总线实例，传入 `nil` 时事件功能不可用 |

**返回值：**

| 类型 | 说明 |
|---|---|
| `PlanEngineInterface` | 新创建的规划引擎实例 |

---

## 接口方法

### `CreatePlan` — 创建新规划

```go
func (e *Engine) CreatePlan(description string, todos []TodoItem) *Plan
```

创建一个新的规划并设为当前活跃规划。自动为每个 `TodoItem` 生成唯一 ID 和创建时间。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `description` | `string` | 规划描述 |
| `todos` | `[]TodoItem` | 任务列表 |

**返回值：**

| 类型 | 说明 |
|---|---|
| `*Plan` | 创建成功的规划对象，其 `ID` 格式为 `plan-{纳秒时间戳}` |

**副作用：** 发布 `plan.created` 事件；将新规划设为当前活跃规划。

---

### `CreatePlanFromJSON` — 从 JSON 创建规划

```go
func (e *Engine) CreatePlanFromJSON(description, todosJSON string) (*Plan, error)
```

从 JSON 字符串解析任务列表并创建新规划，供 LLM 工具调用使用。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `description` | `string` | 规划描述 |
| `todosJSON` | `string` | JSON 格式的任务列表字符串，需反序列化为 `[]TodoItem` |

**返回值：**

| 类型 | 说明 |
|---|---|
| `*Plan` | 创建成功的规划对象 |
| `error` | JSON 解析错误时返回，错误由 `core/errors` 包装 |

---

### `GetActivePlan` — 获取活跃规划

```go
func (e *Engine) GetActivePlan() *Plan
```

返回当前活跃的规划对象。

**返回值：**

| 类型 | 说明 |
|---|---|
| `*Plan` | 当前活跃规划；若无活跃规划返回 `nil` |

---

### `Progress` — 获取规划进度

```go
func (e *Engine) Progress(planID string) float64
```

计算指定规划的任务完成进度。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `planID` | `string` | 规划 ID |

**返回值：**

| 类型 | 说明 |
|---|---|
| `float64` | 进度值，范围 `0.0 ~ 1.0`。规划不存在或无任务时返回 `0.0` |

**计算方式：** `(completed + cancelled) / 总任务数`

---

### `EnterPlanMode` — 进入 Plan 模式

```go
func (e *Engine) EnterPlanMode(reason string) (string, error)
```

进入 Plan 模式，用于禁用 edit/write/bash 等修改类工具，仅保留只读工具。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `reason` | `string` | 进入 Plan 模式的原因 |

**返回值：**

| 类型 | 说明 |
|---|---|
| `string` | 带有状态信息和退出提示的消息文本 |
| `error` | 已在 Plan 模式中时返回 `nil`，不会报错 |

**副作用：** 发布 `plan.mode.entered` 事件；设置 `planMode` 为 `true`。

---

### `ExitPlanMode` — 退出 Plan 模式

```go
func (e *Engine) ExitPlanMode(response string) (string, error)
```

退出 Plan 模式，恢复所有工具的可用性。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `response` | `string` | 用户的响应或确认消息 |

**返回值：**

| 类型 | 说明 |
|---|---|
| `string` | 带有状态信息和用户响应的消息文本 |
| `error` | 不在 Plan 模式中时返回 `nil`，不会报错 |

**副作用：** 发布 `plan.mode.exited` 事件；清空 `planMode` 和 `planModeReason`。

---

### `ListTodos` — 列出任务列表

```go
func (e *Engine) ListTodos(planID string) string
```

以格式化文本形式返回规划中所有任务的状态概览。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `planID` | `string` | 规划 ID |

**返回值：**

| 类型 | 说明 |
|---|---|
| `string` | 格式化的任务列表文本，包含状态图标、优先级和进度百分比 |

**状态图标映射：**

| 状态 | 图标 |
|---|---|
| `pending` | ⬜ |
| `in_progress` | 🔄 |
| `completed` | ✅ |
| `cancelled` | ❌ |
| `failed` | ❌ |

---

### `IsPlanMode` — 检查 Plan 模式状态

```go
func (e *Engine) IsPlanMode() bool
```

查询当前是否处于 Plan 模式。

**返回值：**

| 类型 | 说明 |
|---|---|
| `bool` | `true` 表示当前处于 Plan 模式 |

---

### `Close` — 关闭引擎

```go
func (e *Engine) Close()
```

关闭规划引擎，取消所有事件订阅并释放资源。该方法为幂等操作，重复调用安全。

---

## 扩展方法（Engine 指针类型直接暴露，未包含在接口中）

### `ClearActivePlan` — 清除活跃规划

```go
func (e *Engine) ClearActivePlan()
```

清空当前活跃规划的引用。规划数据本身不会删除，仅将 `activePlanID` 置空。

---

### `GetPlan` — 按 ID 获取规划

```go
func (e *Engine) GetPlan(id string) *Plan
```

根据规划 ID 直接获取规划对象。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `id` | `string` | 规划 ID |

**返回值：**

| 类型 | 说明 |
|---|---|
| `*Plan` | 对应的规划对象，不存在返回 `nil` |

---

### `UpdateTodoStatus` — 更新任务状态

```go
func (e *Engine) UpdateTodoStatus(planID, todoID string, status Status) error
```

更新指定规划中某个任务的状态。当状态变为 `completed` 或 `failed` 时，自动设置 `CompletedAt` 时间戳。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `planID` | `string` | 规划 ID |
| `todoID` | `string` | 任务 ID |
| `status` | `Status` | 新状态 |

**返回值：**

| 类型 | 说明 |
|---|---|
| `error` | 规划或任务不存在时返回错误 |

**副作用：**
- 从 `pending` 变为 `in_progress` 时，发布 `plan.step.start` 事件
- 变为 `completed` 时，发布 `plan.step.done` 事件
- 所有任务完成时，发布 `plan.completed` 事件

---

### `GetTodosJSON` — 获取任务 JSON

```go
func (e *Engine) GetTodosJSON(planID string) (string, error)
```

以 JSON 字符串形式返回指定规划的任务列表，供 LLM 上下文使用。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `planID` | `string` | 规划 ID |

**返回值：**

| 类型 | 说明 |
|---|---|
| `string` | 任务列表的 JSON 字符串，规划不存在时返回 `"[]"` |
| `error` | JSON 序列化错误 |

---

## 事件监听机制

规划引擎在创建时通过 `event.EventBus` 订阅以下事件，实现状态自动流转：

### 订阅事件清单

| 事件名称 | 处理函数 | 优先级 | 触发行为 |
|---|---|---|---|
| `task.started` | `onTaskStarted` | 50 | 自动将活跃规划中首个 `pending` 任务切换为 `in_progress` |
| `task.completed` | `onTaskCompleted` | 50 | 自动将活跃规划中当前 `in_progress` 任务标记为 `completed` |
| `task.failed` | `onTaskFailed` | 50 | 自动将活跃规划中当前 `in_progress` 任务标记为 `failed` |

### 发布事件清单

规划引擎在状态变更时向事件总线发布以下事件：

| 事件名称 | 发布时机 | 附带数据 |
|---|---|---|
| `plan.created` | 调用 `CreatePlan` 成功后 | `plan_id`, `description`, `todo_count` |
| `plan.step.start` | 任务状态从非 in_progress 变为 in_progress 时 | `plan_id`, `todo_id` |
| `plan.step.done` | 任务状态变为 completed 时 | `plan_id`, `todo_id` |
| `plan.completed` | 规划中所有任务均已完成或取消时 | `plan_id` |
| `plan.mode.entered` | 调用 `EnterPlanMode` 成功后 | `reason` |
| `plan.mode.exited` | 调用 `ExitPlanMode` 成功后 | `response`, `reason` |

### 事件流转图

```
task.started  →  onTaskStarted  →  UpdateTodoStatus(pending → in_progress)
                                       ↓
task.completed  →  onTaskCompleted  →  UpdateTodoStatus(in_progress → completed)
                                          ↓  (如果全部完成)
                                    计划完成 → plan.completed

task.failed  →  onTaskFailed  →  UpdateTodoStatus(in_progress → failed)
```

---

## 并发安全

`Engine` 的所有公开方法均通过 `sync.RWMutex` 保护：
- 读操作（`GetActivePlan`, `GetPlan`, `Progress`, `ListTodos`, `GetTodosJSON`, `IsPlanMode`）使用读锁 `RLock`/`RUnlock`
- 写操作（`CreatePlan`, `UpdateTodoStatus`, `EnterPlanMode`, `ExitPlanMode`, `ClearActivePlan`, `Close`）使用写锁 `Lock`/`Unlock`

可在多 goroutine 环境下安全使用。

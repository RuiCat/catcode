# core/event — 事件总线

## 包概述

`core/event` 实现了 catcode 项目的事件总线和触发器系统，借鉴 opencat 的 `EventBus + TriggerManager` 设计，作为 catcode 内部编排的通信主干。该包提供：

- **EventBus 接口**：发布-订阅事件总线，支持通配符模式匹配、优先级排序发布、同步/异步发布、事件历史记录以及递归深度保护（防止事件链死循环）。
- **Event / Subscriber 类型**：事件携带名称和数据字典；订阅者通过模式匹配接收事件，每个订阅者的 `Handler` 串行执行以保证线程安全。
- **优先级排序发布**：订阅者按优先级降序排列，发布时所有匹配的订阅者按优先级从高到低依次执行。
- **Trigger 系统**：基于 `EventBus` 之上构建的条件触发器管理器，支持条件函数（`Condition`）过滤和一次性触发（`Once`），动作在独立 goroutine 中异步执行。

---

## 类型定义

### `Event` — 事件结构

```go
type Event struct {
    Name string         // 事件名称（如 "role.dispatch"）
    Data map[string]any // 事件携带数据
}
```

表示一个发布到总线上的事件。

| 字段 | 类型 | 说明 |
|---|---|---|
| `Name` | `string` | 事件名称，使用点分隔的命名空间（如 `"role.dispatch"`、`"plan.step.start"`） |
| `Data` | `map[string]any` | 事件携带的键值对数据，供订阅者处理时使用 |

---

### `Subscriber` — 事件订阅者

```go
type Subscriber struct {
    ID       string      // 订阅者标识
    Pattern  string      // 匹配模式（支持 * 通配符）
    Handler  func(Event) // 处理函数
    Priority int         // 优先级（数值越大越先执行）
}
```

表示一个订阅了事件总线的监听器。

| 字段 | 类型 | 说明 |
|---|---|---|
| `ID` | `string` | 订阅者的唯一标识，用于日志追踪和调试 |
| `Pattern` | `string` | 匹配模式，支持三种形式：`"*"` 匹配所有事件；`"prefix.*"` 匹配指定前缀下所有事件；`"*.suffix"` 匹配指定后缀的所有事件 |
| `Handler` | `func(Event)` | 事件到达时的回调函数，由内部互斥锁保证串行执行 |
| `Priority` | `int` | 优先级数值，越大越先执行。同一模式组内的订阅者按优先级降序排列 |

> **注意**：`Subscriber` 内部持有 `sync.Mutex`（未导出字段），保证 `Handler` 在同一时间只被一个 goroutine 执行。

---

### `EventBus` — 事件总线接口

```go
type EventBus interface {
    Subscribe(id string, pattern string, handler func(Event), priority int) *Subscriber
    Unsubscribe(sub *Subscriber)
    Publish(name string, data map[string]any)
    PublishAsync(name string, data map[string]any)
    SubscriberCount() int
}
```

发布-订阅事件总线接口。并发安全，可同时在不同 goroutine 中发布和订阅。

| 方法 | 说明 |
|---|---|
| `Subscribe` | 注册一个事件订阅者，返回 `*Subscriber` 用于后续取消订阅 |
| `Unsubscribe` | 取消指定订阅者的注册 |
| `Publish` | 同步发布事件：按优先级顺序依次调用所有匹配的 Handler，调用方会阻塞直到全部 Handler 执行完成 |
| `PublishAsync` | 异步发布事件：在独立 goroutine 中调用 `Publish`，调用方立即返回不阻塞 |
| `SubscriberCount` | 返回当前已注册的订阅者总数 |

---

## 构造函数

### `NewBus` — 创建新的事件总线

```go
func NewBus() EventBus
```

创建并返回一个新的 `EventBus` 实例。

**内部实现细节：**

- 事件历史容量默认为 100 条。
- `publishDepth` 字段类型为 `atomic.Int32`，使用原子操作保证并发安全。发布递归深度上限为 10（通过 `Load()` 读取计数器值防止事件链死循环，超过 10 层则直接返回）。
- 内部使用 `sync.RWMutex` 保护订阅者列表，`sync.RWMutex` 保护事件历史。

**返回值：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `EventBus` | `EventBus` | 新创建的事件总线实例 |

---

### `NewTriggerManager` — 创建触发器管理器

```go
func NewTriggerManager(bus EventBus) *TriggerManager
```

创建并返回一个新的 `TriggerManager` 实例，并自动向给定 `EventBus` 注册一个内部订阅者（ID 为 `"__trigger_manager"`，模式为 `"*"`，优先级为 `0`）。该内部订阅者负责接收所有事件并匹配合适的触发器。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `bus` | `EventBus` | 要监听的事件总线实例 |

**返回值：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `*TriggerManager` | `*TriggerManager` | 新创建的触发器管理器，已自动订阅总线 |

---

### `NewEvent` — 便捷构造器

```go
func NewEvent(name string, data map[string]any) Event
```

创建一个新的 `Event` 实例。如果 `data` 为 `nil`，会自动初始化为空 map，避免后续使用时出现 nil map 写入错误。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `name` | `string` | 事件名称 |
| `data` | `map[string]any` | 事件数据，可为 `nil` |

**返回值：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `Event` | `Event` | 构造好的事件结构 |

---

### `ValidateEventName` — 校验事件名格式

```go
func ValidateEventName(name string) error
```

校验给定的事件名称是否符合规范。

**校验规则：**

- 事件名不能为空。
- 事件名不能包含空格。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `name` | `string` | 待校验的事件名称 |

**返回值：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `error` | `error` | 校验通过返回 `nil`，否则返回描述性错误（来自 `core/errors` 包） |

---

## EventBus 方法

以下方法由 `NewBus()` 返回的 `EventBus` 接口提供。

### `Subscribe` — 订阅事件

```go
func (bus *eventBusImpl) Subscribe(id string, pattern string, handler func(Event), priority int) *Subscriber
```

注册一个事件订阅者并返回 `*Subscriber` 句柄，用于后续取消订阅。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `id` | `string` | 订阅者唯一标识 |
| `pattern` | `string` | 匹配模式，支持通配符 |
| `handler` | `func(Event)` | 事件处理回调函数 |
| `priority` | `int` | 优先级，数值越大越先执行 |

**返回值：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `*Subscriber` | `*Subscriber` | 订阅者句柄，供 `Unsubscribe` 使用 |

**通配符模式说明：**

| 模式 | 匹配行为 | 示例 |
|---|---|---|
| `"*"` | 匹配所有事件 | 订阅所有事件 |
| `"prefix.*"` | 匹配指定前缀下的所有事件 | `"role.*"` 匹配 `"role.dispatch"`、`"role.loaded"` 等 |
| `"*.suffix"` | 匹配指定后缀的所有事件 | `"*.completed"` 匹配 `"task.completed"`、`"plan.completed"` 等 |
| 精确匹配 | 仅当 `pattern == name` 时匹配 | `"role.dispatch"` 仅匹配 `"role.dispatch"` |

> **注意**：通配符不支持中间位置的 `*`（如 `"a.*.b"`），仅支持前缀和后缀两种模式。

---

### `Unsubscribe` — 取消订阅

```go
func (bus *eventBusImpl) Unsubscribe(sub *Subscriber)
```

从事件总线中移除之前注册的订阅者。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `sub` | `*Subscriber` | 之前由 `Subscribe` 返回的订阅者句柄 |

---

### `Publish` — 同步发布事件

```go
func (bus *eventBusImpl) Publish(name string, data map[string]any)
```

同步发布事件。执行流程如下：

1. 使用 `bus.publishDepth.Load()` 检查递归深度（超过 10 层直接返回，防止事件链死循环），使用 `bus.publishDepth.Add(1)` 递增、`defer bus.publishDepth.Add(-1)` 递减。
2. 将事件记录到历史缓存（最近 100 条）。
3. 遍历所有订阅模式，通过 `matchPattern` 收集匹配的订阅者。
4. 按优先级降序排列所有匹配的订阅者。
5. 依次串行执行每个订阅者的 `Handler`（通过 `Subscriber.mu` 互斥锁保证单个订阅者的 Handler 串行化）。

调用方会阻塞直到所有匹配的 Handler 执行完成。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `name` | `string` | 事件名称 |
| `data` | `map[string]any` | 事件携带的数据 |

---

### `PublishAsync` — 异步发布事件

```go
func (bus *eventBusImpl) PublishAsync(name string, data map[string]any)
```

在独立 goroutine 中调用 `Publish`，调用方立即返回不阻塞。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `name` | `string` | 事件名称 |
| `data` | `map[string]any` | 事件携带的数据 |

---

### `SubscriberCount` — 返回订阅者总数

```go
func (bus *eventBusImpl) SubscriberCount() int
```

返回当前事件总线中注册的订阅者总数。

**返回值：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `int` | `int` | 订阅者总数 |

---

### `History` — 返回事件历史

```go
func (bus *eventBusImpl) History() []Event
```

返回最近发布的事件历史列表（最多 100 条），按发布时间从旧到新排列。返回的是深拷贝切片，修改返回值不会影响内部状态。

**返回值：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `[]Event` | `[]Event` | 事件历史切片 |

> **注意**：此方法不属于 `EventBus` 接口，仅为 `eventBusImpl` 的导出方法。若需通过接口访问，请将返回值类型断言为具体实现类型。

---

## Trigger 系统

### `Trigger` — 条件触发器定义

```go
type Trigger struct {
    Name      string           // 触发器名称
    Event     string           // 匹配的事件名（支持 * 通配符）
    Condition func(Event) bool // 条件检查（可选，nil 表示总是触发）
    Action    func(Event)      // 触发时执行的动作
    Priority  int              // 优先级
    Once      bool             // 是否只触发一次
}
```

表示一个条件触发器，当匹配的事件到达且 `Condition` 函数返回 `true` 时，执行 `Action` 函数。

| 字段 | 类型 | 说明 |
|---|---|---|
| `Name` | `string` | 触发器名称，用于 `Unregister` 注销 |
| `Event` | `string` | 匹配的事件名，支持 `*` 通配符（规则同 `Subscriber.Pattern`） |
| `Condition` | `func(Event) bool` | 条件检查函数。为 `nil` 时表示总是触发；不为 `nil` 时仅当返回 `true` 才触发 |
| `Action` | `func(Event)` | 触发时执行的动作函数，在独立 goroutine 中异步执行，不阻塞事件流 |
| `Priority` | `int` | 优先级，数值越大越先匹配 |
| `Once` | `bool` | 是否只触发一次。设为 `true` 后，首次触发即自动标记为已触发，后续事件不再匹配 |

---

### `TriggerManager` — 触发器管理器

```go
type TriggerManager struct {
    // 内部字段未导出
}
```

触发器管理器监听 `EventBus` 上的事件，匹配注册的触发器并执行对应动作。内部维护触发器列表按优先级降序排列。

**方法：**

| 方法 | 说明 |
|---|---|
| `Register` | 注册一个新的触发器 |
| `Unregister` | 按名称注销一个触发器 |

---

### `Register` — 注册触发器

```go
func (tm *TriggerManager) Register(t *Trigger)
```

注册一个新的触发器到管理器中。注册后触发器列表会按优先级降序重新排序。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `t` | `*Trigger` | 要注册的触发器指针 |

---

### `Unregister` — 注销触发器

```go
func (tm *TriggerManager) Unregister(name string)
```

按名称从管理器中注销一个触发器。

**参数：**

| 参数 | 类型 | 说明 |
|---|---|---|
| `name` | `string` | 要注销的触发器名称（对应 `Trigger.Name` 字段） |

---

### 触发器内部匹配逻辑（`onEvent`）

`TriggerManager` 通过内部方法 `onEvent` 处理到达的事件，匹配逻辑如下：

1. 遍历所有已注册的触发器（按优先级降序）。
2. 检查触发器的 `Event` 字段是否与事件的 `Name` 匹配（使用 `matchPattern`，支持通配符）。
3. 若触发器的 `Once` 为 `true` 且已触发过，则跳过。
4. 若触发器的 `Condition` 不为 `nil` 且返回 `false`，则跳过。
5. 满足所有条件后，标记触发器为已触发（`fired = true`），并异步执行 `Action` 函数（在独立 goroutine 中运行，不阻塞事件流的后续处理）。

---

## 内置事件常量

包内预定义了 33 个事件名称常量，按类别分组如下：

### 用户交互事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventUserRequestReceived` | `"user.request.received"` | 用户请求已接收 |
| `EventUserRequestCompleted` | `"user.request.completed"` | 用户请求处理完成 |

### 角色生命周期事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventRoleLoaded` | `"role.loaded"` | 角色配置已加载 |
| `EventRoleActivated` | `"role.activated"` | 角色已激活 |
| `EventRoleDispatch` | `"role.dispatch"` | 角色被调度执行 |
| `EventRoleResult` | `"role.result"` | 角色执行返回结果 |
| `EventRoleError` | `"role.error"` | 角色执行出错 |
| `EventRoleUpdated` | `"role.updated"` | 角色配置已更新 |
| `EventRoleUnloaded` | `"role.unloaded"` | 角色已卸载 |

### 子智能体事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventSubAgentDispatch` | `"subagent.dispatch"` | 子智能体被调度执行 |
| `EventSubAgentResult` | `"subagent.result"` | 子智能体返回执行结果 |
| `EventSubAgentError` | `"subagent.error"` | 子智能体执行出错 |
| `EventAgentStatusChanged` | `"agent.status.changed"` | 智能体状态发生变化 |

### 规划引擎事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventPlanCreated` | `"plan.created"` | 规划已创建 |
| `EventPlanStepStart` | `"plan.step.start"` | 规划步骤开始执行 |
| `EventPlanStepDone` | `"plan.step.done"` | 规划步骤执行完成 |
| `EventPlanCompleted` | `"plan.completed"` | 规划执行完成 |
| `EventPlanModeEntered` | `"plan.mode.entered"` | 进入 Plan 模式 |
| `EventPlanModeExited` | `"plan.mode.exited"` | 退出 Plan 模式 |

### 任务状态事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventTaskStarted` | `"task.started"` | 任务开始 |
| `EventTaskCompleted` | `"task.completed"` | 任务完成 |
| `EventTaskFailed` | `"task.failed"` | 任务失败 |

### 子智能体工具事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventAgentToolStart` | `"agent.tool.start"` | 子智能体开始执行工具 |
| `EventAgentToolEnd` | `"agent.tool.end"` | 子智能体工具执行完成 |

### 工具调用事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventToolCallStart` | `"tool.call.start"` | 工具调用开始 |
| `EventToolCallEnd` | `"tool.call.end"` | 工具调用结束 |

### Session 事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventSessionCreated` | `"session.created"` | 会话已创建 |
| `EventSessionSaved` | `"session.saved"` | 会话已保存 |

### Companion 陪伴角色事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventCompanionTalk` | `"companion.talk"` | 用户与陪伴角色对话 |
| `EventCompanionRespond` | `"companion.respond"` | 陪伴角色作出回应 |
| `EventCompanionStatus` | `"companion.status"` | 陪伴角色状态变化 |

### 对话框消息事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventDialogSend` | `"dialog.send"` | 发送对话框消息（供 `send_message` 工具使用） |

### 选项框事件

| 常量 | 值 | 说明 |
|---|---|---|
| `EventQuestionAsked` | `"question.asked"` | 已向用户发出提问 |

---

## 辅助函数

### `matchPattern` — 事件名模式匹配

```go
func matchPattern(pattern, name string) bool
```

内部使用的模式匹配函数，支持三种匹配规则：

1. `pattern == "*"` → 匹配所有事件名称。
2. `pattern == name` → 精确匹配。
3. `pattern` 以 `".*"` 结尾 → 前缀匹配，如 `"role.*"` 匹配 `"role.dispatch"`。
4. `pattern` 以 `"*."` 开头 → 后缀匹配，如 `"*.completed"` 匹配 `"task.completed"`。
5. 其他情况 → 不匹配。

---

## 使用示例

### 创建事件总线并订阅

```go
import "catcode/core/event"

bus := event.NewBus()

// 订阅所有角色相关事件，优先级为 10
sub := bus.Subscribe("my-subscriber", "role.*", func(evt event.Event) {
    fmt.Printf("收到角色事件: %s, 数据: %v\n", evt.Name, evt.Data)
}, 10)

// 发布事件
bus.Publish(event.EventRoleDispatch, map[string]any{
    "role_name": "greeter",
    "message":   "hello",
})

// 取消订阅
bus.Unsubscribe(sub)
```

### 使用触发器系统

```go
bus := event.NewBus()
tm := event.NewTriggerManager(bus)

// 注册一个一次性触发器：首次 plan.completed 事件时记录日志
tm.Register(&event.Trigger{
    Name:  "log-plan-completion",
    Event: event.EventPlanCompleted,
    Condition: func(evt event.Event) bool {
        // 仅当 tasks_completed > 5 时触发
        count, ok := evt.Data["tasks_completed"].(int)
        return ok && count > 5
    },
    Action: func(evt event.Event) {
        fmt.Println("大型规划已完成！")
    },
    Priority: 100,
    Once:     true,
})

// 发布事件 — 触发器会在独立 goroutine 中异步执行 Action
bus.Publish(event.EventPlanCompleted, map[string]any{
    "tasks_completed": 8,
})

// 注销触发器
tm.Unregister("log-plan-completion")
```

### 使用便捷构造器和校验

```go
evt := event.NewEvent("custom.event", nil)
// evt.Data 现在是空 map，而非 nil

err := event.ValidateEventName("invalid name")
if err != nil {
    fmt.Println("事件名非法:", err) // 输出: 事件名不能包含空格
}
```

# schedule — 调度系统

## 包概述

`schedule` 包实现了周期任务调度与空闲检测系统。当检测到用户无输入且无智能体运行时，调度器会自动执行预定义的默认任务。核心组件包括：

- **IdleDetector**：跟踪用户和智能体的活跃状态，判断系统是否空闲。
- **IdleTask 接口**：定义空闲任务的统一契约，任何实现该接口的类型均可被调度器执行。
- **TaskResult**：封装单次任务执行的结果信息。
- **Scheduler**：管理所有周期任务，按间隔和条件触发执行。
- **DBTask**：从数据库持久化加载的周期任务实现，支持命令执行和繁忙检测。

---

## 类型

### IdleDetector

空闲检测器，跟踪系统和用户的空闲状态。当用户无输入且智能体无活动的时间均超过阈值时，判定系统为空闲。

```go
type IdleDetector struct {
    // 包含未导出的字段
}
```

| 字段（内部） | 说明 |
|-------------|------|
| `lastUserInput` | 最后一次用户输入的时间 |
| `lastAgentActive` | 最后一次智能体活跃的时间 |
| `idleThreshold` | 判定空闲的时间阈值 |

---

### TaskResult

任务执行结果，由 `IdleTask.Run()` 返回。

```go
type TaskResult struct {
    Name    string
    Output  string
    Skipped bool
    Error   error
}
```

**字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 任务名称 |
| `Output` | `string` | 任务的输出文本 |
| `Skipped` | `bool` | 任务是否因条件不满足而被跳过 |
| `Error` | `error` | 执行过程中产生的错误（nil 表示成功） |

---

### DBTask

从数据库加载的周期任务，实现 `IdleTask` 接口。将 `storage.ScheduledTask` 记录包装为可调度的任务。

```go
type DBTask struct {
    Row  *storage.ScheduledTask
    Wdb  storage.WorkspaceDB
    Busy func() bool
}
```

**字段说明：**

| 字段 | 类型 | 说明 |
|------|------|------|
| `Row` | `*storage.ScheduledTask` | 来自数据库的任务记录，包含名称、间隔、描述、启用状态等 |
| `Wdb` | `storage.WorkspaceDB` | 工作区数据库接口，用于标记任务执行时间 |
| `Busy` | `func() bool` | 外部传入的智能体繁忙检查函数（nil 表示不检查） |

---

### Scheduler

周期任务调度器，管理所有注册的 `IdleTask`。由外部 Tick 驱动，每次 Tick 调用 `Check()` 检查并执行到期的任务。

```go
type Scheduler struct {
    // 包含未导出的字段
}
```

| 字段（内部） | 说明 |
|-------------|------|
| `detector` | 关联的空闲检测器 |
| `tasks` | 已注册的任务列表 |
| `lastRun` | 每个任务名称对应上次执行时间的映射 |
| `results` | 最近的任务执行结果（最多保留 50 条） |

---

## 接口

### IdleTask

空闲时执行的任务，所有可调度任务必须实现此接口。

```go
type IdleTask interface {
    Name() string
    Interval() time.Duration
    Condition() func() bool
    Run() TaskResult
}
```

**方法说明：**

| 方法 | 返回值 | 说明 |
|------|--------|------|
| `Name()` | `string` | 返回任务的唯一名称，用于记录上次执行时间 |
| `Interval()` | `time.Duration` | 返回任务的最小执行间隔，调度器不会在间隔内重复触发 |
| `Condition()` | `func() bool` | 返回额外的触发条件函数；返回 nil 表示无额外条件，总是满足 |
| `Run()` | `TaskResult` | 执行任务并返回结果 |

---

## 构造函数

### NewIdleDetector

```go
func NewIdleDetector(idleThreshold time.Duration) *IdleDetector
```

创建新的空闲检测器实例。

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `idleThreshold` | `time.Duration` | 判定空闲的时间阈值。用户最后一次输入和智能体最后一次活动均超过此阈值时，系统被判定为空闲 |

**返回值：**
- `*IdleDetector`：初始化完成的空闲检测器，`lastUserInput` 和 `lastAgentActive` 均设为当前时间。

---

### NewScheduler

```go
func NewScheduler(detector *IdleDetector) *Scheduler
```

创建新的调度器实例。

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `detector` | `*IdleDetector` | 关联的空闲检测器，调度器通过它判断系统是否空闲 |

**返回值：**
- `*Scheduler`：初始化完成的调度器，内部 `lastRun` 映射和 `results` 切片已初始化。

---

## IdleDetector 方法

### Touch

```go
func (d *IdleDetector) Touch()
```

标记用户活动。将 `lastUserInput` 更新为当前时间。应在每次收到用户输入时调用。

**参数：** 无
**返回值：** 无

---

### MarkAgentActive

```go
func (d *IdleDetector) MarkAgentActive()
```

标记智能体活动。将 `lastAgentActive` 更新为当前时间，重置智能体空闲计时。

**参数：** 无
**返回值：** 无

---

### IsIdle

```go
func (d *IdleDetector) IsIdle() bool
```

检查系统是否处于空闲状态。当用户空闲时长和智能体空闲时长 **均** 大于等于 `idleThreshold` 时返回 true。

**参数：** 无

**返回值：**
- `bool`：`true` 表示系统空闲（用户无输入且智能体无活动超过阈值），`false` 表示仍在活跃。

---

### IdleDuration

```go
func (d *IdleDetector) IdleDuration() time.Duration
```

返回自最后一次用户输入以来的空闲时长。

**参数：** 无

**返回值：**
- `time.Duration`：用户空闲时长，等于 `time.Since(d.lastUserInput)`。

---

## Scheduler 方法

### Register

```go
func (s *Scheduler) Register(task IdleTask)
```

向调度器注册一个空闲任务。注册后的任务将在后续 `Check()` 调用中被评估和执行。

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `task` | `IdleTask` | 实现了 `IdleTask` 接口的任务实例 |

**返回值：** 无

**并发安全：** 使用互斥锁保护，可安全并发调用。

---

### Check

```go
func (s *Scheduler) Check(agentBusy bool) []TaskResult
```

检查并执行到期的空闲任务。此方法应由外部 Tick（如 Bubble Tea 的 `TickMsg`）周期性驱动。执行逻辑：

1. 若 `agentBusy` 为 `true`，标记智能体活跃并立即返回 nil（不执行任何任务）。
2. 调用 `detector.IsIdle()` 检查系统是否空闲，若非空闲则返回 nil。
3. 遍历所有已注册任务，对每个任务检查：
   - 距离上次执行时间是否已达到 `Interval()`；
   - `Condition()` 是否满足（nil 或返回 true）。
4. 对满足条件的任务调用 `Run()` 执行，记录执行时间，将结果追加到内部结果列表（最多保留 50 条）。

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `agentBusy` | `bool` | 当前是否有智能体正在运行；`true` 时所有任务均被跳过 |

**返回值：**
- `[]TaskResult`：本轮执行的任务结果列表；若无任务被触发则返回 nil。

**并发安全：** 使用互斥锁保护，可安全并发调用。

---

### Results

```go
func (s *Scheduler) Results() []TaskResult
```

返回最近的任务执行结果（最多 50 条）。返回的是内部结果的副本，修改不会影响调度器内部状态。

**参数：** 无

**返回值：**
- `[]TaskResult`：最近执行的任务结果列表。

---

### Detector

```go
func (s *Scheduler) Detector() *IdleDetector
```

返回调度器关联的空闲检测器实例。

**参数：** 无

**返回值：**
- `*IdleDetector`：调度器创建时传入的空闲检测器。

---

## DBTask 方法

DBTask 实现 `IdleTask` 接口，以下四个方法构成其实现。

### Name

```go
func (t *DBTask) Name() string
```

返回任务名称，直接取自数据库中 `Row.Name`。

**参数：** 无

**返回值：**
- `string`：任务名称。

---

### Interval

```go
func (t *DBTask) Interval() time.Duration
```

返回任务的最小执行间隔，由数据库中 `Row.IntervalSeconds` 字段（秒）转换为 `time.Duration`。

**参数：** 无

**返回值：**
- `time.Duration`：任务执行间隔。

---

### Condition

```go
func (t *DBTask) Condition() func() bool
```

返回触发条件函数。若 `Busy` 字段为 nil，返回 nil（无额外条件）；否则返回一个闭包，仅在智能体不繁忙（`!t.Busy()`）时返回 true。

**参数：** 无

**返回值：**
- `func() bool`：条件检查函数；nil 表示无额外条件。

---

### Run

```go
func (t *DBTask) Run() TaskResult
```

执行任务。执行流程：

1. 若 `Busy` 非 nil 且智能体繁忙，直接返回 `Skipped: true` 的 `TaskResult`。
2. 通过 `Wdb.MarkTaskRun()` 标记数据库中的任务最后执行时间。
3. 调用内部 `executeTask()` 解析并执行任务描述中的命令。
   - 若描述为空，返回 `"完成 (无具体操作描述)"`；
   - 若描述不以命令格式开头（由 `looksLikeCommand` 判断），返回 `"任务: ... — 已检查 (非命令描述)"`；
   - 否则通过 `sh -c` 执行命令（30 秒超时），返回执行成功或失败的输出。

**参数：** 无

**返回值：**
- `TaskResult`：包含任务名称和执行输出的结果。

---

## 函数

### LoadDBTasks

```go
func LoadDBTasks(s *Scheduler, wdb storage.WorkspaceDB, busy func() bool) error
```

从数据库加载所有已启用的定时任务并注册到调度器中。遍历 `wdb.ListScheduledTasks()` 返回的记录，仅注册 `Enabled` 为 `true` 的任务。

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `s` | `*Scheduler` | 目标调度器，任务将被注册到其中 |
| `wdb` | `storage.WorkspaceDB` | 工作区数据库接口，用于查询定时任务列表和执行时标记 |
| `busy` | `func() bool` | 智能体繁忙检查函数，会传递给每个 `DBTask` 实例 |

**返回值：**
- `error`：查询数据库失败时返回错误；成功返回 nil。

---

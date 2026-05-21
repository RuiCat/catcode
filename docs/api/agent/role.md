# agent/role — 角色系统

## 包概述

`agent/role` 实现侧加载（sideload）角色系统。角色 = 智能体配置 + 提示词，支持从数据库和文件系统动态加载。

该包提供两种角色类型：

- **功能型智能体（agent）**：可被主智能体委派任务，如 architect、reviewer、explore 等。
- **陪伴型角色（companion）**：事件驱动、具有情绪状态的互动角色。

角色定义通过 YAML 或 JSON 文件编写，存放在 `.catcode/roles/` 目录下，内置角色在编译时嵌入二进制。支持热重载和运行时状态管理。

---

## 角色合并优先级

角色定义从三个来源加载，按以下优先级（低到高）合并，高优先级覆盖低优先级：

```
builtin（DB 内置）< user_db（DB 用户自定义）< user_file（.catcode/roles/ 文件）
```

1. **builtin** — 编译时嵌入或从 DB `source = "builtin"` 加载的内置角色。
2. **user_db** — 用户通过配置面板存入数据库的自定义角色（`source = "user_db"`）。
3. **user_file** — 用户在 `.catcode/roles/` 目录下以 YAML/JSON 文件定义的角色，优先级最高。

同名角色按上述优先级覆盖：文件定义覆盖 DB 用户定义，DB 用户定义覆盖内置定义。

合并实现在 `mergeAgentDefs`（`merge.go` 第 13 行），该函数为包内函数，不对外导出。

---

## 类型与常量

### `RoleType`

```go
type RoleType string
```

角色类型枚举。

| 常量 | 值 | 说明 |
|------|-----|------|
| `RoleAgent` | `"agent"` | 功能型智能体 |
| `RoleCompanion` | `"companion"` | 陪伴型角色 |

---

### `RoleMode`

```go
type RoleMode string
```

角色运行模式枚举。

| 常量 | 值 | 说明 |
|------|-----|------|
| `ModePrimary` | `"primary"` | 主智能体（工作区有且只有一个） |
| `ModeSubAgent` | `"subagent"` | 子智能体（被主智能体委派任务） |
| `ModeBackground` | `"background"` | 后台角色（事件驱动，常驻但不主动对话） |

---

### `RoleDef`

```go
type RoleDef struct {
    Name         string         // 角色唯一标识名称
    DisplayName  string         // 用户可见的显示名称
    Type         RoleType       // 角色类型：agent 或 companion
    Mode         RoleMode       // 运行模式：primary / subagent / background
    Description  string         // 角色描述
    SystemPrompt string         // 系统提示词（核心，定义角色行为）
    Model        ModelConfig    // 模型配置
    Temperature  float64        // 顶层温度（可选，优先于 Model.Temperature）
    Permission   map[string]any // 权限配置（键值对）
    Tools        []string       // 该角色可用的工具列表
    Triggers     []TriggerDef   // 事件触发器

    // 陪伴型角色特有
    Persona map[string]int // 人格维度（key → 0~100 整数值）
    States  []StateDef     // 运行时状态定义

    // 元数据（序列化时忽略）
    SourcePath string    // 定义来源文件路径
    LoadedAt   time.Time // 加载时间戳
}
```

**说明：**
- `Name` 是唯一标识，不能为空，同一工作区内不可重复。
- `SystemPrompt` 是角色的核心行为指令，支持 YAML 多行文本块（`|`）。
- `Model` 嵌套模型配置；若顶层 `Temperature` 不为零，业务层可将其作为模型温度的 fallback。
- `Permission` 和 `Tools` 为空时（`nil`）会在构造时初始化为空 map / 空切片。
- `Persona` 和 `States` 仅在 `Type = companion` 时有意义，`omitempty` 序列化。
- `SourcePath` 和 `LoadedAt` 为运行时元数据，不会参与 JSON/YAML 的序列化 / 反序列化。

---

### `ModelConfig`

```go
type ModelConfig struct {
    Provider    string          // LLM 提供商（如 "openai", "anthropic", "deepseek"）
    Name        string          // 模型名称（如 "gpt-4o", "claude-3.5-sonnet"）
    Temperature float64         // 采样温度
    Thinking    *ThinkingConfig // thinking 模式配置（可选）
    Limit       *ModelLimit     // 上下文/输出长度限制（可选）
}
```

---

### `ThinkingConfig`

```go
type ThinkingConfig struct {
    Enabled      bool // 是否启用 thinking（推理链）
    BudgetTokens int  // thinking 预算 token 数
}
```

---

### `ModelLimit`

```go
type ModelLimit struct {
    Context int // 上下文窗口 token 上限
    Output  int // 输出 token 上限
}
```

---

### `TriggerDef`

```go
type TriggerDef struct {
    Event     string // 触发事件名（对应 event.Event 的 Type）
    Condition string // 触发条件表达式（可选）
    Action    string // 触发后执行的动作
    Message   string // 触发时的提示消息（可选）
    Priority  int    // 触发器优先级（越高越先执行）
}
```

触发器用于将事件和角色行为关联。当 EventBus 上发布匹配的 `Event` 时，若 `Condition` 满足（或无条件），则执行 `Action` 并可选发送 `Message`。

---

### `StateDef`

```go
type StateDef struct {
    Name        string // 状态名称（如 "mood", "energy"）
    Type        string // 状态类型标识
    RangeMin    int    // 状态值下限
    RangeMax    int    // 状态值上限
    Description string // 状态描述
}
```

状态定义仅用于陪伴型角色，描述该角色在运行时维护的内部状态变量。

---

### `Hook`

```go
type Hook func(instance *Instance, evt event.Event)
```

生命周期钩子函数类型。在角色收到事件时被调用，可用于扩展角色行为。

---

## RoleDef 方法

### `func (d *RoleDef) Validate() error`

**签名：** `func (d *RoleDef) Validate() error`

**参数：** 无（接收者 `*RoleDef`）

**返回值：** `error` — 如果合法则返回 `nil`，否则返回描述错误原因的 `error`

**功能描述：** 校验角色定义的合法性。检查以下字段：
- `Name` 不能为空。
- `Type` 必须是 `RoleAgent` 或 `RoleCompanion`。
- `Mode` 必须是 `ModePrimary`、`ModeSubAgent` 或 `ModeBackground`。

通常在加载或注册角色之前调用。

---

## 转换函数

### `func AgentRowToRoleDef(row *storage.AgentRow) RoleDef`

**签名：** `func AgentRowToRoleDef(row *storage.AgentRow) RoleDef`

**参数：**
- `row *storage.AgentRow` — 来自数据库代理表的行数据

**返回值：** `RoleDef` — 转换后的角色定义

**功能描述：** 将数据库行（`storage.AgentRow`）转换为 `RoleDef` 结构体。该函数负责：
- 映射基础字段（Name、DisplayName、Type、Mode 等）。
- 构建嵌套的 `ModelConfig`（包括条件解析 `Thinking` 和 `Limit`）。
- 反序列化 `PermissionJSON`、`ToolsJSON`、`TriggersJSON` 等 JSON 列。
- 若 JSON 列为空，返回空 map / 空切片（非 nil）。

---

### `func RoleDefToAgentRow(def *RoleDef, source, sourcePath string) *storage.AgentRow`

**签名：** `func RoleDefToAgentRow(def *RoleDef, source, sourcePath string) *storage.AgentRow`

**参数：**
- `def *RoleDef` — 角色定义
- `source string` — 来源标识（如 `"builtin"`、`"user_db"`、`"user_file"`）
- `sourcePath string` — 来源文件路径

**返回值：** `*storage.AgentRow` — 转换后的数据库行结构

**功能描述：** 将 `RoleDef` 序列化为 `storage.AgentRow`，用于写入数据库。该函数负责：
- 映射所有基本字段。
- 序列化 `Permission`、`Tools`、`Triggers` 为 JSON 字符串。
- 设置 `Enabled = true`（默认启用）。
- 展开嵌套的 `ModelConfig`、`Thinking`、`Limit` 结构。

---

## 运行时实例

### `type Instance`

```go
type Instance struct {
    Def    RoleDef            // 角色定义（只读）
    Active bool               // 是否已激活
    State  map[string]int     // 运行时状态（陪伴型角色用）
    Hooks  []Hook             // 生命周期钩子列表
    Tools  []*tool.Tool       // 该角色专属工具
}
```

`Instance` 是角色的运行时表示。每个注册的角色对应一个 `Instance`，包含其定义、激活状态、内部状态变量和钩子。

内部使用 `sync.RWMutex` 保护并发访问。

---

### `func NewInstance(def RoleDef) *Instance`

**签名：** `func NewInstance(def RoleDef) *Instance`

**参数：**
- `def RoleDef` — 角色定义

**返回值：** `*Instance` — 新创建的运行时角色实例

**功能描述：** 根据 `RoleDef` 创建 `Instance`。初始化时 `Active` 为 `false`，`State` 和 `Hooks`、`Tools` 均为空集合。

---

### `func (ins *Instance) SetState(key string, value int)`

**签名：** `func (ins *Instance) SetState(key string, value int)`

**参数：**
- `key string` — 状态键名
- `value int` — 状态值

**返回值：** 无

**功能描述：** 设置角色的运行时状态值。使用写锁保护并发写入。

---

### `func (ins *Instance) GetState(key string) int`

**签名：** `func (ins *Instance) GetState(key string) int`

**参数：**
- `key string` — 状态键名

**返回值：** `int` — 状态值（若键不存在返回 Go 零值 `0`）

**功能描述：** 获取角色的运行时状态值。使用读锁保护并发读取。

---

### `func (ins *Instance) Activate()`

**签名：** `func (ins *Instance) Activate()`

**参数：** 无

**返回值：** 无

**功能描述：** 激活角色，将 `Active` 设置为 `true`。激活后的角色可以接收事件和响应调用。

---

### `func (ins *Instance) Deactivate()`

**签名：** `func (ins *Instance) Deactivate()`

**参数：** 无

**返回值：** 无

**功能描述：** 停用角色，将 `Active` 设置为 `false`。

---

## 角色注册表

### `type RegistryInterface`

```go
type RegistryInterface interface {
    GetPrimary() *Instance
    Get(name string) (*Instance, bool)
    Count() int
    List() []*Instance
    GetAllActive() []*Instance
    Register(def RoleDef) error
    LoadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error
    ReloadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error
}
```

角色注册表的公开接口。业务层应依赖此接口而非具体实现，便于测试和扩展。

---

### `type Registry`

```go
type Registry struct {
    // 未导出字段（内部状态）
}
```

`Registry` 是 `RegistryInterface` 的具体实现。内部维护：
- `roles`：名称到实例的映射（`map[string]*Instance`）。
- `byType`：按角色类型分组的名称列表。
- `bus`：EventBus 引用，用于发布角色加载/卸载事件和注册触发器。
- `loader`：`Loader` 实例，用于文件加载。

所有方法均使用 `sync.RWMutex` 实现并发安全。

---

### `func NewRegistry(bus event.EventBus) RegistryInterface`

**签名：** `func NewRegistry(bus event.EventBus) RegistryInterface`

**参数：**
- `bus event.EventBus` — 事件总线实例（可为 `nil`，此时不发布事件、不注册触发器）

**返回值：** `RegistryInterface` — 新创建的角色注册表

**功能描述：** 创建并初始化空角色注册表。若 `bus` 非空，后续注册/注销角色时会通过 EventBus 发布事件，并自动订阅角色中定义的触发器。

---

### `func (r *Registry) LoadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error`

**签名：** `func (r *Registry) LoadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error`

**参数：**
- `wdb storage.WorkspaceDB` — 工作区数据库接口
- `workDir string` — 工作区根目录路径

**返回值：** `error` — 加载失败时返回错误

**功能描述：** 从 DB 和 `.catcode/roles/` 目录加载所有角色定义并逐一注册。内部调用 `loadDefs` 按三层优先级合并后注册。已存在的角色不会被移除（追加模式）。

---

### `func (r *Registry) ReloadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error`

**签名：** `func (r *Registry) ReloadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error`

**参数：**
- `wdb storage.WorkspaceDB` — 工作区数据库接口
- `workDir string` — 工作区根目录路径

**返回值：** `error` — 重新加载失败时返回错误

**功能描述：** 清空当前注册表中的所有角色，然后重新从 DB 和文件系统加载并注册。用于热重载场景（如用户修改角色文件后触发）。

---

### `func (r *Registry) Register(def RoleDef) error`

**签名：** `func (r *Registry) Register(def RoleDef) error`

**参数：**
- `def RoleDef` — 待注册的角色定义

**返回值：** `error` — 校验失败或注册失败时返回错误

**功能描述：** 校验并注册角色。执行流程：
1. 调用 `def.Validate()` 校验定义合法性。
2. 创建 `Instance` 并激活。
3. 写入 `roles` 映射和 `byType` 索引。
4. 注册角色中定义的触发器到 EventBus。
5. 通过 EventBus 发布 `EventRoleLoaded` 事件。

---

### `func (r *Registry) Unregister(name string)`

**签名：** `func (r *Registry) Unregister(name string)`

**参数：**
- `name string` — 角色名称

**返回值：** 无

**功能描述：** 注销指定名称的角色。若角色不存在则不执行任何操作。注销后会停用实例，从索引中移除，并通过 EventBus 发布 `EventRoleUnloaded` 事件。

注意：此方法不在 `RegistryInterface` 接口中，为 `Registry` 具体类型的附加方法。

---

### `func (r *Registry) Get(name string) (*Instance, bool)`

**签名：** `func (r *Registry) Get(name string) (*Instance, bool)`

**参数：**
- `name string` — 角色名称

**返回值：**
- `*Instance` — 角色实例（不存在时为 `nil`）
- `bool` — 是否存在该角色

**功能描述：** 按名称查找角色实例。使用读锁保证并发安全。

---

### `func (r *Registry) GetByType(typ RoleType) []*Instance`

**签名：** `func (r *Registry) GetByType(typ RoleType) []*Instance`

**参数：**
- `typ RoleType` — 角色类型（`RoleAgent` 或 `RoleCompanion`）

**返回值：** `[]*Instance` — 该类型的所有角色实例列表

**功能描述：** 返回指定类型的所有角色实例。使用读锁保证并发安全。

注意：此方法不在 `RegistryInterface` 接口中。

---

### `func (r *Registry) GetPrimary() *Instance`

**签名：** `func (r *Registry) GetPrimary() *Instance`

**参数：** 无

**返回值：** `*Instance` — 主智能体实例，若不存在则返回 `nil`

**功能描述：** 查找并返回 `Mode == ModePrimary` 且已激活的角色实例。遍历所有注册角色进行匹配。

---

### `func (r *Registry) GetAllActive() []*Instance`

**签名：** `func (r *Registry) GetAllActive() []*Instance`

**参数：** 无

**返回值：** `[]*Instance` — 所有已激活的角色实例列表

**功能描述：** 返回所有 `Active == true` 的角色实例。使用读锁保证并发安全。

---

### `func (r *Registry) List() []*Instance`

**签名：** `func (r *Registry) List() []*Instance`

**参数：** 无

**返回值：** `[]*Instance` — 所有已激活的角色实例列表

**功能描述：** 等价于 `GetAllActive()`。返回所有已激活的角色。

---

### `func (r *Registry) Count() int`

**签名：** `func (r *Registry) Count() int`

**参数：** 无

**返回值：** `int` — 角色总数

**功能描述：** 返回当前注册表中所有角色的数量（包括未激活的角色）。

---

## 角色加载器

### `type EmbedFS`

```go
type EmbedFS interface {
    ReadFile(name string) ([]byte, error)
}
```

嵌入文件系统接口，兼容 Go 的 `embed.FS`。用于在编译时将内置角色定义打包到二进制中。

---

### `type Loader`

```go
type Loader struct {
    // 未导出字段
}
```

`Loader` 从文件系统和嵌入文件系统加载角色定义文件（YAML 或 JSON）。

---

### `func NewLoader(paths []string) *Loader`

**签名：** `func NewLoader(paths []string) *Loader`

**参数：**
- `paths []string` — 搜索路径列表（文件系统目录路径）

**返回值：** `*Loader` — 新创建的加载器

**功能描述：** 创建仅基于文件系统的角色加载器。`paths` 为空时不会扫描任何目录。

---

### `func NewLoaderWithEmbed(paths []string, embedFS EmbedFS) *Loader`

**签名：** `func NewLoaderWithEmbed(paths []string, embedFS EmbedFS) *Loader`

**参数：**
- `paths []string` — 搜索路径列表
- `embedFS EmbedFS` — 嵌入文件系统实例（通常由 `//go:embed` 提供）

**返回值：** `*Loader` — 新创建的加载器

**功能描述：** 创建同时支持嵌入文件系统和外部文件系统的角色加载器。嵌入文件系统中的角色具有最低优先级，可被外部文件系统中的同名角色覆盖。

---

### `func (l *Loader) SetPaths(paths []string)`

**签名：** `func (l *Loader) SetPaths(paths []string)`

**参数：**
- `paths []string` — 新的搜索路径列表

**返回值：** 无

**功能描述：** 动态设置或更新加载器的搜索路径。

---

### `func (l *Loader) Discover() ([]RoleDef, error)`

**签名：** `func (l *Loader) Discover() ([]RoleDef, error)`

**参数：** 无

**返回值：**
- `[]RoleDef` — 发现的所有角色定义列表
- `error` — 扫描过程中遇到的错误（非致命错误会被忽略）

**功能描述：** 扫描所有配置的路径，按优先级加载角色定义：
1. 从嵌入文件系统加载（最低优先级）：预置角色文件名如 `architect.yaml`、`explore.yaml`、`plan.yaml`、`general.yaml`、`reviewer.yaml`、`verifier.yaml`、`lean4.yaml`。
2. 遍历所有搜索路径，逐个加载角色文件。
3. 同名角色按优先级覆盖：文件系统 > 嵌入。

解析失败的文件会被跳过（静默忽略），不影响其他角色的加载。

---

### `func (l *Loader) Load(path string) (RoleDef, error)`

**签名：** `func (l *Loader) Load(path string) (RoleDef, error)`

**参数：**
- `path string` — 角色定义文件的完整路径

**返回值：**
- `RoleDef` — 解析后的角色定义
- `error` — 读取或解析失败时返回错误

**功能描述：** 加载并解析单个角色定义文件。支持 `.json`、`.yaml`、`.yml` 三种扩展名。解析流程：
1. 读取文件内容。
2. 根据扩展名选择 JSON 反序列化或 yaml.v3 解析。
3. 设置 `SourcePath` 和 `LoadedAt` 元数据。
4. 调用 `Validate()` 校验定义合法性。

---

### `func (l *Loader) Reload(path string) (RoleDef, error)`

**签名：** `func (l *Loader) Reload(path string) (RoleDef, error)`

**参数：**
- `path string` — 角色定义文件的完整路径

**返回值：**
- `RoleDef` — 重新解析后的角色定义
- `error` — 读取或解析失败时返回错误

**功能描述：** 等价于 `Load(path)`。用于热重载场景，支持在运行时重新读取角色文件。

---

## YAML 解析

### `func ParseYAML(content string) (RoleDef, error)`

**签名：** `func ParseYAML(content string) (RoleDef, error)`

**参数：**
- `content string` — YAML 格式的字符串内容

**返回值：**
- `RoleDef` — 解析后的角色定义
- `error` — 解析失败时返回错误

**功能描述：** 使用 `gopkg.in/yaml.v3` 标准库解析 YAML 内容为 `RoleDef`。解析后对 nil 的 map 和 slice 字段（如 `Permission`、`Tools`、`Triggers`、`States`、`Persona`）补充零值默认值（空 map 或空切片），确保与原手写解析器行为一致。这是公开 API，允许外部调用者直接解析 YAML 内容而无需写入文件。

---

### 解析器实现说明

角色文件的 YAML 解析已改用 `gopkg.in/yaml.v3` 标准库实现，不再使用手写解析器。解析后会自动填充 nil 字段的零值默认值，行为与原解析器保持一致。

---

## 工具函数

### `func BuildFullModelName(m ModelConfig) string`

**签名：** `func BuildFullModelName(m ModelConfig) string`

**参数：**
- `m ModelConfig` — 模型配置

**返回值：** `string` — 完整的模型标识字符串

**功能描述：** 将模型配置组合为 `"provider:modelname"` 格式的字符串。若 `Provider` 为空，则仅返回 `Name`。

---

### `func WatchUserRoles(workDir string, onChange func()) error`

**签名：** `func WatchUserRoles(workDir string, onChange func()) error`

**参数：**
- `workDir string` — 工作区根目录
- `onChange func()` — 文件变化时的回调函数

**返回值：** `error` — 启动监视失败时返回错误

**功能描述：** 监视 `.catcode/roles/` 目录变化。当该目录下任意文件发生变更时，调用 `onChange` 回调。适用于热重载场景，通常回调中调用 `Registry.ReloadFromWorkspace`。

内部使用 `loader.NewDirWatcher` 实现，轮询间隔为 3 秒。

**所在文件：** `watcher.go`

---

## 包内函数（不导出）

以下是包内部使用的辅助函数，不对外暴露：

| 函数 | 文件 | 用途 |
|------|------|------|
| `mergeAgentDefs(dbDefs, fileDefs)` | `merge.go` | 按三层优先级合并 DB 和文件定义 |
| `discoverUserRoleFiles(workDir)` | `role_registry.go:109` | 扫描 `.catcode/roles/` 目录 |
| `loadDefs(wdb, workDir)` | `role_registry.go:91` | 从 DB + 文件系统加载合并后的定义 |
| `registerTriggers(inst)` | `role_registry.go:259` | 向 EventBus 注册角色的触发器 |
| `loadEmbedded()` | `role.go` | 从嵌入 FS 加载内置角色 |
| `parseBytes(data, filename)` | `role.go` | 按扩展名分发解析 |
| `isRoleFile(path)` | `role_parser.go:12` | 判断文件是否为角色定义文件 |
| `mergeRoles(roles, newDef)` | `role_parser.go:18` | 覆盖同名角色（用于 Loader.Discover） |

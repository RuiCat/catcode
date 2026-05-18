# data/storage — SQLite 持久化层

## 包概述

`data/storage` 包提供基于 SQLite 的工作区数据持久化层。每个工作区（`.catcode/`）维护一个独立的 `data.db` 数据库文件，通过 WAL 模式支持并发读写。数据库包含 9 张核心表，经过 v1→v6 共 6 个版本的 schema 演进。

### 9 张数据库表

| 表名 | 用途 |
|------|------|
| `schema_version` | 数据库版本管理 |
| `settings` | 工作区配置（键值对模型） |
| `agent_definitions` | 智能体定义（内置/用户自定义） |
| `conversations` | 对话会话 |
| `messages` | 对话消息 |
| `memory` | 长期记忆条目（多级索引） |
| `context_snapshots` | 上下文压缩快照 |
| `scheduled_tasks` | 周期任务 |
| `error_logs` | 错误日志 |

### Schema 迁移历史

| 版本 | 变更内容 |
|------|----------|
| v1 | 初始 schema：9 张表 |
| v2 | memory 表添加 `description`、`memory_type` 字段及多级索引 |
| v3 | memory 表新增 `scope` 字段（global / workspace） |
| v4 | memory 表唯一约束从 `UNIQUE(key)` 修正为 `UNIQUE(scope, key)`（重建表迁移） |
| v5 | messages 表新增 `reasoning_content` 和 `enabled` 列 |
| v6 | 新增 `error_logs` 表 |

### WAL 模式配置

```
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -20000;       -- 20MB 缓存
PRAGMA synchronous = NORMAL;
PRAGMA mmap_size = 268435456;     -- 256MB 内存映射
```

连接池配置：`MaxOpenConns=5`，`MaxIdleConns=2`，支持嵌套查询和并发读。

---

## 核心接口

### `WorkspaceDB` 接口

定义于 `workspace.go`，是工作区数据库所有操作的核心抽象。外部代码通过此接口访问数据，无需关心底层 `sql.DB` 实现。

```go
type WorkspaceDB interface {
    // Settings
    GetSetting(key string) (value, valueType string, err error)
    SetSetting(key, value, valueType, source string) error
    GetAllSettingsFlattened() (map[string]any, error)
    GetAllSettingsMap() map[string]SettingEntry
    Seed() error

    // Conversations
    SaveConversation(conv *ConversationRow, messages []*MessageRow) error
    LoadConversation(id string) (*ConversationRow, []*MessageRow, error)
    ListConversations() ([]ConversationInfo, error)

    // Memory
    SetMemory(scope, key, content, description, memoryType, tags string, importance int) error
    GetMemory(scope, key string) (*MemoryEntry, error)
    ScanMemoryHeaders(scope string) ([]*MemoryHeader, error)
    FindRelevantMemories(scope, query string, limit int) ([]*MemoryEntry, error)
    DeleteMemory(scope, key string) error

    // Context Snapshots
    CreateSnapshot(convID, label, messagesJSON, summary string, tokenCount int) error

    // Error Logs
    LogError(category, severity, message, stackTrace, source, convID string) error

    // Agents
    GetAllAgentDefinitions() ([]*AgentRow, error)

    // Scheduled Tasks
    MarkTaskRun(id int64) error
    CreateScheduledTask(name, description string, intervalSec int) (*ScheduledTask, error)
    DeleteScheduledTask(id int64) error
    UpdateScheduledTask(id int64, name, description string, intervalSec int, enabled bool) error
    ListScheduledTasks() ([]*ScheduledTask, error)

    // Internal
    DB() *sql.DB
    Close() error
}
```

### `MemoryService` 接口

定义于 `memory_service.go`。在 `WorkspaceDB.memory` 表之上提供聚合层，包含内存缓存、索引构建和智能记忆选择功能。按 `scope` 字段区分全局（global）和工作区（workspace）记忆。

```go
type MemoryService interface {
    SetMemory(scope MemoryScope, key, content, description, memoryType, tags string, importance int) error
    GetMemory(scope MemoryScope, key string) (*MemoryEntry, error)
    DeleteMemory(scope MemoryScope, key string) error
    BuildIndex(contextHint string) string
    Search(query string, scope MemoryScope, limit int) []ScopedMemory
    ListHeaders(scope MemoryScope) []ScopeHeader
    SetMemorySelector(sel MemorySelector)
    InvalidateCache()
}
```

**缓存机制说明**：`MemoryService` 内部维护 `cachedIndex` 字符串缓存。每次 `SetMemory` 或 `DeleteMemory` 操作会将 `cacheDirty` 标记为 `true`，下一次 `BuildIndex` 调用时重新扫描数据库并重建索引文本。`InvalidateCache()` 可外部强制失效缓存。

---

## 类型定义

### `ConversationRow` — 会话记录（`conversations.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `string` | 主键，UUID |
| `Model` | `string` | 使用的模型名称 |
| `SystemPrompt` | `string` | 系统提示词 |
| `Summary` | `string` | 对话摘要 |
| `CompressThreshold` | `int` | 压缩阈值（token 数） |
| `MetadataJSON` | `string` | JSON 元数据 |
| `MessageCount` | `int` | 消息数量 |
| `TokenCount` | `int` | 总 token 数 |
| `CreatedAt` | `time.Time` | 创建时间 |
| `UpdatedAt` | `time.Time` | 更新时间 |

### `ConversationInfo` — 会话简要信息（`conversations.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `string` | 会话 ID |
| `Model` | `string` | 使用的模型 |
| `MessageCount` | `int` | 消息数量 |
| `CreatedAt` | `time.Time` | 创建时间 |
| `UpdatedAt` | `time.Time` | 更新时间 |

### `MessageRow` — 消息记录（`conversations.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `int64` | 自增主键 |
| `ConversationID` | `string` | 所属会话 ID |
| `Seq` | `int` | 消息序号 |
| `Role` | `string` | 角色（user / assistant / tool） |
| `Content` | `string` | 消息内容 |
| `Name` | `string` | 名称 |
| `ToolCallID` | `string` | 工具调用 ID |
| `ToolCallsJSON` | `string` | 工具调用 JSON |
| `ReasoningContent` | `string` | 推理内容（v5 新增） |
| `Enabled` | `bool` | 是否启用（v5 新增） |
| `CreatedAt` | `time.Time` | 创建时间 |

### `AgentRow` / `AgentDef` — 智能体定义（`agents.go`）

`AgentDef` 是 `AgentRow` 的类型别名。

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `int64` | 自增主键 |
| `Name` | `string` | 唯一名称 |
| `DisplayName` | `string` | 显示名称 |
| `Type` | `string` | 类型："agent" 或 "companion" |
| `Mode` | `string` | 模式："primary" / "subagent" / "background" |
| `Description` | `string` | 描述 |
| `SystemPrompt` | `string` | 系统提示词 |
| `ModelProvider` | `string` | 模型提供商 |
| `ModelName` | `string` | 模型名称 |
| `ModelTemperature` | `*float64` | 模型温度（可为 nil） |
| `ThinkingEnabled` | `bool` | 是否启用思考 |
| `ThinkingBudgetTokens` | `*int` | 思考预算 token（可为 nil） |
| `ModelLimitContext` | `*int` | 上下文长度限制（可为 nil） |
| `ModelLimitOutput` | `*int` | 输出长度限制（可为 nil） |
| `PermissionJSON` | `string` | 权限配置 JSON |
| `ToolsJSON` | `string` | 工具列表 JSON |
| `TriggersJSON` | `string` | 触发器配置 JSON |
| `PersonaJSON` | `string` | 角色人设 JSON |
| `StatesJSON` | `string` | 状态列表 JSON |
| `Temperature` | `float64` | 温度（角色顶层覆盖） |
| `Source` | `string` | 来源："builtin" / "user_db" / "user_file" |
| `SourcePath` | `string` | 来源路径 |
| `Version` | `int` | 版本号 |
| `Enabled` | `bool` | 是否启用 |

### `MemoryEntry` — 记忆条目（`memory.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `int64` | 自增主键 |
| `Scope` | `string` | 范围："global" 或 "workspace" |
| `Key` | `string` | 唯一键（与 scope 组成联合唯一约束） |
| `Content` | `string` | 完整内容 |
| `Description` | `string` | 简短描述（用于索引扫描） |
| `MemoryType` | `string` | 类型：user / feedback / project / reference |
| `Tags` | `string` | 标签 |
| `Importance` | `int` | 重要性评分 |
| `AccessCount` | `int` | 访问次数 |
| `CreatedAt` | `time.Time` | 创建时间 |
| `UpdatedAt` | `time.Time` | 更新时间 |
| `AccessedAt` | `time.Time` | 最近访问时间 |

### `MemoryHeader` — 记忆索引头部（`memory.go`）

轻量级结构，用于索引扫描时不加载完整内容。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Key` | `string` | 记忆键 |
| `Description` | `string` | 描述 |
| `MemoryType` | `string` | 类型 |
| `Importance` | `int` | 重要性 |
| `AgeDays` | `int` | 距今天数 |
| `MtimeMs` | `int64` | 最后修改时间（毫秒时间戳） |

### `MemoryScope` — 记忆范围类型（`memory_service.go`）

```go
type MemoryScope string

const (
    ScopeGlobal    MemoryScope = "global"    // 项目全局记忆
    ScopeWorkspace MemoryScope = "workspace" // 工作区独立记忆
)
```

### `MemorySelector` — 智能记忆选择函数类型（`memory_service.go`）

```go
type MemorySelector func(wdb WorkspaceDB, context string, maxResults int) ([]*MemoryEntry, error)
```

由上层的 compact 模块注入，用于根据上下文相关性评分筛选最相关的记忆条目。

### `ScopedMemory` — 带范围的记忆条目（`memory_service.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `Scope` | `MemoryScope` | 记忆范围 |
| `Memory` | `*MemoryEntry` | 记忆条目指针 |

### `ScopeHeader` — 带范围的索引头部（`memory_service.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `Scope` | `MemoryScope` | 记忆范围 |
| `Headers` | `[]*MemoryHeader` | 该范围下的索引头部列表 |

### `ScheduledTask` — 周期任务（`tasks.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `int64` | 自增主键 |
| `Name` | `string` | 任务名称 |
| `Description` | `string` | 任务描述 |
| `IntervalSeconds` | `int` | 执行间隔（秒） |
| `Enabled` | `bool` | 是否启用 |
| `LastRun` | `*time.Time` | 上次运行时间（可为 nil） |
| `NextRun` | `*time.Time` | 下次计划运行时间（可为 nil） |
| `CreatedAt` | `time.Time` | 创建时间 |
| `UpdatedAt` | `time.Time` | 更新时间 |

### `ErrorLogEntry` — 错误日志条目（`error_logs.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `int64` | 自增主键 |
| `Category` | `string` | 错误类别（API / 工具 / 权限 / LLM / 网络 / 内部） |
| `Severity` | `string` | 严重级别：error / warning / info |
| `Message` | `string` | 错误消息 |
| `StackTrace` | `string` | 堆栈跟踪 |
| `Source` | `string` | 来源（architect / subagent / llm / startup） |
| `ConversationID` | `string` | 关联会话 ID |
| `CreatedAt` | `time.Time` | 创建时间 |

### `SnapshotInfo` — 上下文快照简要信息（`context_snapshots.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `ID` | `int64` | 自增主键 |
| `ConversationID` | `string` | 所属会话 ID |
| `Label` | `string` | 快照标签 |
| `TokenCount` | `int` | Token 数量 |
| `CreatedAt` | `time.Time` | 创建时间 |

### `SettingEntry` — 配置条目（`flatten.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `Key` | `string` | 配置键（使用 "." 分隔层级） |
| `Value` | `string` | 配置值（字符串形式） |
| `ValueType` | `string` | 值类型：string / int / float / bool / json |
| `Source` | `string` | 来源：builtin / user |
| `Description` | `string` | 描述说明 |

### `InstructionFiles` — 项目指令文件集合（`instructions.go`）

| 字段 | 类型 | 说明 |
|------|------|------|
| `AGENTS` | `string` | AGENTS.md 文件内容 |
| `CLAUDE` | `string` | CLAUDE.md 文件内容 |
| `Catcode` | `string` | .catcode/instructions.md 文件内容 |
| `Root` | `string` | 项目根目录（找到 .git 的目录） |

---

## 构造函数与顶层函数

### `OpenWorkspace` — 打开或创建工作区数据库（`workspace.go`）

```go
func OpenWorkspace(workDir string) (WorkspaceDB, bool, error)
```

**参数**：
- `workDir` — 工作目录路径，数据库文件将位于 `<workDir>/.catcode/data.db`

**返回值**：
- `WorkspaceDB` — 工作区数据库实例
- `bool` — `true` 表示数据库首次创建（`isNew` 标志）
- `error` — 打开/初始化失败时的错误

**功能**：确保 `.catcode/` 目录存在，以 WAL 模式打开 SQLite 数据库（busy_timeout=5000ms），配置连接池（MaxOpenConns=5），执行 PRAGMA 和 schema 迁移。

---

### `NewMemoryService` — 创建记忆服务（`memory_service.go`）

```go
func NewMemoryService(workspace WorkspaceDB) MemoryService
```

**参数**：
- `workspace` — WorkspaceDB 实例，可为 nil（未初始化工作区时）

**返回值**：`MemoryService` 接口实现

**功能**：创建记忆聚合服务，封装 WorkspaceDB 的 memory 表操作，提供索引缓存和智能选择能力。

---

### `LoadInstructions` — 加载项目指令文件（`instructions.go`）

```go
func LoadInstructions(workDir string) *InstructionFiles
```

**参数**：
- `workDir` — 工作目录路径，函数会自动向上搜索 `.git` 目录以确定项目根

**返回值**：`*InstructionFiles` 包含 AGENTS.md、CLAUDE.md 和 .catcode/instructions.md 的内容

**功能**：从 workDir 向上搜索 `.git` 目录确定项目根，然后读取所有支持的指令文件。每个文件最多读取 8000 字符（以 rune 计）。

---

### `ToJSON` — 序列化为 JSON 字符串（`conversations.go`）

```go
func ToJSON(v any) string
```

**参数**：`v` — 任意类型的值

**返回值**：JSON 字符串（序列化失败时返回空字符串）

---

### `FromJSON` — 从 JSON 字符串反序列化（`conversations.go`）

```go
func FromJSON(raw string, target any) error
```

**参数**：
- `raw` — JSON 字符串
- `target` — 反序列化目标（指针）

**返回值**：反序列化错误

---

### `UnflattenSettings` — 扁平化配置重组为嵌套 map（`flatten.go`）

```go
func UnflattenSettings(entries map[string]SettingEntry) map[string]any
```

**参数**：`entries` — 扁平化的 SettingEntry 映射表（键名用 "." 分隔层级）

**返回值**：嵌套 `map[string]any`，还原原始层级结构

**功能**：将数据库中按 `"key1.key2"` 格式存储的扁平配置，重组成嵌套的 `map[string]any` 树结构。根据 `ValueType` 自动解析值类型（int / float / bool / json / string）。

---

## WorkspaceDB 接口方法

以下方法分别实现于 `settings.go`、`conversations.go`、`memory.go`、`tasks.go`、`error_logs.go`、`context_snapshots.go`、`agents.go`、`workspace.go`。

### 设置类（`settings.go`）

#### `GetSetting`

```go
func (w *workspaceDBImpl) GetSetting(key string) (value, valueType string, err error)
```

**参数**：`key` — 配置键

**返回值**：配置值、值类型字符串、错误

**功能**：从 `settings` 表按主键查询单个配置项。

---

#### `SetSetting`

```go
func (w *workspaceDBImpl) SetSetting(key, value, valueType, source string) error
```

**参数**：
- `key` — 配置键
- `value` — 配置值
- `valueType` — 值类型（string / int / float / bool / json）
- `source` — 来源标识

**返回值**：错误

**功能**：写入单个配置（INSERT ... ON CONFLICT DO UPDATE），使用写锁保护。

---

#### `GetAllSettingsFlattened`

```go
func (w *workspaceDBImpl) GetAllSettingsFlattened() (map[string]any, error)
```

**返回值**：`map[string]any` 键值对映射（已根据 value_type 解析为正确类型）

**功能**：获取所有配置条目，将字符串值根据 value_type 解析为原始类型后返回。等同于先调用 `GetAllSettings` 再对每条调用 `parseValue`。

---

#### `GetAllSettingsMap`

```go
func (w *workspaceDBImpl) GetAllSettingsMap() map[string]SettingEntry
```

**返回值**：`map[string]SettingEntry`，key 为配置键

**功能**：以 SettingEntry 结构形式返回所有配置，供 `UnflattenSettings` 使用。出错时返回空 map。

---

#### `Seed`

```go
func (w *workspaceDBImpl) Seed() error
```

**返回值**：错误

**功能**：仅在数据库首次创建后调用。检查 `settings` 表是否有数据：若为空则写入默认配置（`seedDefaultSettings`）和默认角色定义（`seedDefaultRoles`）。若有数据（非首次打开），则执行旧格式 model name 迁移（`migrateSettingsModelName`，将无 provider 前缀的 model key 补全为 `deepseek:xxx` 格式）。

---

### 会话类（`conversations.go`）

#### `SaveConversation`

```go
func (w *workspaceDBImpl) SaveConversation(conv *ConversationRow, messages []*MessageRow) error
```

**参数**：
- `conv` — 会话记录（ID 为主键）
- `messages` — 消息列表

**返回值**：错误

**功能**：在事务中执行：先 INSERT ... ON CONFLICT DO UPDATE 保存/更新会话，再 DELETE 该会话的旧消息后批量 INSERT 新消息。

---

#### `LoadConversation`

```go
func (w *workspaceDBImpl) LoadConversation(id string) (*ConversationRow, []*MessageRow, error)
```

**参数**：`id` — 会话 ID

**返回值**：会话记录、消息列表（按 seq ASC 排序）、错误

**功能**：先查询 conversations 表获取会话记录，再查询 messages 表获取关联消息，按 `seq ASC` 排序。

---

#### `ListConversations`

```go
func (w *workspaceDBImpl) ListConversations() ([]ConversationInfo, error)
```

**返回值**：ConversationInfo 切片（最多 50 条，按 updated_at DESC 排序）

**功能**：列出最近 50 个会话的简要信息。

---

### 记忆类（`memory.go`）

#### `SetMemory`

```go
func (w *workspaceDBImpl) SetMemory(scope, key, content, description, memoryType, tags string, importance int) error
```

**参数**：
- `scope` — 范围："global" 或 "workspace"
- `key` — 唯一键
- `content` — 完整内容
- `description` — 简短描述
- `memoryType` — 类型：user / feedback / project / reference
- `tags` — 标签
- `importance` — 重要性评分

**返回值**：错误

**功能**：INSERT ... ON CONFLICT(scope, key) DO UPDATE，写入或更新记忆条目。

---

#### `GetMemory`

```go
func (w *workspaceDBImpl) GetMemory(scope, key string) (*MemoryEntry, error)
```

**参数**：
- `scope` — 范围
- `key` — 唯一键

**返回值**：MemoryEntry 指针、错误

**功能**：按 (scope, key) 查询记忆条目。使用读锁 `w.mu.RLock()` 保护查询操作。

查询成功后启动 goroutine 异步更新 `access_count` 和 `accessed_at`：
- goroutine 内部使用 **写锁 `w.mu.Lock()`** 保护更新操作，避免与其他写操作竞争
- UPDATE SQL 包含 `WHERE scope = ? AND key = ?` 精确限定更新目标行
- 访问计数更新失败静默忽略，不影响主查询流程

---

#### `ScanMemoryHeaders`

```go
func (w *workspaceDBImpl) ScanMemoryHeaders(scope string) ([]*MemoryHeader, error)
```

**参数**：`scope` — 范围过滤（空字符串表示不按 scope 过滤）

**返回值**：MemoryHeader 切片（最多 100 条，按 importance DESC, updated_at DESC 排序）

**功能**：轻量级扫描记忆索引，不加载完整 `content` 字段。使用 `julianday()` 计算 age_days，`strftime('%s')` 计算 mtime_ms。

---

#### `FindRelevantMemories`

```go
func (w *workspaceDBImpl) FindRelevantMemories(scope, query string, limit int) ([]*MemoryEntry, error)
```

**参数**：
- `scope` — 范围过滤（空字符串表示不按 scope 过滤）
- `query` — 搜索关键词
- `limit` — 返回数量上限

**返回值**：MemoryEntry 切片

**功能**：按关键词模糊搜索记忆（匹配 content、description、tags），按 importance DESC, updated_at DESC 排序，返回完整 MemoryEntry。

---

#### `DeleteMemory`

```go
func (w *workspaceDBImpl) DeleteMemory(scope, key string) error
```

**参数**：
- `scope` — 范围
- `key` — 唯一键

**返回值**：错误

**功能**：按 (scope, key) 删除记忆条目。

---

### 快照类（`context_snapshots.go`）

#### `CreateSnapshot`

```go
func (w *workspaceDBImpl) CreateSnapshot(convID, label, messagesJSON, summary string, tokenCount int) error
```

**参数**：
- `convID` — 会话 ID
- `label` — 快照标签
- `messagesJSON` — 消息列表 JSON
- `summary` — 对话摘要
- `tokenCount` — Token 数量

**返回值**：错误

**功能**：向 `context_snapshots` 表插入一条上下文压缩快照。

---

### 错误日志类（`error_logs.go`）

#### `LogError`

```go
func (w *workspaceDBImpl) LogError(category, severity, message, stackTrace, source, convID string) error
```

**参数**：
- `category` — 错误类别
- `severity` — 严重级别：error / warning / info
- `message` — 错误消息
- `stackTrace` — 堆栈跟踪
- `source` — 来源：architect / subagent / llm / startup
- `convID` — 关联会话 ID

**返回值**：错误

**功能**：向 `error_logs` 表插入一条错误日志。

---

### 智能体类（`agents.go`）

#### `GetAllAgentDefinitions`

```go
func (w *workspaceDBImpl) GetAllAgentDefinitions() ([]*AgentRow, error)
```

**返回值**：AgentRow 切片（仅返回 `enabled=1` 的记录，primary 模式排在前面）

**功能**：查询所有已启用的智能体定义。

---

### 周期任务类（`tasks.go`）

#### `CreateScheduledTask`

```go
func (w *workspaceDBImpl) CreateScheduledTask(name, description string, intervalSec int) (*ScheduledTask, error)
```

**参数**：
- `name` — 任务名称
- `description` — 任务描述
- `intervalSec` — 执行间隔（秒）

**返回值**：新创建的 ScheduledTask（包含自增 ID）

---

#### `UpdateScheduledTask`

```go
func (w *workspaceDBImpl) UpdateScheduledTask(id int64, name, description string, intervalSec int, enabled bool) error
```

**参数**：
- `id` — 任务 ID
- `name` — 新名称
- `description` — 新描述
- `intervalSec` — 新执行间隔（秒）
- `enabled` — 是否启用

**返回值**：错误

---

#### `DeleteScheduledTask`

```go
func (w *workspaceDBImpl) DeleteScheduledTask(id int64) error
```

**参数**：`id` — 任务 ID

**返回值**：错误

---

#### `ListScheduledTasks`

```go
func (w *workspaceDBImpl) ListScheduledTasks() ([]*ScheduledTask, error)
```

**返回值**：ScheduledTask 切片（按 enabled DESC, created_at DESC 排序）

---

#### `MarkTaskRun`

```go
func (w *workspaceDBImpl) MarkTaskRun(id int64) error
```

**参数**：`id` — 任务 ID

**返回值**：错误

**功能**：将任务的 `last_run` 更新为当前时间。

---

### 内部方法

#### `DB`

```go
func (w *workspaceDBImpl) DB() *sql.DB
```

**返回值**：底层 `*sql.DB`（供子 store 直接使用）

---

#### `Close`

```go
func (w *workspaceDBImpl) Close() error
```

**返回值**：错误

**功能**：关闭底层 `sql.DB` 连接。

---

## MemoryService 接口方法

全部定义于 `memory_service.go`，由 `memoryServiceImpl` 实现。

### `SetMemory`

```go
func (ms *memoryServiceImpl) SetMemory(scope MemoryScope, key, content, description, memoryType, tags string, importance int) error
```

委托到 `WorkspaceDB.SetMemory`。写入成功后标记 `cacheDirty=true` 使索引缓存失效。

---

### `GetMemory`

```go
func (ms *memoryServiceImpl) GetMemory(scope MemoryScope, key string) (*MemoryEntry, error)
```

委托到 `WorkspaceDB.GetMemory`。

---

### `DeleteMemory`

```go
func (ms *memoryServiceImpl) DeleteMemory(scope MemoryScope, key string) error
```

委托到 `WorkspaceDB.DeleteMemory`。删除成功后标记 `cacheDirty=true`。

---

### `BuildIndex`

```go
func (ms *memoryServiceImpl) BuildIndex(contextHint string) string
```

**参数**：
- `contextHint` — 可选的上下文提示（如最近对话摘要），用于智能筛选相关记忆；传空字符串时使用简单头部扫描

**返回值**：格式化的索引文本，结构为：

```
[记忆索引]
🌐 项目全局记忆:
  • key | description | 📋 ★importance
📁 工作区记忆:
  • key | description | 👤 ★importance
---
使用 memory_get(scope, key) 获取完整记忆，memory_search(query) 搜索记忆
```

**缓存逻辑**：若缓存有效（`cacheDirty=false`）且 `cachedIndex` 非空，直接返回缓存。否则根据是否有 `contextHint` 决定：有上下文提示时通过注入的 `MemorySelector` 智能筛选（最多 30 条）；无上下文提示时通过 `ScanMemoryHeaders` 按重要性排序。每个 scope 在索引中最多显示 25 条，每条 description 最多 80 字符。构建完成后更新缓存。

---

### `Search`

```go
func (ms *memoryServiceImpl) Search(query string, scope MemoryScope, limit int) []ScopedMemory
```

**参数**：
- `query` — 搜索关键词
- `scope` — 范围过滤（`"all"` 表示搜索所有范围）
- `limit` — 返回数量上限

**返回值**：`[]ScopedMemory` 带 scope 标签的记忆条目列表

**功能**：调用 `WorkspaceDB.FindRelevantMemories` 进行模糊搜索，结果包装为 `ScopedMemory`。

---

### `ListHeaders`

```go
func (ms *memoryServiceImpl) ListHeaders(scope MemoryScope) []ScopeHeader
```

**参数**：`scope` — 范围过滤（`"all"` 表示返回所有范围）

**返回值**：`[]ScopeHeader`，每个 ScopeHeader 包含一个 scope 下的所有 MemoryHeader

---

### `SetMemorySelector`

```go
func (ms *memoryServiceImpl) SetMemorySelector(sel MemorySelector)
```

**参数**：`sel` — 智能记忆选择函数

**功能**：注入智能记忆选择器，用于 `BuildIndex` 中根据上下文相关性评分筛选记忆。用于打破上层 compact 模块与本包的 import cycle。

---

### `InvalidateCache`

```go
func (ms *memoryServiceImpl) InvalidateCache()
```

**功能**：强制失效索引缓存，下一次 `BuildIndex` 将重新扫描数据库。

---

## Schema 迁移函数

定义于 `schema.go`。

### `initSchema`

```go
func initSchema(db *sql.DB) error
```

**参数**：`db` — 数据库连接

**功能**：执行完整的 `schemaV1` DDL 创建所有 9 张表及索引，并将当前版本 `currentSchemaVersion(6)` 写入 `schema_version` 表。

---

### `migrateDB`

```go
func migrateDB(db *sql.DB) error
```

**参数**：`db` — 数据库连接

**功能**：检查 `schema_version` 表是否存在。若不存在（新数据库）则调用 `initSchema`；若存在则检查当前版本并按顺序执行增量迁移：

| 触发条件 | 迁移内容 |
|----------|----------|
| `version < 2` | 使用 `columnExists(db, "memory", ...)` 检查列是否存在，若不存在则 ALTER TABLE memory 添加 `description`、`memory_type` 列；创建 `idx_memory_type`、`idx_memory_importance` 索引 |
| `version < 3` | 使用 `columnExists(db, "memory", "scope")` 检查列是否存在，若不存在则 ALTER TABLE memory 添加 `scope` 列；创建 `idx_memory_scope` 索引 |
| `version < 4` | 调用 `migrateMemoryUniqueV4` 将 UNIQUE(key) 改为 UNIQUE(scope, key)（重建表迁移） |
| `version < 5` | 使用 `columnExists(db, "messages", "reasoning_content")` 和 `columnExists(db, "messages", "enabled")` 检查列是否存在，若不存在则 ALTER TABLE messages 添加 `reasoning_content`、`enabled` 列 |
| `version < 6` | 创建 `error_logs` 表及索引 |

---

### `SetSchemaVersion`

```go
func SetSchemaVersion(db *sql.DB, version int, description string) error
```

**参数**：
- `db` — 数据库连接
- `version` — 版本号
- `description` — 版本描述

**功能**：向 `schema_version` 表插入一条版本记录。

---

### `columnExists`

```go
func columnExists(db *sql.DB, table, column string) (bool, error)
```

**参数**：
- `db` — 数据库连接
- `table` — 表名
- `column` — 列名

**返回值**：
- `bool` — 列存在返回 `true`
- `error` — 查询失败时返回错误

**功能**：使用 `PRAGMA table_info` 查询指定表是否存在某列。内部执行 `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`，计数大于 0 表示列存在。

> **取代字符串错误匹配**：v1→v2、v2→v3、v4→v5 三处迁移代码已改用 `columnExists` 替代旧的 "ALTER TABLE 失败后匹配错误字符串" 方式检测列是否存在。此方法更安全可靠，不依赖错误消息文本。

---

### `migrateMemoryUniqueV4`

```go
func migrateMemoryUniqueV4(db *sql.DB) error
```

**参数**：`db` — 数据库连接

**功能**：在事务中将 `memory` 表的唯一约束从 `UNIQUE(key)` 改为 `UNIQUE(scope, key)`。由于 SQLite 不支持 `ALTER TABLE DROP CONSTRAINT`，采用重建表策略：创建 `memory_new` 表（正确约束）→ 复制数据 → 删除旧表 → 重命名新表 → 重建全部索引。执行前会检查 `sqlite_master`，若已存在 `UNIQUE(scope, key)` 则跳过。

---

## 配置扁平化/重组函数

定义于 `flatten.go`。

### `flattenMap`

```go
func flattenMap(prefix string, data map[string]any) []SettingEntry
```

**参数**：
- `prefix` — 键名前缀（递归层级）
- `data` — 待扁平化的嵌套 map

**返回值**：`[]SettingEntry` 扁平化列表

**功能**：递归遍历嵌套 `map[string]any`，将叶节点转换为 `SettingEntry`。键名用 "." 分隔层级（如 `"providers.deepseek.base_url"`）。自动判断值类型：string → `"string"`，bool → `"bool"`，int → `"int"`，float64 调用 `autoType` 判断整数/浮点数，[]any/其它复合类型 → JSON 序列化为 `"json"`。

---

### `autoType`

```go
func autoType(v float64) string
```

**参数**：`v` — float64 值

**返回值**：`"int"` 或 `"float"`

**功能**：若 `v == float64(int64(v))` 则返回 `"int"`，否则返回 `"float"`。

---

### `setNested`

```go
func setNested(m map[string]any, parts []string, value any)
```

**参数**：
- `m` — 目标嵌套 map
- `parts` — 键路径分段（如 `["providers", "deepseek", "base_url"]`）
- `value` — 要设置的值

**功能**：在嵌套 map 中按路径设置值，自动创建中间层级 map。

---

### `parseValue`

```go
func parseValue(raw string, valueType string) any
```

**参数**：
- `raw` — 字符串形式的原始值
- `valueType` — 值类型标识

**返回值**：解析后的原始类型值

**功能**：根据 `valueType` 将字符串值解析为对应 Go 类型：
- `"int"` → `int64`
- `"float"` → `float64`
- `"bool"` → `bool`（`"true"` 或 `"1"` 为 true）
- `"json"` → 尝试 `json.Unmarshal`，失败则返回原始字符串
- 其他 → 原始字符串

---

## InstructionFiles 方法

定义于 `instructions.go`。

### `IsEmpty`

```go
func (i *InstructionFiles) IsEmpty() bool
```

**返回值**：`true` 当 AGENTS、CLAUDE、Catcode 三个字段全部为空字符串

---

### `FormatContext`

```go
func (i *InstructionFiles) FormatContext(maxChars int) string
```

**参数**：`maxChars` — 最大字符数（以 rune 计），<=0 时默认为 4000

**返回值**：格式化的 LLM 上下文字符串，格式为：

```
[项目指令文件]
📄 AGENTS.md:
<内容>
---
📄 CLAUDE.md:
<内容>
---
📄 .catcode/instructions.md:
<内容>
---
```

**功能**：按 AGENTS → CLAUDE → Catcode 顺序拼接文件内容，共享 `maxChars` 限制，超出部分截断并追加 `"...(截断)"`。

---

## 附加方法（不在 WorkspaceDB 接口内）

以下方法在 `workspaceDBImpl` 上导出，但不在 `WorkspaceDB` 接口中。它们可通过类型断言（`*workspaceDBImpl`）或直接引用访问（内部使用）。

### 会话（`conversations.go`）

#### `CreateConversation`

```go
func (w *workspaceDBImpl) CreateConversation(conv *ConversationRow) error
```

插入新会话（不含消息）。

#### `DeleteConversation`

```go
func (w *workspaceDBImpl) DeleteConversation(id string) error
```

删除会话及其所有关联消息（先显式删除 messages 表中的关联消息，再删除 conversations 表中的会话记录）。

#### `SearchConversations`

```go
func (w *workspaceDBImpl) SearchConversations(keyword string) ([]ConversationInfo, error)
```

按消息内容关键词搜索会话（LIKE 模糊匹配），最多返回 20 条。

---

### 智能体（`agents.go`）

#### `GetAgentByName`

```go
func (w *workspaceDBImpl) GetAgentByName(name string) (*AgentRow, error)
```

按 name 查询单个智能体定义。

#### `UpsertAgent`

```go
func (w *workspaceDBImpl) UpsertAgent(row *AgentRow) error
```

INSERT ... ON CONFLICT(name) DO UPDATE，更新时 `version` 字段自动 +1。

#### `DeleteAgent`

```go
func (w *workspaceDBImpl) DeleteAgent(name string) error
```

按 name 删除智能体定义。

---

### 设置（`settings.go`）

#### `GetAllSettings`

```go
func (w *workspaceDBImpl) GetAllSettings() (map[string]SettingEntry, error)
```

查询所有配置条目，返回 `map[string]SettingEntry`。

#### `BatchSetSettings`

```go
func (w *workspaceDBImpl) BatchSetSettings(entries []SettingEntry) error
```

在事务中批量写入配置项（INSERT ... ON CONFLICT DO UPDATE）。

#### `DeleteSetting`

```go
func (w *workspaceDBImpl) DeleteSetting(key string) error
```

按 key 删除单个配置项。

---

### 记忆（`memory.go`）

#### `ListMemory`

```go
func (w *workspaceDBImpl) ListMemory(scope string) ([]*MemoryEntry, error)
```

按 scope 过滤列出所有完整记忆（空字符串表示全部），按 importance DESC, updated_at DESC 排序。

---

### 错误日志（`error_logs.go`）

#### `GetErrorLogs`

```go
func (w *workspaceDBImpl) GetErrorLogs(limit int) ([]*ErrorLogEntry, error)
```

查询最近 N 条错误日志（按 created_at DESC）。

#### `GetErrorLogsByCategory`

```go
func (w *workspaceDBImpl) GetErrorLogsByCategory(category string, limit int) ([]*ErrorLogEntry, error)
```

按类别查询错误日志。

#### `CleanOldErrorLogs`

```go
func (w *workspaceDBImpl) CleanOldErrorLogs(days int) error
```

删除 N 天前的错误日志（使用 `datetime('now', '-N days')`）。

---

### 快照（`context_snapshots.go`）

#### `ListSnapshots`

```go
func (w *workspaceDBImpl) ListSnapshots(convID string) ([]*SnapshotInfo, error)
```

列出指定会话的所有快照（按 created_at DESC）。


---

## 安全模块

`data/storage/secrets.go` 提供 API Key 的 AES-256-GCM 加密保护。密钥从本机 hostname + uid 通过 SHA-256 派生，密文使用 base64(nonce + ciphertext) 格式。解密失败时自动回退到原始值以实现向后兼容。详见 [secrets.md](./secrets.md)。

# tool — 工具系统

## 包概述

`tool` 包实现工具注册与调度系统，是 catcode 智能体与外部世界交互的核心桥梁。包的设计借鉴 catai 的 Call/CallUpdate 双回调架构，兼容 OpenAI function calling 协议。

### 核心类型关系

```
Registry (工具注册中心，线程安全)
├── Tool × N (工具定义)
│   ├── FuncDef (函数签名)
│   ├── Call (主动回调，LLM 调用时触发)
│   │   └── ToolCallback = func(ctx, args) → (result, error)
│   └── CallUpdate (被动回调，LLM 响应后同步)
│       └── UpdateCallback = func(ctx, response)
│
PermissionChecker (权限检查器)
└── PermissionRule × N (权限规则)
    └── PermissionLevel (Allow / Ask / Deny)

Context → SessionID, WorkDir, ToolCallID, Permission, GuardReviewer
```

### Call/CallUpdate 双回调设计

| 回调 | 触发时机 | 用途 |
|------|---------|------|
| `Call` | LLM 主动调用工具时 | 执行工具逻辑，返回结构化结果 |
| `CallUpdate` | LLM 每次响应后（无论工具是否被调用） | 被动同步状态，如刷新文件快照、更新 LSP 缓存 |

`Registry.CallUpdateAll()` 在每轮 LLM 响应后遍历所有启用工具的 `CallUpdate` 回调，确保状态始终与外部环境一致。

---

## 类型定义

### Tool — 工具定义

兼容 OpenAI function calling 协议的完整工具定义。

```go
type Tool struct {
    Type     string  `json:"type"`     // 固定 "function"
    Function FuncDef `json:"function"` // 函数定义

    Call       ToolCallback   `json:"-"` // LLM 调用时触发
    CallUpdate UpdateCallback `json:"-"` // LLM 响应后被动同步
    Enable     bool           `json:"-"` // 是否启用

    cachedJSON []byte `json:"-"` // 预编码 JSON 缓存（零拷贝）
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Type` | `string` | 固定为 `"function"`，对应 OpenAI function calling 协议 |
| `Function` | `FuncDef` | 函数签名定义 |
| `Call` | `ToolCallback` | 主动调用回调，LLM `tool_calls` 触发执行 |
| `CallUpdate` | `UpdateCallback` | 被动同步回调，每次 LLM 响应后自动调用 |
| `Enable` | `bool` | 工具是否启用，注册时自动设为 `true` |

#### 方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `Update` | `func (t *Tool) Update() error` | 预编码工具为 JSON 并缓存到 `cachedJSON`，用于零拷贝输出 |
| `CachedJSON` | `func (t *Tool) CachedJSON() []byte` | 返回预编码的 JSON 字节切片 |

---

### FuncDef — 函数定义

```go
type FuncDef struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 函数名，全局唯一标识 |
| `Description` | `string` | 函数描述，供 LLM 理解用途 |
| `Parameters` | `json.RawMessage` | JSON Schema 格式的参数定义 |

---

### ToolCallback — 主动调用回调

```go
type ToolCallback func(ctx *Context, args map[string]any) (string, error)
```

LLM 调用工具时执行的函数签名。

| 参数 | 类型 | 说明 |
|------|------|------|
| `ctx` | `*Context` | 工具执行上下文 |
| `args` | `map[string]any` | LLM 传入的参数，键值对形式 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| 结果 | `string` | 工具执行的文本结果，返回给 LLM |
| 错误 | `error` | 执行失败时返回错误，LLM 据此感知并重试 |

---

### UpdateCallback — 被动同步回调

```go
type UpdateCallback func(ctx *Context, response map[string]any)
```

每次 LLM 响应后自动执行的同步回调，无论当前工具是否被 LLM 显式调用。

| 参数 | 类型 | 说明 |
|------|------|------|
| `ctx` | `*Context` | 工具执行上下文 |
| `response` | `map[string]any` | LLM 本轮完整响应数据 |

---

### Context — 工具执行上下文

```go
type Context struct {
    Ctx        context.Context // 可取消的请求上下文
    SessionID  string
    WorkDir    string
    ToolCallID string          // LLM tool_call id
    Permission PermissionLevel
    Extra      map[string]any  // 扩展数据

    GuardReviewer func(command string) (approved bool, reason string)
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Ctx` | `context.Context` | 可取消的请求上下文，用于在整个工具调用链路中传播取消信号 |
| `SessionID` | `string` | 会话唯一标识 |
| `WorkDir` | `string` | 当前工作目录绝对路径 |
| `ToolCallID` | `string` | LLM 分配的工具调用 ID |
| `Permission` | `PermissionLevel` | 当前操作权限级别 |
| `Extra` | `map[string]any` | 扩展数据，可在各回调间传递自定义上下文 |
| `GuardReviewer` | `func(command string) (bool, string)` | 可选的 LLM 级语义审查回调。在 bash 工具的 guardCheck 正则检查通过后、命令实际执行前调用。用于子智能体通过 guard 子智能体进行安全审查。guard 子智能体自身应设为 `nil` 以避免循环审查 |

---

### PermissionLevel — 权限级别

```go
type PermissionLevel int

const (
    Allow PermissionLevel = iota // 自动允许
    Ask                          // 需用户确认
    Deny                         // 自动拒绝
)
```

| 常量 | 值 | 说明 |
|------|-----|------|
| `Allow` | `0` | 自动允许，无需用户干预 |
| `Ask` | `1` | 需要用户显式确认 |
| `Deny` | `2` | 自动拒绝执行 |

---

### Schema — JSON Schema 对象

用于构建 OpenAI function calling 所需的 `parameters` 字段。

```go
type Schema struct {
    Type       string              `json:"type"`
    Properties map[string]Property `json:"properties,omitempty"`
    Required   []string            `json:"required,omitempty"`
    Enum       []string            `json:"enum,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Type` | `string` | JSON Schema 类型，通常为 `"object"` |
| `Properties` | `map[string]Property` | 参数属性定义 |
| `Required` | `[]string` | 必填参数名列表 |
| `Enum` | `[]string` | 枚举限制列表 |

---

### Property — Schema 属性定义

```go
type Property struct {
    Type        string   `json:"type"`
    Description string   `json:"description,omitempty"`
    Enum        []string `json:"enum,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Type` | `string` | 属性类型（`"string"`、`"number"`、`"boolean"` 等） |
| `Description` | `string` | 属性描述 |
| `Enum` | `[]string` | 枚举值限制 |

#### MustMarshalSchema — Schema 序列化

```go
func MustMarshalSchema(s Schema) json.RawMessage
```

将 Schema 对象序列化为 JSON，序列化失败时触发 panic。用于在包初始化阶段生成 `FuncDef.Parameters`。

---

## 工具注册表 (Registry)

### Registry — 工具注册中心

线程安全的工具注册表，管理所有已注册工具的增删查改。

```go
type Registry struct {
    mu    sync.RWMutex
    tools map[string]*Tool  // name → Tool
    order []string          // 保持注册顺序
}
```

#### NewRegistry — 创建注册表

```go
func NewRegistry() *Registry
```

返回一个新的空注册表。

---

### Registry 方法

#### Register

```go
func (r *Registry) Register(t *Tool) error
```

注册一个工具。自动调用 `t.Update()` 预编码 JSON。若工具名重复则返回错误。

| 参数 | 类型 | 说明 |
|------|------|------|
| `t` | `*Tool` | 要注册的工具，`Function.Name` 作为键 |

| 返回值 | 说明 |
|--------|------|
| `error` | 重复注册或预编码失败时返回错误 |

内部操作：
1. 检查 `t.Function.Name` 是否已存在
2. 设置 `t.Enable = true`
3. 调用 `t.Update()` 预编码 JSON 缓存
4. 存入 `tools` 并追加至 `order`

#### Unregister

```go
func (r *Registry) Unregister(name string)
```

注销指定名称的工具，线程安全。

#### Get

```go
func (r *Registry) Get(name string) (*Tool, bool)
```

按名称获取工具。第二个返回值表示是否存在。

#### All

```go
func (r *Registry) All() []*Tool
```

返回所有已注册工具的切片，按注册顺序排列（含已禁用的工具）。

#### AllEnabled

```go
func (r *Registry) AllEnabled() []*Tool
```

返回所有 `Enable == true` 的工具切片，按注册顺序排列。

#### Enable

```go
func (r *Registry) Enable(name string)
```

启用指定工具，若工具不存在则静默忽略。

#### Disable

```go
func (r *Registry) Disable(name string)
```

禁用指定工具，若工具不存在则静默忽略。

#### CallUpdateAll

```go
func (r *Registry) CallUpdateAll(ctx *Context, response map[string]any)
```

遍历所有已启用工具的 `CallUpdate` 回调并执行。借鉴 catai 设计：无论工具是否被 LLM 调用，都在响应后同步状态。

#### Count

```go
func (r *Registry) Count() int
```

返回已注册工具总数。

---

## 权限系统

### PermissionRule — 权限规则

```go
type PermissionRule struct {
    Tool  string          // 工具名，空字符串表示所有工具
    Path  string          // 文件路径 glob 模式
    Level PermissionLevel
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Tool` | `string` | 目标工具名。空字符串表示匹配所有工具 |
| `Path` | `string` | 文件路径 glob 模式，支持 `*`、`**`(递归)、`?`(单字符) |
| `Level` | `PermissionLevel` | 匹配时的权限级别 |

---

### PermissionChecker — 权限检查器

```go
type PermissionChecker struct {
    rules []PermissionRule
    mu    sync.RWMutex
}
```

#### NewPermissionChecker

```go
func NewPermissionChecker(rules []PermissionRule) *PermissionChecker
```

用给定的规则列表创建权限检查器。

#### Check

```go
func (pc *PermissionChecker) Check(toolName string, path string) PermissionLevel
```

按规则列表顺序匹配，返回首次命中规则的权限级别。若无匹配：

| 工具 | 默认级别 |
|------|---------|
| `"bash"` | `Ask`（需用户确认） |
| 其他 | `Allow`（自动允许） |

#### matchGlob（内部函数）

```go
func matchGlob(pattern, name string) bool
```

支持三种匹配策略：
1. 精确匹配：`pattern == "*"` 或 `pattern == name`
2. 标准 glob：使用 `filepath.Match`
3. 前缀匹配：pattern 以 `*` 结尾时进行前缀比较

---

### PermissionFromMap — 权限规则解析

```go
func PermissionFromMap(m map[string]any) []PermissionRule
```

从配置映射解析权限规则列表。支持两种格式：

**格式一：简单格式**
```go
"read": "allow"   → Tool="read", Path="*", Level=Allow
"bash": "deny"    → Tool="bash", Path="*", Level=Deny
```

**格式二：嵌套格式（按路径细化）**
```go
"bash": {
    "git *": "allow",
    "rm *":  "deny",
    "*":     "ask"
}
→ [bash, git *, Allow]
→ [bash, rm *, Deny]
→ [bash, *, Ask]
```

| 权限字符串 | 对应常量 |
|-----------|---------|
| `"allow"` | `Allow` |
| `"ask"` | `Ask` |
| `"deny"` | `Deny` |

---

## Question — 选项框工具类型

`question.go` 定义了 LLM 向用户发起交互式提问的数据结构。

### QuestionInfo — 单个问题

```go
type QuestionInfo struct {
    Question string           `json:"question"` // 完整问题
    Header   string           `json:"header"`   // 短标签
    Options  []QuestionOption `json:"options"`  // 选项列表
    Multiple bool             `json:"multiple"` // 是否多选
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Question` | `string` | 完整的问题文本 |
| `Header` | `string` | 问题简短标签/标题 |
| `Options` | `[]QuestionOption` | 用户可选的选项列表 |
| `Multiple` | `bool` | `true` 表示允许多选 |

---

### QuestionOption — 单个选项

```go
type QuestionOption struct {
    Label       string `json:"label"`       // 显示文本
    Description string `json:"description"` // 选项说明
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Label` | `string` | 选项显示标签 |
| `Description` | `string` | 选项的详细描述 |

---

### QuestionRequest — LLM 提问请求

```go
type QuestionRequest struct {
    Questions []QuestionInfo `json:"questions"` // 问题列表
}
```

LLM 调用 question 工具时传入的参数结构，包含一组待展示的问题。

---

### QuestionAnswer — 用户回答

```go
type QuestionAnswer struct {
    Answers [][]string `json:"answers"` // questions[i] → selected labels
}
```

用户回答后返回的结构。`Answers[i]` 对应 `Questions[i]` 中被选中的选项 Label 列表。

---

## 内置工具注册 (builtin/register.go)

### ToolFactory

```go
func ToolFactory(
    provider     llm.Provider,
    roleReg      role.RegistryInterface,
    bus          event.EventBus,
    wdb          storage.WorkspaceDB,
    memoryService storage.MemoryService,
) func(string) *tool.Tool
```

返回一个工厂函数，根据工具名称创建对应的 `*Tool` 实例。若名称不识别返回 `nil`。

| 参数 | 类型 | 说明 | 依赖工具 |
|------|------|------|---------|
| `provider` | `llm.Provider` | LLM 提供者 | `companion_talk` |
| `roleReg` | `role.RegistryInterface` | 角色注册表 | `companion_talk` |
| `bus` | `event.EventBus` | 事件总线 | `send_message`, `question`, `companion_talk` |
| `wdb` | `storage.WorkspaceDB` | 工作区数据库 | `db_*`, `go_run`, `schedule_*` |
| `memoryService` | `storage.MemoryService` | 记忆服务 | `memory_*` |

### 注册的工具名称清单

| 工具名 | 工厂函数 | 分类 |
|--------|---------|------|
| `read` | `ReadTool()` | 文件读取 |
| `write` | `WriteTool()` | 文件写入 |
| `edit` | `EditTool()` | 文件编辑 |
| `glob` | `GlobTool()` | 文件名搜索 |
| `grep` | `GrepTool()` | 内容搜索 |
| `bash` | `BashTool()` | 命令执行 |
| `diff` | `DiffTool()` | 差异对比 |
| `webfetch` | `WebFetchTool()` | 网页抓取 |
| `skill` | `SkillTool()` | 技能加载 |
| `apply_patch` | `ApplyPatchTool()` | 补丁应用 |
| `send_message` | `SendMessageTool(bus)` | 消息发送 |
| `log_issue` | `LogIssueTool()` | 问题记录 |
| `question` | `QuestionTool(bus)` | 交互提问 |
| `companion_talk` | `CompanionTalkTool(provider, roleReg, bus)` | 猫猫对话 |
| `db_query` | `DBQueryTool(wdb)` | 数据库查询 |
| `db_exec` | `DBExecTool(wdb)` | 数据库写操作 |
| `db_tables` | `DBTablesTool(wdb)` | 数据库表结构 |
| `go_run` | `GoRunTool(wdb)` | Go 脚本执行（yaegi） |
| `memory_set` | `MemorySetTool(memoryService)` | 记忆写入 |
| `memory_get` | `MemoryGetTool(memoryService)` | 记忆读取 |
| `memory_search` | `MemorySearchTool(memoryService)` | 记忆搜索 |
| `memory_list` | `MemoryListTool(memoryService)` | 记忆列表 |
| `memory_delete` | `MemoryDeleteTool(memoryService)` | 记忆删除 |
| `schedule_create` | `ScheduleCreateTool(wdb)` | 计划任务创建 |
| `schedule_list` | `ScheduleListTool(wdb)` | 计划任务列表 |
| `schedule_delete` | `ScheduleDeleteTool(wdb)` | 计划任务删除 |
| `schedule_toggle` | `ScheduleToggleTool(wdb)` | 计划任务启停 |
| `plan_enter` | `nil`（在 main.go 直接注册） | 计划模式进入 |
| `plan_exit` | `nil`（在 main.go 直接注册） | 计划模式退出 |

### 重点工具实现细节

#### `go_run` — runYaegiScript

`runYaegiScript` 不再使用 `fmt.Sprintf` 模板将用户脚本嵌入 Go 代码，改为分步 `i.Eval()` 执行：
1. **Step 1**: `i.Eval()` 声明 `package main` 和标准库导入
2. **Step 2**: `i.Eval()` 定义输出收集器 `_output` 和 `println`/`printf` 辅助函数
3. **Step 3**: `i.Eval(script)` 直接在解释器全局作用域执行用户脚本（不嵌入任何模板）
4. **Step 4**: `i.Eval("_output.String()")` 收集脚本输出

这种分步执行方式避免了字符串模板注入风险，确保用户脚本的语法正确性不受模板干扰。

#### `db_exec` — DBExecTool

`DBExecTool` 的 `Call` 函数在安全检查中直接使用规范化（`strings.ToUpper` 后 `TrimSpace`）的 SQL 进行判断：
- **禁止操作**: `DROP`、`ALTER`、**`TRUNCATE`** 均被拦截
- **DELETE 强制**: `DELETE` 语句必须包含 `WHERE` 条件，防止误删全表

#### `db_query` — DBQueryTool

`DBQueryTool` 的查询结果会自动过滤敏感信息：当查询 `settings` 表时，若 `key` 列的值以 `.api_key` 结尾或等于 `api_key`，对应的 `value` 列将被替换为 `[敏感信息已隐藏]`，防止 API Key 等敏感数据通过查询结果泄露给 LLM。

> **注意**：`db_*`、`go_run`、`schedule_*` 系列工具在 `wdb` 为 `nil` 时返回 `nil`，不会暴露给 LLM。`memory_*` 系列同理，依赖 `memoryService` 非空。`plan_enter`/`plan_exit` 仅主智能体使用，工厂返回 `nil`，由 `main.go` 直接创建 `PlanEnterTool`/`PlanExitTool` 实例注册。

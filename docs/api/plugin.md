# plugin — 插件系统

## 包概述

`plugin` 包实现基于 [yaegi](https://github.com/traefik/yaegi) 解释器的 Go 插件热加载系统。插件以 `.go` 源文件形式存放在 `.catcode/plugins/` 目录下，可在运行时动态加载，扩展工具（Tool）、角色（Role）和命令。

核心设计由三层组成：

- **接口层**：`Plugin`/`ToolPlugin`/`RolePlugin` 定义了插件与宿主之间的契约。
- **管理器层**：`Manager` 负责插件生命周期管理（发现、加载、查询、重载）。
- **加载器层**：`Loader` 负责通过 yaegi 解释器编译并执行插件源码，同时注入 catcode 内部包的符号表（134 个符号，9 个包），使得插件可以直接 `import` 主项目的内部包而不依赖 GOPATH 或源码树。

---

## 接口定义

### `Plugin`

所有插件必须实现的顶层接口。

```go
type Plugin interface {
    Name() string
    Version() string
}
```

| 方法 | 返回值 | 说明 |
|------|--------|------|
| `Name() string` | 插件名称 | 用作插件的唯一标识，在 Manager 内部以名称索引 |
| `Version() string` | 版本号 | 语义化版本字符串，如 `"1.0.0"` |

---

### `ToolPlugin`

扩展工具的插件接口，嵌入 `Plugin`。

```go
type ToolPlugin interface {
    Plugin
    Tools(bus event.EventBus) []*tool.Tool
}
```

| 方法 | 参数 | 返回值 | 说明 |
|------|------|--------|------|
| `Tools(bus)` | `bus event.EventBus` — 事件总线，工具可通过它发布/订阅事件 | `[]*tool.Tool` | 返回该插件提供的一组工具定义。Manager 通过 `GetToolInstances` 收集所有已启用 ToolPlugin 的工具 |

---

### `RolePlugin`

扩展角色的插件接口，嵌入 `Plugin`。

```go
type RolePlugin interface {
    Plugin
    RoleDef() role.RoleDef
}
```

| 方法 | 返回值 | 说明 |
|------|--------|------|
| `RoleDef() role.RoleDef` | `role.RoleDef` | 返回该插件定义的角色配置。Manager 通过 `GetRoleInstances` 收集所有已启用 RolePlugin 的角色定义 |

---

## 数据结构

### `PluginContext`

插件运行上下文，在创建 Loader/Manager 时注入。

```go
type PluginContext struct {
    WorkDir string        // 工作区目录
    Bus     event.EventBus // 事件总线
    UI      uiAPI.UIAPI   // TUI 插件接口
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `WorkDir` | `string` | 当前工作区根目录路径，通过 yaegi 注入为插件中的 `var PluginWorkDir` 全局变量 |
| `Bus` | `event.EventBus` | 事件总线实例，传递给 ToolPlugin 的 `Tools()` 方法，工具可借此与主系统通信 |
| `UI` | `uiAPI.UIAPI` | TUI 插件接口，插件可通过此字段注册侧边栏面板、更新内容 |

---

### `PluginInfo`

插件元信息，由 Manager 在加载后填充并对外暴露。

```go
type PluginInfo struct {
    Name    string
    Version string
    Type    string // "tool" / "role" / "unknown"
    Path    string // 源文件路径
    Enabled bool
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 插件名称，来自 `Plugin.Name()` |
| `Version` | `string` | 插件版本，来自 `Plugin.Version()` |
| `Type` | `string` | 插件类型：`"tool"`（实现 ToolPlugin）、`"role"`（实现 RolePlugin）、`"unknown"`（仅实现 Plugin 基础接口） |
| `Path` | `string` | 插件源文件在磁盘上的绝对路径 |
| `Enabled` | `bool` | 是否启用（加载成功后默认 `true`） |

---

## Manager — 插件管理器

`Manager` 是插件系统的核心入口，负责插件的全生命周期管理。

### 类型定义

```go
type Manager struct {
    // 内部字段，不对外暴露
}
```

内部通过 `sync.RWMutex` 保证并发安全，维护 `plugins`（名称→PluginInfo 的映射）和 `instances`（名称→Plugin 实例的映射）。

---

### `NewManager` — 创建管理器

```go
func NewManager(pluginsDir string, ctx *PluginContext) *Manager
```

| 参数 | 类型 | 说明 |
|------|------|------|
| `pluginsDir` | `string` | 插件源文件所在目录的路径（通常为 `.catcode/plugins/`） |
| `ctx` | `*PluginContext` | 插件运行上下文，包含工作目录和事件总线 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| — | `*Manager` | 初始化后的管理器实例，内部 Loader 已就绪 |

---

### `Manager.LoadAll` — 加载全部插件

```go
func (m *Manager) LoadAll() ([]PluginInfo, error)
```

扫描 `pluginsDir` 目录下所有 `.go` 文件，逐一通过 Loader 编译加载，跳过加载失败的文件。

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `[]PluginInfo` | 切片 | 成功加载的插件元信息列表 |
| `error` | — | 仅当目录扫描失败时返回错误；单个插件加载失败不阻止整体流程 |

---

### `Manager.List` — 列出已加载插件

```go
func (m *Manager) List() []PluginInfo
```

返回当前所有已加载并注册的插件元信息（无论类型和启用状态）。

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `[]PluginInfo` | 切片 | 所有已加载插件的元信息列表 |

---

### `Manager.GetTools` — 获取工具插件信息

```go
func (m *Manager) GetTools() []PluginInfo
```

返回所有类型为 `"tool"` 且已启用的插件元信息。

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `[]PluginInfo` | 切片 | 已启用的 ToolPlugin 元信息列表 |

---

### `Manager.GetToolInstances` — 获取工具实例

```go
func (m *Manager) GetToolInstances(bus event.EventBus) []*tool.Tool
```

遍历所有已启用的 `ToolPlugin`，调用各自的 `Tools(bus)` 方法，汇总返回所有工具定义。

| 参数 | 类型 | 说明 |
|------|------|------|
| `bus` | `event.EventBus` | 事件总线实例，传递给每个 ToolPlugin 的 `Tools()` 方法 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `[]*tool.Tool` | 切片 | 所有已启用 ToolPlugin 提供的工具定义集合 |

> **注意**：通过 yaegi 加载的插件，其 `Tools()` 返回值在反射层面无法直接断言为 `[]*tool.Tool`（解释器与宿主类型系统隔离）。目前仅支持通过 `ToolPlugin` 接口断言的方式获取工具，而 `pluginWrapper` 未实现 `ToolPlugin`，因此 **通过 yaegi 加载的 ToolPlugin 在此方法中会被跳过**。这是当前版本的已知限制。

---

### `Manager.GetRoles` — 获取角色插件信息

```go
func (m *Manager) GetRoles() []PluginInfo
```

返回所有类型为 `"role"` 且已启用的插件元信息。

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `[]PluginInfo` | 切片 | 已启用的 RolePlugin 元信息列表 |

---

### `Manager.GetRoleInstances` — 获取角色定义

```go
func (m *Manager) GetRoleInstances() []role.RoleDef
```

遍历所有已启用的 `RolePlugin`，调用各自的 `RoleDef()` 方法，汇总返回所有角色定义。

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `[]role.RoleDef` | 切片 | 所有已启用 RolePlugin 提供的角色定义集合 |

> **注意**：与 `GetToolInstances` 同理，通过 yaegi 加载的 RolePlugin 在此方法中会被跳过。

---

### `Manager.Reload` — 重载插件

```go
func (m *Manager) Reload(name string) error
```

根据插件名称重新加载已注册的插件（重新读取并编译源文件，更新实例）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `name` | `string` | 要重载的插件名称（即 `Plugin.Name()` 的返回值） |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `error` | — | 若名称不存在则返回错误；重载成功返回 `nil` |

---

## Loader — 插件加载器

`Loader` 封装 yaegi 解释器，负责将 `.go` 源文件编译为可运行的插件实例。

### 类型定义

```go
type Loader struct {
    // 内部字段，不对外暴露
}
```

---

### `NewLoader` — 创建加载器

```go
func NewLoader(pluginsDir string, ctx *PluginContext) *Loader
```

| 参数 | 类型 | 说明 |
|------|------|------|
| `pluginsDir` | `string` | 插件源文件目录路径 |
| `ctx` | `*PluginContext` | 插件运行上下文 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| — | `*Loader` | 初始化后的加载器实例 |

---

### `Loader.Discover` — 发现插件文件

```go
func (l *Loader) Discover() ([]string, error)
```

扫描 `pluginsDir` 目录，返回所有 `.go` 文件的绝对路径。忽略子目录和非 `.go` 文件。

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `[]string` | 切片 | `.go` 文件绝对路径列表；目录不存在时返回 `nil, nil` |
| `error` | — | 目录存在但扫描失败时返回错误 |

---

### `Loader.Load` — 加载单个插件

```go
func (l *Loader) Load(path string) (Plugin, error)
```

读取指定路径的 `.go` 源文件，通过 yaegi 解释器编译执行，返回 `Plugin` 接口实例。加载流程如下：

1. 读取源文件内容。
2. 创建 yaegi 解释器实例（`Unrestricted: false`，限制插件对危险包的访问，只能使用预注册的 `catcodeSymbols` 符号表）。
3. 注入 Go 标准库符号表（`stdlib.Symbols`）。
4. 注入 catcode 内部包符号表（`catcodeSymbols`，详见下文）。
5. 注入 `PluginWorkDir` 全局变量（值为 `ctx.WorkDir`）。
6. 编译执行插件源码。
7. 验证 `Plugin` 顶层变量存在。
8. 调用 `Plugin.Name()` 和 `Plugin.Version()` 获取元信息。
9. 通过方法检测判断插件类型（`hasMethod` 检测 `Tools` 和 `RoleDef`）。
10. 返回 `*pluginWrapper` 实例（内部包装类型，实现 `Plugin` 接口）。

| 参数 | 类型 | 说明 |
|------|------|------|
| `path` | `string` | 插件源文件的绝对路径 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `Plugin` | 接口 | 加载成功后的插件实例（实际类型为 `*pluginWrapper`） |
| `error` | — | 文件读取、编译、验证、方法调用任一环节失败均返回错误 |


> **安全说明**：解释器以 `Unrestricted: false` 模式创建，限制插件对危险包（如 `os`、`syscall` 等）的访问。这是为了防止恶意插件执行任意系统调用，实现远程代码执行（RCE）防护。插件只能使用通过 `catcodeSymbols` 预注册的 catcode 内部包符号，无法直接访问操作系统底层能力。
---

## catcodeSymbols — 插件符号导出机制

`catcodeSymbols` 是 `plugin` 包内部的 `interp.Exports` 类型变量，通过 `buildCatcodeSymbols()` 函数构建。它在 yaegi 解释器中注册 catcode 主项目的内部包类型、函数和常量，使插件可以直接使用 `import "catcode/tool"` 等语句，而无需搭建 GOPATH 或创建源码树符号链接。

### 符号表总览

共注册 **134 个符号**，覆盖 **9 个内部包**：

| # | 包路径（import path） | 符号数 | 涵盖内容 |
|---|----------------------|--------|----------|
| 1 | `catcode/tool` | 13 | Tool、FuncDef、Schema、Property、Context、PermissionLevel、PermissionRule 类型；MustMarshalSchema、NewPermissionChecker、PermissionFromMap 函数；Allow/Ask/Deny 权限常量 |
| 2 | `catcode/core/event` | 40 | EventBus、Event、Subscriber、Trigger 等类型及构造函数；全部 31 个事件常量（用户交互、角色生命周期、子智能体、规划引擎、工具调用、Session、陪伴角色、对话框） |
| 3 | `catcode/agent/role` | 15 | RoleDef、ModelConfig、ThinkingConfig、ModelLimit、TriggerDef、StateDef 等类型；RoleAgent/RoleCompanion 及 Mode 常量；BuildFullModelName、ParseYAML 函数 |
| 4 | `catcode/core/errors` | 19 | CatError、ErrorCollector、SelfCorrect 类型；New/Newf/Wrap/Wrapf 等构造函数；9 个 Category 常量 |
| 5 | `catcode/core/buffer` | 2 | Buffer 类型及 New 构造函数 |
| 6 | `catcode/ai/llm` | 26 | Provider、ProviderRegistry、ChatRequest、Message、ToolDef、ChatResponse、Choice、Usage、StreamEvent、OpenAIClient 等类型；构造函数及工具函数；5 个 StreamEventType 常量 |
| 7 | `catcode/ai/session` | 4 | Session、Message 类型；New、FromConversationRow 函数 |
| 8 | `catcode/ai/compact` | 5 | CompactDecision 类型；ShouldCompact、BuildCompactionPrompt 函数；AutoCompactBufferRatio、MinMessagesForCompact 常量 |
| 9 | `catcode/data/storage` | 10 | ConversationRow、MessageRow、MemoryEntry、MemoryService、MemoryScope、ErrorLogEntry 等类型；ScopeGlobal/ScopeWorkspace 常量 |

### 使用方式

插件源码中直接使用标准 Go import 语法即可引用这些包：

```go
package main

import (
    "catcode/tool"
    "catcode/core/event"
    "catcode/agent/role"
)

type MyPlugin struct{}

func (p *MyPlugin) Name() string    { return "my-plugin" }
func (p *MyPlugin) Version() string { return "1.0.0" }
func (p *MyPlugin) Tools(bus event.EventBus) []*tool.Tool {
    return []*tool.Tool{
        {
            Name: "hello",
            FuncDef: tool.FuncDef{...},
            Permission: tool.Allow,
        },
    }
}

var Plugin MyPlugin
```

### 注入时机

符号表在 `Loader.Load()` 中通过 `i.Use(catcodeSymbols)` 注入，紧接在标准库符号表之后、插件源码编译之前执行。

### 实现原理

`buildCatcodeSymbols()` 利用 Go 的 `reflect` 包获取 catcode 内部包的运行时类型信息和函数指针，构建 `interp.Exports` 映射。yaegi 解释器在遇到 `import "catcode/tool"` 时，直接从该映射中查找对应的包名（如 `"catcode/tool/tool"`）并解析符号。

- 类型注册：使用 `reflect.ValueOf((*包.类型)(nil))` 获取指针类型的反射值，yaegi 据此推导类型的结构和方法集。
- 函数注册：使用 `reflect.ValueOf(包.函数名)` 获取函数指针，插件中可直接调用。
- 常量注册：使用 `reflect.ValueOf(包.常量名)` 获取常量值，插件中可直接引用。

---

## 内部机制说明

### pluginWrapper

`pluginWrapper` 是 `Loader.Load()` 返回的实际类型（`*pluginWrapper`），它实现了 `Plugin` 接口。之所以不直接暴露为导出类型，是因为 yaegi 解释器中的类型系统与宿主 Go 运行时隔离，通过接口断言获取 `ToolPlugin`/`RolePlugin` 在当前版本中存在限制。

```go
// 内部类型，不导出
type pluginWrapper struct {
    name     string
    version  string
    infoType string               // "tool" / "role" / "unknown"
    interp   *interp.Interpreter  // yaegi 解释器实例
    hasTools bool
    hasRole  bool
    ctx      *PluginContext
}
```

方法：
- `Name() string` — 返回插件名称
- `Version() string` — 返回插件版本

### 插件类型判定

`Loader.Load()` 通过检测插件中是否定义了 `Tools` 和 `RoleDef` 方法来判断插件类型：

- 存在 `Plugin.Tools` 方法 → 类型为 `"tool"`
- 存在 `Plugin.RoleDef` 方法 → 类型为 `"role"`
- 两者都不存在 → 类型为 `"unknown"`

判定逻辑位于 `Loader.Load()` 第 100–112 行，使用内部函数 `hasMethod(i *interp.Interpreter, expr string) bool` 进行反射检测。

### 插件源文件约定

1. 文件必须位于 `pluginsDir` 目录下，以 `.go` 为扩展名。
2. 必须定义为 `package main`。
3. 必须定义顶层变量 `var Plugin <类型>`，该变量须实现 `Name()` 和 `Version()` 方法。
4. 若要作为工具插件，须实现 `Tools(bus interface{}) []*tool.Tool` 方法（注意 yaegi 中参数类型可能需使用 `interface{}`）。
5. 若要作为角色插件，须实现 `RoleDef() role.RoleDef` 方法。

---

## 并发安全

`Manager` 内部使用 `sync.RWMutex` 保护 `plugins` 和 `instances` 映射。所有公开的读方法（`List`、`GetTools`、`GetToolInstances`、`GetRoles`、`GetRoleInstances`、`Reload` 的前半部分）使用读锁；写方法（`loadOne`）使用写锁。`LoadAll` 调用 `loadOne` 时内部已加锁，因此整个 Manager 是并发安全的。

---

## 使用示例

```go
import (
    "catcode/core/event"
    "catcode/plugin"
)

func main() {
    ctx := &plugin.PluginContext{
        WorkDir: "/path/to/workspace",
        Bus:     event.NewBus(),
    }

    mgr := plugin.NewManager("/path/to/workspace/.catcode/plugins", ctx)

    // 加载所有插件
    plugins, err := mgr.LoadAll()
    if err != nil {
        panic(err)
    }
    fmt.Printf("已加载 %d 个插件\n", len(plugins))

    // 列出所有插件
    for _, p := range mgr.List() {
        fmt.Printf("  - %s (%s) v%s [%s]\n", p.Name, p.Type, p.Version, p.Path)
    }

    // 获取所有工具实例
    tools := mgr.GetToolInstances(ctx.Bus)
    fmt.Printf("共 %d 个工具\n", len(tools))

    // 获取所有角色定义
    roles := mgr.GetRoleInstances()
    fmt.Printf("共 %d 个角色\n", len(roles))

    // 重载指定插件
    if err := mgr.Reload("my-plugin"); err != nil {
        fmt.Printf("重载失败: %v\n", err)
    }
}
```

---

## 依赖关系

```
plugin
├── catcode/agent/role    (RoleDef 类型)
├── catcode/core/event    (EventBus 接口)
├── catcode/core/errors   (cerr 错误包装)
├── catcode/tool          (Tool 类型)
├── catcode/ai/llm        (LLM 类型，符号表)
├── catcode/ai/session    (Session 类型，符号表)
├── catcode/ai/compact    (上下文压缩，符号表)
├── catcode/core/buffer   (Buffer，符号表)
├── catcode/data/storage  (存储类型，符号表)
└── github.com/traefik/yaegi  (Go 解释器)
```

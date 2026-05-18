# core/config — 配置管理

## 包概述

`core/config` 包实现了 catcode 的完整配置管理系统，包含配置结构体定义、从工作区加载、环境变量覆盖、CLI 参数覆盖、校验、序列化持久化等功能。

同时，`core/config/loader` 子包提供了一个通用的、基于优先级的多源配置加载框架，支持热重载。

### 配置优先级系统

配置从低到高依次应用，高优先级覆盖低优先级：

| 层级 | 来源 | 说明 |
|------|------|------|
| 1 | `default_settings.json` | 嵌入的内置默认配置 |
| 2 | DB settings 表 | 工作区数据库中的持久化配置 |
| 3 | 环境变量 (`CATCODE_*`) | 系统环境变量覆盖 |
| 4 | CLI 参数 | 命令行参数覆盖 |

---

## config 包 — 配置核心

导入路径：`catcode/core/config`

---

### 类型定义

#### Config

全局配置结构体，包含所有 catcode 的配置项。

```go
type Config struct {
    Model       string                      `json:"model"`
    SmallModel  string                      `json:"small_model"`
    Providers   map[string]ProviderConfig   `json:"providers"`
    DefaultAgent string                     `json:"default_agent"`
    Agents      map[string]AgentConfig      `json:"agents"`
    RolePaths   []string                    `json:"role_paths"`
    Permissions map[string]any              `json:"permissions"`
    TUI         TUIConfig                   `json:"tui"`
    MCPServers  []MCPServerConfig           `json:"mcp_servers"`
    LSP         map[string]LSPConfig        `json:"lsp"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Model` | `string` | 默认模型标识符，格式为 `"provider:modelname"`（两侧均不能为空） |
| `SmallModel` | `string` | 轻量模型标识符，用于探索、摘要等低开销任务。格式同上，可为空表示回退到 `Model` |
| `Providers` | `map[string]ProviderConfig` | 模型提供商配置，键为提供商标识符（如 `"deepseek"`） |
| `DefaultAgent` | `string` | 默认主智能体名称，不能为空 |
| `Agents` | `map[string]AgentConfig` | 智能体配置集合，键为智能体名称 |
| `RolePaths` | `[]string` | 角色定义文件的搜索路径列表 |
| `Permissions` | `map[string]any` | 权限配置，自由格式的键值对 |
| `TUI` | `TUIConfig` | 终端用户界面配置 |
| `MCPServers` | `[]MCPServerConfig` | MCP 服务器配置列表 |
| `LSP` | `map[string]LSPConfig` | LSP（语言服务器）配置，键为语言标识符 |

---

#### ProviderConfig

模型提供商配置。

```go
type ProviderConfig struct {
    Name    string                 `json:"name"`
    BaseURL string                 `json:"base_url"`
    APIKey  string                 `json:"api_key"`
    Models  map[string]ModelConfig `json:"models"`
    Options map[string]any         `json:"options"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 提供商显示名称（如 `"DeepSeek"`） |
| `BaseURL` | `string` | API 基础 URL（如 `https://api.deepseek.com/v1`） |
| `APIKey` | `string` | API 密钥，可通过环境变量自动注入 |
| `Models` | `map[string]ModelConfig` | 该提供商下的模型列表，键为模型标识符 |
| `Options` | `map[string]any` | 额外选项，自由格式 |

---

#### ModelConfig

单个模型配置。

```go
type ModelConfig struct {
    Name    string         `json:"name"`
    Options map[string]any `json:"options,omitempty"`
    Limit   *ModelLimit    `json:"limit,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 模型名称 |
| `Options` | `map[string]any` | 模型级额外选项（可选） |
| `Limit` | `*ModelLimit` | 模型使用限制（可选） |

---

#### ModelLimit

模型使用限制参数。

```go
type ModelLimit struct {
    Context int `json:"context"`
    Output  int `json:"output"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Context` | `int` | 上下文窗口大小（token 数） |
| `Output` | `int` | 最大输出 token 数 |

---

#### AgentConfig

智能体配置。

```go
type AgentConfig struct {
    Description string         `json:"description"`
    Mode        string         `json:"mode"`
    Model       string         `json:"model"`
    Temperature float64        `json:"temperature"`
    Permission  map[string]any `json:"permission"`
    Color       string         `json:"color"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Description` | `string` | 智能体的描述文本 |
| `Mode` | `string` | 智能体模式：`"primary"`（主智能体）、`"subagent"`（子智能体）、`"background"`（后台智能体） |
| `Model` | `string` | 智能体使用的模型 |
| `Temperature` | `float64` | 模型温度参数（0-1 之间，控制输出随机性） |
| `Permission` | `map[string]any` | 智能体的权限配置 |
| `Color` | `string` | 智能体在 TUI 中的显示颜色 |

---

#### TUIConfig

终端用户界面（TUI）配置。

```go
type TUIConfig struct {
    Theme       string  `json:"theme"`
    FontSize    int     `json:"font_size"`
    ChatRatio   float64 `json:"chat_ratio"`
    EnableMouse bool    `json:"enable_mouse"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Theme` | `string` | 主题：`"dark"`（暗色）或 `"light"`（亮色） |
| `FontSize` | `int` | 字体大小 |
| `ChatRatio` | `float64` | 聊天面板在界面中的比例（0-1） |
| `EnableMouse` | `bool` | 是否启用鼠标交互 |

---

#### MCPServerConfig

MCP（Model Context Protocol）服务器配置。

```go
type MCPServerConfig struct {
    Name    string            `json:"name"`
    Command string            `json:"command"`
    Args    []string          `json:"args"`
    Env     map[string]string `json:"env,omitempty"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | MCP 服务器名称 |
| `Command` | `string` | 启动命令 |
| `Args` | `[]string` | 命令参数列表 |
| `Env` | `map[string]string` | 环境变量键值对（可选） |

---

#### LSPConfig

语言服务器协议（LSP）配置。

```go
type LSPConfig struct {
    Command    []string `json:"command"`
    Extensions []string `json:"extensions"`
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Command` | `[]string` | LSP 启动命令及其参数 |
| `Extensions` | `[]string` | 关联的文件扩展名列表 |

---

### 函数

#### LoadFromWorkspace

从工作区数据库加载配置，依次应用环境变量和 CLI 参数覆盖。

```go
func LoadFromWorkspace(wdb storage.WorkspaceDB, cliModel string, cliTemp float64) (*Config, error)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `wdb` | `storage.WorkspaceDB` | 工作区数据库接口，用于读取 `settings` 表中的配置 |
| `cliModel` | `string` | CLI 指定的模型覆盖值，空字符串表示不覆盖 |
| `cliTemp` | `float64` | CLI 指定的温度覆盖值，`0` 表示不覆盖（仅当 `> 0` 时生效） |

**返回值：**

| 返回值 | 说明 |
|--------|------|
| `*Config` | 加载并合并后的完整配置 |
| `error` | 加载或校验失败时返回错误 |

**加载流程：**

1. 从 `wdb.GetAllSettingsFlattened()` 读取 DB 中的扁平化配置
2. 将扁平化键值对重组为嵌套结构（`storage.UnflattenSettings`）
3. 序列化为 JSON 后反序列化到 `Config` 结构体
4. 调用 `applyEnvOverrides()` 应用环境变量覆盖
5. 使用 CLI 参数覆盖 `Model` 和所有 Agent 的 `Temperature`
6. 调用 `ensureAPIKeys()` 从环境变量补充 API 密钥
7. 调用 `Validate()` 进行最终校验

---

#### Validate

校验配置的合法性。

```go
func (c *Config) Validate() error
```

**校验规则：**

- `Model` 不能为空
- `Model` 必须为 `"provider:modelname"` 格式（两侧均不能为空）
- 如果设置了 `SmallModel`，同样必须为 `"provider:modelname"` 格式
- `DefaultAgent` 不能为空

**返回值：** 校验通过返回 `nil`，否则返回描述具体问题的错误。

---

#### ToJSON

将配置序列化为格式化的 JSON 字符串。

```go
func (c *Config) ToJSON() (string, error)
```

**返回值：**

| 返回值 | 说明 |
|--------|------|
| `string` | 缩进格式的 JSON 字符串 |
| `error` | 序列化失败时返回错误 |

---

#### SaveTo

将配置保存到指定文件路径。如果父目录不存在，会自动创建。

```go
func (c *Config) SaveTo(path string) error
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `path` | `string` | 目标文件路径 |

**行为：**
- 自动创建父目录（权限 `0755`）
- 以缩进 JSON 格式写入文件（权限 `0644`）

**返回值：** 目录创建失败、序列化失败或写入失败时返回错误。

---

#### GetProvider

根据提供商名称获取对应的配置。

```go
func (c *Config) GetProvider(name string) (ProviderConfig, bool)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `name` | `string` | 提供商标识符（如 `"deepseek"`） |

**返回值：**

| 返回值 | 说明 |
|--------|------|
| `ProviderConfig` | 提供商配置；未找到时返回零值 |
| `bool` | 是否找到该提供商 |

---

#### GetAgent

根据智能体名称获取对应的配置。

```go
func (c *Config) GetAgent(name string) (AgentConfig, bool)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `name` | `string` | 智能体名称 |

**返回值：**

| 返回值 | 说明 |
|--------|------|
| `AgentConfig` | 智能体配置；未找到时返回零值 |
| `bool` | 是否找到该智能体 |

---

#### DefaultProvider

从 `Config.Model` 解析出默认提供商并返回其配置。如果解析的提供商不存在，则回退到返回第一个可用的提供商。

```go
func (c *Config) DefaultProvider() ProviderConfig
```

**解析逻辑：**
1. 使用 `llm.ParseModelName()` 从 `Model` 字段中解析提供商名称
2. 在 `Providers` 映射中查找该提供商
3. 如果未找到，返回 `Providers` 映射中的第一个提供商
4. 如果没有任何提供商，返回零值 `ProviderConfig{}`

**返回值：** 解析出的 `ProviderConfig`。

---

#### ResolveModel

从 `Config.Model` 解析提供商配置和纯模型名。

```go
func (c *Config) ResolveModel() (ProviderConfig, string)
```

**实现：** 内部调用 `resolveModelName(c.Model)`。

**返回值：**

| 返回值 | 说明 |
|--------|------|
| `ProviderConfig` | 解析到的提供商配置 |
| `string` | 纯模型名称（不含提供商前缀） |

---

#### ResolveSmallModel

从 `Config.SmallModel` 解析提供商配置和纯模型名。如果 `SmallModel` 为空，则回退到 `ResolveModel()`。

```go
func (c *Config) ResolveSmallModel() (ProviderConfig, string)
```

**返回值：**

| 返回值 | 说明 |
|--------|------|
| `ProviderConfig` | 解析到的提供商配置 |
| `string` | 纯模型名称（不含提供商前缀） |

---

### 内部函数

以下函数未导出，仅在包内使用，此处列出以便理解包的工作流程。

#### applyEnvOverrides

```go
func applyEnvOverrides(cfg *Config)
```

应用环境变量覆盖。支持的变量：

| 环境变量 | 覆盖字段 | 说明 |
|----------|----------|------|
| `CATCODE_MODEL` | `Config.Model` | 覆盖默认模型 |
| `CATCODE_THEME` | `Config.TUI.Theme` | 覆盖 TUI 主题 |
| `CATCODE_BASE_URL` | 所有 `ProviderConfig.BaseURL` | 覆盖所有提供商的 API 基础 URL |

#### ensureAPIKeys

```go
func ensureAPIKeys(cfg *Config)
```

确保每个提供商都已设置 API 密钥。按以下优先级查找：

1. 检查提供商配置中 `APIKey` 是否已设置
2. 如果为空，读取 `{PROVIDER_NAME}_API_KEY` 环境变量（如 `DEEPSEEK_API_KEY`）
3. 如果仍为空，回退到 `OPENAI_API_KEY` 环境变量作为通用回退

---

## loader 包 — 配置加载框架

导入路径：`catcode/core/config/loader`

`loader` 包提供了一个通用的、基于优先级的多源配置加载框架。设计理念：
- **Source（源）**：配置的来源，如文件、环境变量、远程等，每个源具有独立的优先级和加载函数。
- **Loader（加载器）**：管理多个 Source，按优先级加载并深度合并。
- **Watcher（监视器）**：支持配置变更的监视和热重载。

优先级（从低到高）：内置默认值 → 全局配置 → 项目配置 → 本地配置 → 环境变量 → CLI 参数

---

### 类型定义

#### Source

代表一个配置源，封装了名称、优先级和加载逻辑。

```go
type Source struct {
    Name     string
    Priority int
    Load     func() (map[string]any, error)
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Name` | `string` | 配置源名称，用于调试和错误报告 |
| `Priority` | `int` | 优先级，数字越大优先级越高。同优先级按添加顺序保留 |
| `Load` | `func() (map[string]any, error)` | 加载函数，返回扁平化或嵌套的键值对映射 |

---

#### ChangeEvent

配置变更事件，当监视器检测到配置变化时产生。

```go
type ChangeEvent struct {
    Source string
    Keys   []string
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Source` | `string` | 变更来源标识符（如 `"dir:/path/to/config"`） |
| `Keys` | `[]string` | 发生变更的键路径列表 |

---

#### ChangeHandler

配置变更回调函数类型。

```go
type ChangeHandler func(event ChangeEvent)
```

---

#### Loader

统一配置加载器，管理多个 Source 和 Watcher。

```go
type Loader struct {
    // 未导出字段：
    // mu       sync.RWMutex
    // sources  []Source
    // watchers []Watcher
    // handlers []ChangeHandler
    // cache    map[string]any
}
```

Loader 是线程安全的，所有公开方法均内部加锁。

**使用流程：**

1. 调用 `New()` 创建 Loader
2. 通过 `AddSource()` 添加配置源
3. 可选：通过 `AddWatcher()` 添加监视器、通过 `OnChange()` 注册回调
4. 调用 `Load()` 或 `LoadInto()` 加载配置
5. 可选：调用 `StartWatching()` 启动热重载

---

#### Watcher

配置监视器接口，实现此接口可扩展监视能力。

```go
type Watcher interface {
    Start(onChange func(ChangeEvent)) error
    Stop() error
}
```

| 方法 | 说明 |
|------|------|
| `Start(onChange func(ChangeEvent)) error` | 开始监视配置变更，检测到变更时回调 `onChange` |
| `Stop() error` | 停止监视 |

---

#### DirWatcher

目录监视器，通过轮询方式监视目录下文件的修改和删除。

```go
type DirWatcher struct {
    // 未导出字段：
    // dir      string
    // pattern  string
    // interval time.Duration
    // stopCh   chan struct{}
    // modTimes map[string]time.Time
}
```

`DirWatcher` 实现了 `Watcher` 接口。

---

### 函数

#### New

创建新的配置加载器实例。

```go
func New() *Loader
```

**返回值：** 初始化完成的 `*Loader`，内部切片和映射为空。

---

#### AddSource

向加载器添加一个配置源。添加后内部按优先级排序，同优先级保持添加顺序。

```go
func (l *Loader) AddSource(src Source)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `src` | `Source` | 要添加的配置源 |

**注意：** 添加新源后不会自动重新加载，需要显式调用 `Load()` 或 `LoadInto()`。

---

#### AddWatcher

向加载器添加一个配置监视器。

```go
func (l *Loader) AddWatcher(w Watcher)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `w` | `Watcher` | 实现了 `Watcher` 接口的监视器实例 |

---

#### OnChange

注册配置变更回调函数。当任意监视器检测到配置变更时触发所有已注册的回调。

```go
func (l *Loader) OnChange(handler ChangeHandler)
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `handler` | `ChangeHandler` | 变更回调函数，接收 `ChangeEvent` 参数 |

---

#### Load

按优先级顺序加载所有配置源并深度合并。

```go
func (l *Loader) Load() (map[string]any, error)
```

**行为：**
- 按优先级从低到高依次加载各 Source
- 使用 `deepMerge` 深度合并：map 类型递归合并，其他类型直接覆盖
- 合并结果同时存入内部缓存

**返回值：**

| 返回值 | 说明 |
|--------|------|
| `map[string]any` | 合并后的键值对映射 |
| `error` | 任意 Source 加载失败时返回错误 |

---

#### LoadInto

加载配置并填充到目标结构体中。使用 JSON 作为中间序列化格式，天然支持嵌套结构体和类型转换。

```go
func (l *Loader) LoadInto(target any) error
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `target` | `any` | 目标结构体指针，配置将填充到此对象 |

**实现：** 内部调用 `Load()` → 序列化为 JSON → 反序列化到 `target`。

**返回值：** 加载、序列化或反序列化失败时返回错误。

---

#### GetCache

获取缓存的合并配置。返回的是深拷贝，修改不影响内部缓存。

```go
func (l *Loader) GetCache() map[string]any
```

**返回值：** 上次 `Load()` 结果的深拷贝；若从未调用过 `Load()`，返回空映射。

---

#### StartWatching

启动所有已添加的监视器，开始监听配置变更。

```go
func (l *Loader) StartWatching() error
```

**返回值：** 任意监视器启动失败时返回错误。

---

#### StopWatching

停止所有已添加的监视器。

```go
func (l *Loader) StopWatching() error
```

**返回值：** 任意监视器停止失败时返回错误。

---

#### NewDirWatcher

创建目录监视器实例。通过定期扫描目录下匹配模式的文件来检测变更。

```go
func NewDirWatcher(dir, pattern string, interval time.Duration) *DirWatcher
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `dir` | `string` | 要监视的目录路径 |
| `pattern` | `string` | 文件名匹配模式（支持 `filepath.Match` 通配符，如 `"*.json"`），空字符串表示匹配所有文件 |
| `interval` | `time.Duration` | 轮询间隔 |

**返回值：** 初始化完成的 `*DirWatcher`。

---

#### (*DirWatcher) Start

开始监视目录变更。启动一个后台 goroutine 按指定间隔扫描目录。首次调用时会扫描目录以建立文件的修改时间基线。

```go
func (dw *DirWatcher) Start(onChange func(ChangeEvent)) error
```

**参数：**

| 参数 | 类型 | 说明 |
|------|------|------|
| `onChange` | `func(ChangeEvent)` | 检测到文件新增、修改或删除时的回调函数 |

**返回值：** 始终返回 `nil`。

**检测逻辑：**
- 文件修改：当前修改时间晚于记录的修改时间
- 文件新增：当前文件不在记录的修改时间映射中
- 文件删除：之前记录的文件在当前扫描中不存在

每次检测到变更都会生成一个 `ChangeEvent`，其 `Source` 为 `"dir:{目录路径}"`，`Keys` 包含变更的文件路径。

---

#### (*DirWatcher) Stop

停止目录监视。关闭内部的停止通道，后台 goroutine 将在下一次 tick 时退出。

```go
func (dw *DirWatcher) Stop() error
```

**返回值：** 始终返回 `nil`。

---

### 内部函数

#### deepMerge

深度合并两个映射。对于嵌套 map 类型递归合并（保留 dst 中未覆盖的键），对于非 map 类型直接覆盖。

```go
func deepMerge(dst, src map[string]any)
```

#### copyMap

深拷贝一个映射（包括嵌套映射）。

```go
func copyMap(src map[string]any) map[string]any
```

---

## 使用示例

### 基本配置加载

```go
import (
    "catcode/core/config"
    "catcode/data/storage"
)

// 从工作区加载配置
wdb, _ := storage.OpenWorkspaceDB("/path/to/workspace")
cfg, err := config.LoadFromWorkspace(wdb, "", 0)
if err != nil {
    // 处理错误
}

// 使用配置
provider, modelName := cfg.ResolveModel()
agent, found := cfg.GetAgent(cfg.DefaultAgent)
```

### 使用 Loader 框架

```go
import (
    "catcode/core/config"
    "catcode/core/config/loader"
)

l := loader.New()

// 添加内置默认源
l.AddSource(loader.Source{
    Name:     "defaults",
    Priority: 0,
    Load: func() (map[string]any, error) {
        return map[string]any{"model": "deepseek:deepseek-chat"}, nil
    },
})

// 添加文件源
l.AddSource(loader.Source{
    Name:     "project-config",
    Priority: 10,
    Load: func() (map[string]any, error) {
        data, err := os.ReadFile(".catcode/config.json")
        if err != nil {
            return nil, err
        }
        var m map[string]any
        json.Unmarshal(data, &m)
        return m, nil
    },
})

// 加载到 Config 结构体
var cfg config.Config
if err := l.LoadInto(&cfg); err != nil {
    log.Fatal(err)
}
```

### 配置热重载

```go
l := loader.New()
// ... 添加 Source

// 添加目录监视器
dw := loader.NewDirWatcher(".catcode", "*.json", 5*time.Second)
l.AddWatcher(dw)

// 注册变更回调
l.OnChange(func(event loader.ChangeEvent) {
    log.Printf("配置变更: source=%s keys=%v", event.Source, event.Keys)
    // 重新加载配置
    l.Load()
})

l.StartWatching()
defer l.StopWatching()
```

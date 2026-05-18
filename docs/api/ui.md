# ui/tui — 终端用户界面

## 包概述

`ui/tui` 是 catcode 的终端用户界面（TUI）包，基于 [Bubble Tea](https://github.com/charmbracelet/bubbletea) 框架构建。该包提供了完整的聊天式 AI 交互终端界面，包含以下核心模块：

| 模块 | 说明 |
|------|------|
| **TUI Model**（`tui.go`） | Bubble Tea 主模型，定义 `Model` 结构体、`Init` 方法、生命周期管理 |
| **Update**（`tui_update.go`） | `Model.Update()` 实现，所有消息路由与键盘事件处理（593 行） |
| **View**（`tui_view.go`） | `Model.View()` 实现，界面布局与渲染逻辑（198 行） |
| **渲染系统**（`renderer.go`, `styles.go`） | Markdown 渲染、代码语法高亮、lipgloss 样式定义 |
| **消息系统**（`messages.go`） | 各类 Bubble Tea Msg 类型，用于组件间通信 |
| **组件系统**（`component/`） | 组件接口定义和标准实现（状态栏、选项框等） |
| **事件适配器**（`adapters.go`） | 将 Model 内部方法适配为组件接口 |
| **管理器**（`manager/`） | UI 布局计算和鼠标事件路由 |
| **插件 API**（`plugin/`） | 对外暴露的插件界面接口 |
| **侧边栏**（`sidebar_render.go`） | 6 个侧边栏面板（规划/日志/智能体/猫猫/任务/会话） |
| **@ 命令**（`at_parser.go`） | `@agent` 命令解析器 |
| **@mention**（`mention.go`） | 子智能体自动补全 |
| **选项框**（`question.go`） | 选项对话框交互模式 |

架构核心：`Model` 实现 `tea.Model` 接口，通过 `Init`/`Update`/`View` 三方法驱动整个 TUI。文件已按 Bubble Tea 生命周期拆分为 `tui.go`（Init + 结构体定义）、`tui_update.go`（Update 消息路由）、`tui_view.go`（View 渲染布局），共三个文件。所有外部交互（流式内容、状态更新、侧边栏数据等）均通过 Bubble Tea Message 传递到 `Update` 方法中统一处理。

---

## 一、TUI Model 与生命周期（tui.go）

### `Model` 结构体

TUI 的主模型，持有全部界面状态。实现 `tea.Model` 接口。

**字段概览**：

| 字段 | 类型 | 说明 |
|------|------|------|
| `width`, `height` | `int` | 终端窗口尺寸 |
| `ready` | `bool` | 是否已完成首次初始化 |
| `viewport` | `viewport.Model` | 聊天区 viewport |
| `messages` | `[]*ChatMessage` | 聊天消息列表 |
| `streamStatus` | `string` | 当前流式状态：`""`(空闲), `"思考中"`, `"执行工具: xxx"`, `"等待子智能体: xxx"` |
| `streamBuf` | `strings.Builder` | 流式内容缓冲区 |
| `streamTokens` | `int` | 已接收的 token 数 |
| `pendingInput` | `string` | 流式期间缓存的用户输入 |
| `streamActive` | `bool` | 是否正在流式输出（防重入） |
| `thinkingBuf` | `strings.Builder` | 流式思考内容缓冲区 |
| `thinkingActive` | `bool` | 是否在接收思考内容 |
| `showThinking` | `bool` | 全局开关：是否显示思考过程 |
| `status` | `component.StatusDisplay` | 状态栏接口 |
| `chat` | `component.ChatDisplay` | 聊天区接口 |
| `side` | `component.SidebarDisplay` | 侧边栏接口 |
| `input` | `component.InputDisplay` | 输入区接口 |
| `questionMode` | `bool` | 是否处于选项框模式 |
| `mentionState` | `*MentionState` | @mention 自动补全状态 |
| `agentList` | `[]AgentInfo` | 智能体描述列表 |
| `scheduledTasks` | `[]ScheduledTaskInfo` | 周期任务列表 |
| `workspacePath` | `string` | 工作区路径 |
| `sidebarTab` | `SidebarTab` | 当前侧边栏面板 |
| `sidebarWidth` | `int` | 侧边栏宽度 |
| `companionMood` | `string` | 猫猫心情 |
| `companionIntimacy` | `int` | 猫猫亲密度 |
| `isDark` | `bool` | 主题：`true` 暗色，`false` 亮色 |
| `submit` | `func(string)` | 用户提交消息的回调 |
| `onCancel` | `func()` | 取消回调 |
| `mdRenderer` | `*MarkdownRenderer` | Markdown 渲染器缓存 |
| `statusBar` | `*component.StatusBarComponent` | 状态栏组件 |

---

### `func New(modelName string, toolCount, roleCount int, sidebarWidth int, onSubmit func(string)) *Model`

创建并初始化 TUI Model。

| 参数 | 类型 | 说明 |
|------|------|------|
| `modelName` | `string` | 当前使用的模型名称 |
| `toolCount` | `int` | 可用工具数量 |
| `roleCount` | `int` | 可用角色数量 |
| `sidebarWidth` | `int` | 初始侧边栏宽度（像素） |
| `onSubmit` | `func(string)` | 用户提交消息时调用的回调函数 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `m` | `*Model` | 已初始化的 TUI Model |

**功能**：初始化 textarea、viewport、侧边栏 viewport、Markdown 渲染器、状态栏组件、四个组件适配器（status/chat/side/input）；设置默认样式和颜色；发送欢迎消息。

---

### `func (m *Model) Init() tea.Cmd`

Bubble Tea 生命周期：初始化命令。

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `cmd` | `tea.Cmd` | 组合命令：启动 textarea 光标闪烁 + 侧边栏定时刷新 |

**功能**：返回 `tea.Batch(textarea.Blink, tickSidebar())`——启动光标闪烁和 2 秒间隔的侧边栏定时刷新。

---

### `func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)`

Bubble Tea 生命周期：消息处理。所有用户交互和外部事件在此方法中统一路由。

| 参数 | 类型 | 说明 |
|------|------|------|
| `msg` | `tea.Msg` | Bubble Tea 消息 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `model` | `tea.Model` | 更新后的模型 |
| `cmd` | `tea.Cmd` | 后续命令 |

**处理的消息类型**：

| 消息 | 处理逻辑 |
|------|---------|
| `tea.WindowSizeMsg` | 更新窗口尺寸，重新计算布局，重建渲染器 |
| `tea.KeyMsg` | 键盘事件路由（详见下表） |
| `StreamMsg` | 流式文本输出：思考标记(`🧠>`)、工具标记(`⚙️`)、子智能体标记(`🤖`)、错误标记(`❌⚠️🛡️`)、自我纠正(`🔄`)、完成标记(`✅`)、继续思考(`💭`) |
| `StreamDoneMsg` | 流式结束：清除状态，发送缓存的 pendingInput |
| `ToolCallMsg` | 工具调用消息 |
| `AddMessageMsg` | 添加指定类型的聊天消息 |
| `StatusMsg` | 更新状态栏信息（模型名/工具数/消息数） |
| `UpdateTodosMsg` | 更新规划面板 Todo 列表 |
| `UpdateLogMsg` | 追加日志条目 |
| `UpdateAgentsMsg` | 更新智能体列表 |
| `UpdateCompanionMsg` | 更新猫猫状态 |
| `QuestionRequestMsg` | 进入选项框模式 |
| `UpdateTasksMsg` | 更新周期任务列表 |
| `SessionInfoMsg` | 更新会话信息（工作区路径/插件数/MCP 服务器数） |
| `UpdateSessionsMsg` | 更新历史会话列表 |
| `TickMsg` | 定时器：刷新智能体 spinner 动画，执行 onTick 回调 |

**键盘事件路由优先级**（由高到低）：
1. 子会话视图按键（esc/ctrl+c 退出，f1-f6 切换面板，tab 循环，滚动键）
2. @mention 菜单键盘
3. 选项框模式键盘
4. 搜索模式键盘
5. Agents Tab 选中导航（↑/↓/enter）
6. 全局快捷键：`ctrl+c`（取消/退出）、`ctrl+s`（发送）、`ctrl+f`（搜索）、`ctrl+left/right`（侧边栏宽度）、`alt+t`（切换思考）、f1-f6（面板切换）、tab（循环面板）、`ctrl+_`（帮助）

---

### `func (m *Model) View() string`

Bubble Tea 生命周期：渲染界面。

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `view` | `string` | 完整的终端界面字符串 |

**布局结构**（从上到下）：
```
┌─ status bar ─────────────────────┐
│  chat area  │ separator │ sidebar│
│             │           │        │
└──────────────────────────────────┘
┌─ input area ─────────────────────┐
└─ help bar ───────────────────────┘

特殊模式：
- 子会话视图：聊天区显示 subSessionVP 内容，帮助栏显示子会话快捷键
- 选项框模式：输入区替换为选项框界面，压缩 viewport 高度
- @mention 模式：在输入区下方叠加自动补全菜单
- 搜索模式：在侧边栏和帮助栏之间显示搜索栏
```

---

### `func (m *Model) StatusDisplay() component.StatusDisplay`

返回状态栏组件的接口适配器。

### `func (m *Model) ChatDisplay() component.ChatDisplay`

返回聊天区组件的接口适配器。

### `func (m *Model) SidebarDisplay() component.SidebarDisplay`

返回侧边栏组件的接口适配器。

### `func (m *Model) InputDisplay() component.InputDisplay`

返回输入区组件的接口适配器。

---

## 二、消息类型（messages.go）

所有自定义的 Bubble Tea `Msg` 类型，通过 `tea.Program.Send()` 或 `tea.Cmd` 发送到 `Update` 方法。

### `type StreamMsg string`

流式输出消息。每条消息包含一段流式文本，可能带有特殊前缀标记：

| 前缀 | 含义 | 处理 |
|------|------|------|
| `\n🧠>` | 思考过程开始 | 累积到 `thinkingBuf`，状态设为 `"深度思考中..."` |
| `\n🧠✓` | 思考过程结束 | 设置 `thinkingActive = false` |
| `\n⚙️xxx` | 工具执行 | 状态改为 `"执行工具: xxx"` |
| `\n💭` | 继续思考 | 重置缓冲，创建新 AI 消息占位 |
| `\n🤖 xxx` | 子智能体启动 | 状态改为 `"等待子智能体: xxx"` |
| `\n✅` | 工具/子智能体完成 | 状态恢复 `"思考中"` |
| `\n🔄` | 自我纠正 | 状态设为 `"自我纠正中"` |
| `\n❌/⚠️/🛡️` | 错误/警告 | 记录日志 `"warn"`，不显示在对话框 |
| 普通文本 | 正常流式输出 | 累加到 `streamBuf`，刷新聊天区 |

---

### `type StreamDoneMsg struct{}`

流式传输完成消息。处理逻辑：
1. 将未处理的思考内容附加到最后一条 AI 消息
2. 重置 `streamStatus`, `streamActive`, `thinkingBuf`, `thinkingActive`
3. 若有缓存的 `pendingInput`，自动发送
4. 记录完成日志

---

### `type ToolCallMsg string`

工具调用消息，在聊天区显示工具调用信息。

---

### `type AddMessageMsg struct`

```
type AddMessageMsg struct {
    Type    MessageType  // 消息类型
    Content string       // 消息内容
    Sender  string       // 发送者标识
}
```

直接向聊天区添加一条消息，无需经过流式流程。

---

### `type StatusMsg struct`

```
type StatusMsg struct {
    ModelName string  // 模型名称（非空时更新）
    ToolCount int     // 工具数量（>0 时更新）
    MsgCount  int     // 消息数量（>0 时更新）
}
```

更新状态栏显示信息。

---

### `type UpdateTodosMsg struct`

```
type UpdateTodosMsg struct {
    Todos []TodoEntry  // Todo 条目列表
}
```

更新规划面板的 Todo 列表，触发侧边栏刷新。

---

### `type UpdateLogMsg struct`

```
type UpdateLogMsg struct {
    Time    string  // 时间戳
    Content string  // 日志内容
    Level   string  // 日志级别："info"/"warn"/"error"/"debug"
}
```

向日志面板追加一条日志。超过 200 条时裁剪前 100 条。

---

### `type UpdateAgentsMsg struct`

```
type UpdateAgentsMsg struct {
    Agents []AgentEntry  // 智能体条目列表
}
```

更新智能体面板列表。若当前在子会话视图且 agent 状态变化，动态刷新内容。

---

### `type UpdateCompanionMsg struct`

```
type UpdateCompanionMsg struct {
    Mood       string  // 心情："happy"/"neutral"/"shy"/"tsundere"/"sleepy"
    Intimacy   int     // 亲密度 (0-100)
    Excitement int     // 兴奋度 (0-100)
    Shyness    int     // 害羞度 (0-100)
    Fatigue    int     // 疲劳度 (0-100)
}
```

更新猫猫状态面板。

---

### `type UpdateTasksMsg struct`

```
type UpdateTasksMsg struct {
    Tasks []ScheduledTaskInfo  // 周期任务列表
}
```

更新周期任务列表。

---

### `type SessionInfoMsg struct`

```
type SessionInfoMsg struct {
    WorkspacePath  string  // 工作区路径
    PluginCount    int     // 插件数量
    MCPServerCount int     // MCP 服务器数量
}
```

更新会话信息面板。

---

### `type UpdateSessionsMsg struct`

```
type UpdateSessionsMsg struct {
    Sessions []SessionInfo  // 历史会话列表
}
```

更新历史会话列表。

---

### `type QuestionRequestMsg struct`

```
type QuestionRequestMsg struct {
    Questions []QuestionInfo        // 问题列表
    ReplyCh   chan tool.QuestionAnswer  // 回答通道
}
```

请求进入选项框模式。若当前已在选项框中，则排队到 `questionPending`。

---

### `type TickMsg time.Time`

定时器消息（每 2 秒），用于驱动侧边栏刷新和智能体 spinner 动画。

---

## 三、核心数据类型（types.go）

### `type MessageType int`

聊天消息类型枚举。

| 常量 | 值 | 显示文本 | 说明 |
|------|-----|---------|------|
| `MsgUser` | `0` | `"👤 你"` | 用户消息 |
| `MsgAssistant` | `1` | `"🤖 AI"` | AI 回复 |
| `MsgTool` | `2` | `"🔧 工具"` | 工具调用输出 |
| `MsgError` | `3` | `"❌ 错误"` | 错误信息 |
| `MsgSystem` | `4` | `"📋 系统"` | 系统提示 |

**方法**：
- `(t MessageType) String() string` — 返回显示文本（带 emoji）
- `(t MessageType) HeaderStyle() lipgloss.Style` — 返回对应类型的头部样式

---

### `type ChatMessage struct`

```
type ChatMessage struct {
    Type           MessageType  // 消息类型
    Content        string       // 消息正文（Markdown 格式）
    Thinking       string       // 思考/推理过程内容
    ThinkingFolded bool         // 是否折叠思考过程
    Sender         string       // 发送者标识
    Folded         bool         // 是否折叠（长消息自动折叠）
}
```

每条聊天消息的数据结构。支持：
- **思考过程**：`Thinking` 字段存储 LLM 推理内容，可通过 `/thinking` 命令或 `Alt+T` 切换折叠
- **自动折叠**：`MsgTool` 和 `MsgAssistant` 类型超过 12 行时自动设置 `Folded = true`

---

### `type SidebarTab int`

侧边栏面板类型。

| 常量 | 值 | 显示名 | 快捷键 |
|------|-----|-------|--------|
| `TabPlan` | `0` | `"📋 规划"` | F1 |
| `TabLog` | `1` | `"📜 日志"` | F2 |
| `TabAgents` | `2` | `"🤖 智能体"` | F3 |
| `TabCompanion` | `4` | `"🐱 猫猫"` | F5 |
| `TabTasks` | `5` | `"⏰ 任务"` | F6 |
| `TabSession` | `3` | `"💾 会话"` | F4 |

**方法**：
- `(t SidebarTab) String() string` — 返回面板显示名
- `(t SidebarTab) Shortcut() string` — 返回快捷键

---

### `type LogEntry struct`

```
type LogEntry struct {
    Time    string  // 时间戳（格式 "15:04:05"）
    Content string  // 日志内容
    Level   string  // 级别："info"/"warn"/"error"/"debug"
}
```

---

### `type TodoEntry struct`

```
type TodoEntry struct {
    Content string  // 任务内容
    Status  string  // 状态："done"/"active"/"pending"/"failed"/"completed"/"cancelled"
}
```

规划面板的 Todo 条目。

---

### `type AgentEntry struct`

```
type AgentEntry struct {
    Name        string        // 智能体名称
    ID          string        // 唯一标识
    Status      string        // 状态："pending"/"running"/"completed"/"error"/"idle"
    Task        string        // 简短任务描述
    FullTask    string        // 完整任务描述
    Spinner     int           // spinner 动画帧索引
    CurrentTool string        // 当前执行工具名
    ToolCount   int           // 已执行工具数
    StartTime   time.Time     // 开始时间
    Duration    time.Duration // 完成耗时
    ErrorMsg    string        // 错误信息
    FullOutput  string        // 格式化完整输出
}
```

子智能体状态条目。

---

### `type SessionInfo struct`

```
type SessionInfo struct {
    ID           string  // 会话 ID
    Model        string  // 模型名称
    MessageCount int     // 消息数量
    IsActive     bool    // 是否当前活跃会话
}
```

---

### `type ScheduledTaskInfo struct`

```
type ScheduledTaskInfo struct {
    ID              int64   // 任务 ID
    Name            string  // 任务名称
    Description     string  // 任务描述
    IntervalSeconds int     // 执行间隔（秒）
    Enabled         bool    // 是否启用
}
```

周期任务信息。

---

### `type QuestionInfo = tool.QuestionInfo`

问题信息类型别名（从 `catcode/tool` 包导入），包含 `Question`（问题文本）、`Options`（选项列表）、`Multiple`（是否多选）。

### `type QuestionOption = tool.QuestionOption`

选项类型别名。

### `type QuestionAnswer = tool.QuestionAnswer`

用户回答类型别名，包含 `Answers [][]string`（每个问题的选中选项）。

---

## 四、渲染系统

### `type MarkdownRenderer struct`（renderer.go）

Markdown 到终端的渲染器，支持标题、列表、引用、分隔线、代码块（含语法高亮）、行内代码、粗体、斜体。

**字段**（不导出）：
| 字段 | 类型 | 说明 |
|------|------|------|
| `width` | `int` | 渲染宽度（像素） |
| `codeStyle` | `CodeBlockStyle` | 代码块样式 |
| `syntaxTheme` | `SyntaxTheme` | 语法高亮主题 |
| `showLineNum` | `bool` | 是否显示行号 |
| `isDark` | `bool` | 是否暗色主题 |
| `codeBg` | `lipgloss.Color` | 代码块背景色 |
| `bgColor` | `lipgloss.Color` | 全局背景色 |

---

### `func NewMarkdownRenderer(width int, isDark bool) *MarkdownRenderer`

| 参数 | 类型 | 说明 |
|------|------|------|
| `width` | `int` | 渲染宽度 |
| `isDark` | `bool` | `true` 使用暗色主题，`false` 使用亮色主题 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `r` | `*MarkdownRenderer` | 渲染器实例 |

**功能**：根据主题选择对应的 `CodeBlockStyle`、`SyntaxTheme` 和背景色。

---

### `func (r *MarkdownRenderer) Render(text string) string`

| 参数 | 类型 | 说明 |
|------|------|------|
| `text` | `string` | 原始 Markdown 文本 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `rendered` | `string` | 终端格式渲染结果 |

**功能**：逐行解析 Markdown，识别代码块边界、标题、列表、引用、分隔线；代码块内部进行语法高亮。

---

### 支持的语法高亮语言

| 语言标识 | 高亮方法 | 特性 |
|---------|---------|------|
| `go`, `golang` | `highlightGo` | Go 关键字、类型、内置函数 |
| `python`, `py` | `highlightPython` | Python 关键字 |
| `javascript`, `js`, `typescript`, `ts`, `tsx`, `jsx` | `highlightJS` | JS/TS 关键字 |
| `rust`, `rs` | `highlightRust` | Rust 关键字 |
| `json`, `yaml`, `yml`, `toml`, `xml`, `html`, `css` | `highlightData` | 键值对着色 |
| `bash`, `sh`, `shell`, `zsh` | `highlightBash` | 命令高亮 |
| `diff` | `highlightDiff` | +/- 行着色 |
| 其他 | `highlightGeneric` | 数字高亮 |

---

### `type SyntaxTheme struct`

```
type SyntaxTheme struct {
    Keyword     lipgloss.Color  // 关键字颜色
    Type        lipgloss.Color  // 类型颜色
    String      lipgloss.Color  // 字符串颜色
    Number      lipgloss.Color  // 数字颜色
    Comment     lipgloss.Color  // 注释颜色
    Operator    lipgloss.Color  // 操作符颜色
    Function    lipgloss.Color  // 函数颜色
    Builtin     lipgloss.Color  // 内置函数颜色
    Punctuation lipgloss.Color  // 标点颜色
}
```

**预定义主题**：

| 变量 | 说明 |
|------|------|
| `DarkSyntax` | 暗色主题（红/蓝/紫/橙配色） |
| `LightSyntax` | 亮色主题（对应 GitHub 风格） |

---

### `type CodeBlockStyle struct`

```
type CodeBlockStyle struct {
    HeaderFg  lipgloss.Color  // 头部前景色
    HeaderBg  lipgloss.Color  // 头部背景色
    BodyFg    lipgloss.Color  // 主体前景色
    BodyBg    lipgloss.Color  // 主体背景色
    LineNumFg lipgloss.Color  // 行号前景色
}
```

---

### 辅助函数（renderer.go）

`func tokenize(line string) []string` — 代码行分词，保留空白和标点  
`func isNumber(s string) bool` — 检查字符串是否为数字字面量

---

## 五、样式系统（styles.go）

### 全局颜色常量

| 变量 | 颜色值 | 用途 |
|------|--------|------|
| `primary` | `#FF6B35` | 主色调（橙） |
| `secondary` | `#4A90D9` | 副色调（蓝） |
| `success` | `#50C878` | 成功/工具消息 |
| `warningC` | `#FFD700` | 警告 |
| `errColor` | `#FF4444` | 错误 |
| `muted` | `#888888` | 次要文本 |
| `bg` | `#1a1b26` | 全局背景色（暗色） |
| `fg` | `#c0caf5` | 全局前景色 |
| `panelBg` | `#24283b` | 面板背景色 |
| `borderFg` | `#414868` | 边框颜色 |
| `accent` | `#58A6FF` | 强调色（蓝） |

---

### 全局样式变量

| 变量 | 说明 |
|------|------|
| `titleStyle` | 标题样式：primary 粗体 |
| `statusStyle` | 状态栏样式：muted 前景 |
| `panelStyle` | 默认面板：圆角边框，borderFg |
| `panelActiveStyle` | 激活面板：primary 边框 |
| `userBorderStyle` | 用户消息边框：左竖线 secondary |
| `aiContentStyle` | AI 消息正文：fg 前景 |
| `toolContentStyle` | 工具消息：success 斜体 |
| `errContentStyle` | 错误消息：errColor |
| `sysContentStyle` | 系统消息：muted 斜体 |
| `userHeaderStyle` | 用户头部：secondary 粗体 |
| `aiHeaderStyle` | AI 头部：primary 粗体 |
| `toolHeaderStyle` | 工具头部：success 粗体 |
| `errHeaderStyle` | 错误头部：errColor 粗体 |
| `sysHeaderStyle` | 系统头部：muted 斜体 |
| `sidebarTitleStyle` | 侧边栏标题：primary 粗体 |
| `tabActiveStyle` | 激活 Tab：bg 前景 primary 背景 |
| `tabInactiveStyle` | 未激活 Tab：muted 前景 bg 背景 |
| `inputStyle` | 输入框：圆角边框 borderFg |
| `inputFocusedStyle` | 聚焦输入框：primary 边框 |
| `helpStyle` | 帮助栏：muted |
| `progressBarStyle` | 进度条前景：success |
| `progressBgStyle` | 进度条背景：borderFg |
| `separatorStyle` | 分隔符：borderFg |
| `thinkingFoldedStyle` | 折叠的思考过程：muted 斜体 |
| `thinkingTextStyle` | 思考文本：muted 斜体 |
| `thinkingBorderStyle` | 思考过程边框：左竖线灰色 |

**侧边栏图标**：

| 变量 | 值 |
|------|-----|
| `sidebarItemDone` | `"✅"` (success 色) |
| `sidebarItemActive` | `"🔄"` (warningC 色) |
| `sidebarItemPending` | `"⬜"` (muted 色) |
| `sidebarItemFailed` | `"❌"` (errColor 色) |

---

### `var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}`

流式输出和智能体动画的 spinner 帧序列。

---

### `func ProgressBar(percent int, width int) string`

| 参数 | 类型 | 说明 |
|------|------|------|
| `percent` | `int` | 进度百分比 (0-100)，自动钳位 |
| `width` | `int` | 总宽度（最小 10） |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `bar` | `string` | 形如 `[████░░░░]  50%` 的进度条 |

---

### 辅助函数（messages.go）

`func truncStr(s string, maxLen int) string` — 截断字符串到 `maxLen` 个 rune，末尾追加 `…`  
`func nowTime() string` — 返回当前时间 `"15:04:05"` 格式  
`func mutedStyle(s string) string` — 使用 muted 颜色渲染文本  
`func warningStyle(s string) string` — 使用 warningC 颜色渲染文本  
`func errStyle(s string) string` — 使用 errColor 颜色渲染文本  
`func accentStyle(s string) string` — 使用 accent 颜色渲染文本  
`func boldStyle(s string) string` — 使用 primary 粗体渲染文本  
`func max(a, b int) int` — 返回较大值  
`func wrapText(text string, maxWidth int) string` — 按字符宽度换行

---

### Model 公开方法（messages.go）

### `func (m *Model) SetOnSidebarWidthChange(cb func(int))`

设置侧边栏宽度变更回调。

### `func (m *Model) SetOnTick(cb func())`

设置周期任务回调（每次 TickMsg 触发时调用）。

### `func (m *Model) SidebarWidth() int`

返回当前侧边栏宽度。

### `func (m *Model) SetAgentList(agents []AgentInfo)`

设置 @mention 自动补全的智能体列表。

### `func (m *Model) HasPendingInput() bool`

是否有缓存的用户输入（流式期间插入的）。

### `func (m *Model) TakePendingInput() string`

取出缓存的用户输入并清空。

---

## 六、事件适配器（adapters.go）

适配器将 `Model` 的内部方法包装为 `component` 包的接口，供外部（插件等）使用。

### `type statusAdapter struct`

实现 `component.StatusDisplay` 接口。

| 方法 | 说明 |
|------|------|
| `View() string` | 调用 `statusBar.View()` 或 `Model.renderStatus()` |
| `SetWidth(w int)` | 设置宽度 |
| `SetModelInfo(model, tools, roles, msgs int)` | 更新模型信息 |
| `SetStreamStatus(status string)` | 更新流式状态 |
| `Name() string` | 返回 `"status"` |

---

### `type chatAdapter struct`

实现 `component.ChatDisplay` 和 `component.Scrollable` 接口。

| 方法 | 说明 |
|------|------|
| `View() string` | 返回 viewport 视图 |
| `SetWidth(w int)` | 设置宽度 |
| `AddMessage(msgType, content)` | 调用 `Model.addMsg()` 添加消息 |
| `AppendStream(text)` | 追加流式文本并刷新 |
| `StreamDone()` | 重置流式状态 |
| `Refresh()` | 调用 `Model.refreshChat()` |
| `ScrollToBottom()` | viewport 滚动到底部 |
| `ScrollUp(n)` | 向上滚动 n 行 |
| `ScrollDown(n)` | 向下滚动 n 行 |
| `ScrollToTop()` | 滚动到顶部 |

---

### `type sidebarAdapter struct`

实现 `component.SidebarDisplay` 和 `component.Scrollable` 接口。

| 方法 | 说明 |
|------|------|
| `View() string` | 渲染 Tab 栏 + viewport 视图 |
| `SetWidth(w int)` | 设置宽度 |
| `SwitchTab(tab int)` | 切换面板，重置子会话状态 |
| `NextTab()` | 切换到下一个面板 |
| `SetTodos(todos)` | 转换并设置 Todo 条目 |
| `SetLogs(logs)` | 转换并设置日志条目 |
| `SetAgents(agents)` | 转换并设置智能体条目 |
| `Refresh()` | 刷新侧边栏 |
| `ScrollUp(n)` / `ScrollDown(n)` / `ScrollToTop()` / `ScrollToBottom()` | 侧边栏 viewport 滚动 |

---

### `type inputAdapter struct`

实现 `component.InputDisplay` 接口。

| 方法 | 说明 |
|------|------|
| `View() string` | 渲染 textarea（带聚焦样式） |
| `SetWidth(w int)` | 设置宽度 |
| `Focused() bool` | 返回 textarea 是否聚焦 |
| `SetHelpText(text)` | 设置帮助文本 |
| `HelpView() string` | 渲染帮助栏 |

---

## 七、@ 命令系统

### `type AtCommand struct`（at_parser.go）

```
type AtCommand struct {
    AgentType string  // 子智能体类型
    Task      string  // 任务描述
    IsAtCmd   bool    // 是否为 @ 命令
}
```

---

### `var ValidAgentTypes`

```
map[string]bool{"explore": true, "plan": true, "general": true, "reviewer": true, "verifier": true, "lean4": true}
```

有效的子智能体类型集合。

---

### `func ParseAtCommand(input string) AtCommand`

| 参数 | 类型 | 说明 |
|------|------|------|
| `input` | `string` | 用户原始输入 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `cmd` | `AtCommand` | 解析结果，`IsAtCmd` 为 `true` 表示成功匹配 |

**功能**：解析 `@agent 任务描述` 格式的命令。若不以 `@` 开头或 agent 类型无效，返回 `IsAtCmd = false`。

---

### `func AllAgentTypes() []string`

返回所有有效 agent 类型的列表。

---

## 八、@mention 自动补全（mention.go）

### `type AgentInfo struct`

```
type AgentInfo struct {
    Name        string  // 智能体名称
    Description string  // 描述文本
}
```

---

### `type MentionState struct`

```
type MentionState struct {
    Active   bool         // 是否正在显示菜单
    Query    string       // 用户输入的 @ 后文本
    Agents   []AgentInfo  // 匹配的智能体列表
    Selected int          // 当前选中索引
}
```

---

### `func CheckMention(text string, agents []AgentInfo) *MentionState`

| 参数 | 类型 | 说明 |
|------|------|------|
| `text` | `string` | 当前 textarea 内容 |
| `agents` | `[]AgentInfo` | 可用智能体列表 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `ms` | `*MentionState` | 若触发 @mention 则返回状态，否则 `nil` |

**功能**：检测 textarea 中最后一个 `@` 符号（在行首或空格后），若后续文本匹配智能体名称前缀且无空格，则激活自动补全。

---

### `func RenderMention(ms *MentionState, maxWidth int) string`

| 参数 | 类型 | 说明 |
|------|------|------|
| `ms` | `*MentionState` | @mention 状态 |
| `maxWidth` | `int` | 菜单最大宽度 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `view` | `string` | 弹出菜单的终端渲染字符串 |

---

### `func (m *Model) HandleMentionKey(key string) bool`

| 参数 | 类型 | 说明 |
|------|------|------|
| `key` | `string` | 按键字符串 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `handled` | `bool` | 是否已处理该按键 |

**支持的按键**：
- `↑` / `k` — 上移选中项
- `↓` / `j` — 下移选中项
- `Enter` / `Tab` — 确认选中，替换 textarea 中的 `@` 文本为 `@agent `
- `Esc` — 关闭菜单

---

## 九、选项框模式（question.go）

### `func (m *Model) EnterQuestionMode(questions []tool.QuestionInfo, replyCh chan tool.QuestionAnswer)`

| 参数 | 类型 | 说明 |
|------|------|------|
| `questions` | `[]tool.QuestionInfo` | 问题列表 |
| `replyCh` | `chan tool.QuestionAnswer` | 回答通道 |

**功能**：进入选项框模式。若已在选项框中，将请求加入 `questionPending` 队列排队。

---

### `var questionPanelStyle`

选项框面板样式：`MaxWidth(80)`，`MarginTop(1)`。

**键盘操作**：

| 按键 | 功能 |
|------|------|
| `↑` / `k` | 上移选中项 |
| `↓` / `j` | 下移选中项 |
| `空格` | 切换多选选项（仅多选模式） |
| `Enter` | 确认当前问题（单选自动选中当前），跳到下一题；最后一题提交 |
| `Tab` | 跳到下一题 |
| `Esc` | 取消，提交空答案 |
| `PgUp/PgDown/Home/End` | 被拦截，防止意外滚动 |

---

## 十、状态栏和侧边栏（status_bar.go, sidebar_render.go）

### `func (m *Model) renderStatus() string`

渲染状态栏：`🐱 catcode | {模型名} | {工具数}工具 {角色数}角色 | {消息数}条消息`。

### `func (m *Model) renderSidebarTabs() string`

渲染侧边栏顶部的 Tab 切换栏，激活的 Tab 使用反色样式。

### `func (m *Model) renderSearchBar() string`

渲染搜索栏（Ctrl+F 触发）。

### `func (m *Model) renderHelpContent() string`

渲染快捷键帮助面板（Markdown 表格）。

---

### 侧边栏面板渲染

| 方法 | 面板 | 内容 |
|------|------|------|
| `renderPlanTab(sb)` | F1 规划 | 进度条 + Todo 列表（带状态图标） |
| `renderLogTab(sb)` | F2 日志 | 最近 50 条日志（按级别着色） |
| `renderAgentsTab(sb)` | F3 智能体 | 子智能体列表（状态动画、选中导航） |
| `renderCompanionTab(sb)` | F5 猫猫 | 心情 + 四个状态条（亲密度/兴奋度/害羞度/疲劳度） |
| `renderTasksTab(sb)` | F6 任务 | 周期任务列表（ID/名称/间隔/启用状态） |
| `renderSessionTab(sb)` | F4 会话 | 工作区信息 + 智能体列表 + 扩展信息 + 历史会话 |

---

### 子会话视图

| 方法 | 说明 |
|------|------|
| `enterSubSession(idx int)` | 进入子会话视图，显示选中 agent 的完整输出 |
| `refreshSubSessionContent(agent AgentEntry)` | 刷新子会话内容（名称、任务、状态、耗时、错误、Markdown 输出） |

---

## 十一、Component 包（component/）

### 基础类型

#### `type Rect struct`

```
type Rect struct{ X, Y, Width, Height int }
```

屏幕矩形区域，包含 `Contains(col, row int) bool` 方法。

---

#### `type MessageType int`

与 `tui.MessageType` 一致的枚举（`MsgUser`, `MsgAssistant`, `MsgTool`, `MsgError`, `MsgSystem`）。

---

### `type Component interface`

组件基础接口。

```
type Component interface {
    Name() string
    Update(msg tea.Msg) (Component, tea.Cmd)
    View() string
    Focused() bool
    Focus() tea.Cmd
    Blur()
    HandleMouse(msg tea.MouseMsg) (bool, tea.Cmd)
    Bounds() Rect
    SetBounds(r Rect)
    Visible() bool
    SetVisible(v bool)
}
```

| 方法 | 说明 |
|------|------|
| `Name()` | 返回组件名称 |
| `Update(msg)` | 处理消息，返回更新后的组件和命令 |
| `View()` | 渲染组件视图 |
| `Focused()` | 是否聚焦 |
| `Focus()` | 获取焦点 |
| `Blur()` | 失去焦点 |
| `HandleMouse(msg)` | 处理鼠标事件，返回是否消费和命令 |
| `Bounds()` | 获取组件矩形区域 |
| `SetBounds(r)` | 设置组件矩形区域 |
| `Visible()` | 是否可见 |
| `SetVisible(v)` | 设置可见性 |

---

### `type Scrollable interface`

```
type Scrollable interface {
    Component
    ScrollUp(n int)
    ScrollDown(n int)
    ScrollToTop()
    ScrollToBottom()
}
```

可滚动组件接口，继承 `Component`，增加滚动控制方法。

---

### `type DialogComponent interface`

```
type DialogComponent interface {
    Component
    Result() <-chan interface{}
}
```

对话框组件接口，继承 `Component`，增加结果通道。

---

### `type Renderable interface`

```
type Renderable interface {
    View() string
}
```

最小可渲染接口。

---

### `type WidthAware interface`

```
type WidthAware interface {
    Renderable
    SetWidth(w int)
}
```

需要宽度信息的组件接口。

---

### 显示接口

#### `type StatusDisplay interface`

```
type StatusDisplay interface {
    WidthAware
    SetModelInfo(model string, tools, roles, msgs int)
    SetStreamStatus(status string)
}
```

| 方法 | 说明 |
|------|------|
| `SetModelInfo(model, tools, roles, msgs)` | 更新模型/工具/角色/消息数 |
| `SetStreamStatus(status)` | 更新流式状态文本 |

---

#### `type ChatDisplay interface`

```
type ChatDisplay interface {
    WidthAware
    Scrollable
    AddMessage(msgType MessageType, content string)
    AppendStream(text string)
    StreamDone()
    Refresh()
    ScrollToBottom()
}
```

| 方法 | 说明 |
|------|------|
| `AddMessage(msgType, content)` | 添加聊天消息 |
| `AppendStream(text)` | 追加流式文本 |
| `StreamDone()` | 标记流式结束 |
| `Refresh()` | 刷新聊天区 |
| `ScrollToBottom()` | 滚动到底部 |

---

#### `type SidebarDisplay interface`

```
type SidebarDisplay interface {
    WidthAware
    Scrollable
    SwitchTab(tab int)
    NextTab()
    SetTodos(todos []TodoEntry)
    SetLogs(logs []LogEntry)
    SetAgents(agents []AgentEntry)
    Refresh()
}
```

| 方法 | 说明 |
|------|------|
| `SwitchTab(tab)` | 切换到指定面板 |
| `NextTab()` | 切换到下一个面板 |
| `SetTodos(todos)` | 设置 Todo 列表 |
| `SetLogs(logs)` | 设置日志列表 |
| `SetAgents(agents)` | 设置智能体列表 |
| `Refresh()` | 刷新侧边栏 |

---

#### `type InputDisplay interface`

```
type InputDisplay interface {
    WidthAware
    Focused() bool
    View() string
    SetHelpText(text string)
    HelpView() string
}
```

| 方法 | 说明 |
|------|------|
| `Focused()` | 输入区是否聚焦 |
| `SetHelpText(text)` | 设置帮助文本 |
| `HelpView()` | 渲染帮助文本视图 |

---

#### `type QuestionDisplay interface`

```
type QuestionDisplay interface {
    Renderable
    Activate(questions []QuestionInfo)
    IsActive() bool
    SetReply(reply interface{})
}
```

| 方法 | 说明 |
|------|------|
| `Activate(questions)` | 激活选项框，设置问题列表 |
| `IsActive()` | 是否处于激活状态 |
| `SetReply(reply)` | 预设回复（no-op） |

---

### `type BaseComponent struct`

组件接口的默认实现，包含 `name`、`bounds`、`visible`、`focused` 字段。

#### `func NewBaseComponent(name string) BaseComponent`

创建基础组件。

**方法**：`Name()`, `Focused()`, `Focus()`, `Blur()`, `HandleMouse()`, `Bounds()`, `SetBounds()`, `Visible()`, `SetVisible()`

---

### `type StatusBarComponent struct`

```
type StatusBarComponent struct {
    BaseComponent
    ModelName    string
    ToolCount    int
    RoleCount    int
    SessionMsgs  int
    StreamStatus string
    width        int
}
```

#### `func NewStatusBar() *StatusBarComponent`

创建状态栏组件。

| 方法 | 说明 |
|------|------|
| `View() string` | 渲染状态栏：`🐱 catcode \| {模型} \| {工具数}工具` 居左，消息数居右 |
| `Update(msg)` | no-op |
| `SetBounds(r)` | 设置边界并更新宽度 |

---

### `type QuestionComponent struct`

```
type QuestionComponent struct {
    BaseComponent
    active    bool
    questions []QuestionInfo
    answers   [][]string
    selected  int
    tab       int
    resultCh  chan interface{}
}
```

#### `func NewQuestionComponent() *QuestionComponent`

创建选项框组件。

| 方法 | 说明 |
|------|------|
| `Activate(questions)` | 激活并设置问题列表 |
| `IsActive() bool` | 是否激活 |
| `SetReply(reply)` | no-op |
| `Result() <-chan interface{}` | 返回结果通道（DialogComponent） |
| `Update(msg)` | 处理键盘消息 |
| `View() string` | 渲染选项框界面 |

---

### 子包数据类型

| 类型 | 字段 |
|------|------|
| `TodoEntry` | `Content, Status string` |
| `LogEntry` | `Time, Content, Level string` |
| `AgentEntry` | `Name, Status, Task string; ToolCount int; FullOutput string` |
| `SessionInfo` | `ID, Title string; MsgCount int` |
| `QuestionInfo` | `Header string; Options []string` |

---

## 十二、Manager 包（manager/）

### `type UIManager struct`

UI 中央控制器，负责布局计算和全局状态管理。

```
type UIManager struct {
    width  int
    height int
    showThinking bool
}
```

---

### `func NewUIManager() *UIManager`

创建 UI 管理器。

---

### 方法

| 方法 | 签名 | 说明 |
|------|------|------|
| `SetSize` | `(width, height int)` | 更新终端尺寸 |
| `Size` | `() (int, int)` | 返回当前终端尺寸 |
| `ToggleThinking` | `() bool` | 切换思考过程显示，返回新状态 |
| `IsShowThinking` | `() bool` | 返回是否显示思考过程 |
| `LayoutChatArea` | `(sidebarWidth int) component.Rect` | 计算聊天区矩形（高度 = 终端高度 - 4） |
| `LayoutSidebar` | `(sidebarWidth int) component.Rect` | 计算侧边栏矩形（右对齐） |
| `LayoutInputArea` | `() component.Rect` | 计算输入区矩形（底部 3 行） |
| `LayoutStatusBar` | `() component.Rect` | 计算状态栏矩形（底部 1 行） |
| `LayoutOverlay` | `() component.Rect` | 计算覆盖层矩形（居中 3/4 宽 × 2/3 高） |

---

### `type MouseTarget int`

鼠标事件目标。

| 常量 | 值 |
|------|-----|
| `MouseTargetOverlay` | `0` — 覆盖层（对话框） |
| `MouseTargetInput` | `1` — 输入区 |
| `MouseTargetSidebar` | `2` — 侧边栏 |
| `MouseTargetChat` | `3` — 聊天区 |
| `MouseTargetNone` | `4` — 无目标 |

---

### `func (m *UIManager) DispatchMouse(msg tea.MouseMsg, overlayActive, inputFocused bool, mouseX, mouseY int) MouseTarget`

| 参数 | 类型 | 说明 |
|------|------|------|
| `msg` | `tea.MouseMsg` | 鼠标事件 |
| `overlayActive` | `bool` | 是否有激活的覆盖层 |
| `inputFocused` | `bool` | 输入区是否聚焦 |
| `mouseX` | `int` | 鼠标 X 坐标 |
| `mouseY` | `int` | 鼠标 Y 坐标 |

| 返回值 | 类型 | 说明 |
|--------|------|------|
| `target` | `MouseTarget` | 鼠标事件命中的目标 |

**路由优先级**：覆盖层 > 输入区 > 侧边栏 > 聊天区 > 无目标。

---

## 十三、Plugin 包（plugin/）

### `type UIAPI interface`

插件与 TUI 交互的界面接口。

```
type UIAPI interface {
    RegisterSidebarTab(title string, render func(width int) string) error
    UnregisterSidebarTab(title string) error
    ShowQuestion(questions []tool.QuestionInfo) <-chan tool.QuestionAnswer
    ShowConfirm(message string) <-chan bool
    AppendToChat(content string)
    ShowNotification(message string, level string, duration time.Duration)
    SetStatus(key, value string)
}
```

| 方法 | 说明 |
|------|------|
| `RegisterSidebarTab(title, render)` | 注册自定义侧边栏面板 |
| `UnregisterSidebarTab(title)` | 注销自定义侧边栏面板 |
| `ShowQuestion(questions)` | 显示选项对话框，返回回答 channel |
| `ShowConfirm(message)` | 显示确认对话框，返回确认 channel |
| `AppendToChat(content)` | 向聊天区追加内容 |
| `ShowNotification(message, level, duration)` | 显示通知 |
| `SetStatus(key, value)` | 设置状态栏键值 |

---

## 十四、内部交互流程

### 用户发送消息的完整流程

```
1. 用户在 textarea 输入内容，按 Ctrl+S
2. Update 收到 tea.KeyMsg("ctrl+s")
3. 若 streamStatus != ""（正在流式）→ 缓存到 pendingInput
4. 否则：addMsg(MsgUser, input) → 设置 streamStatus = "思考中" → 流式标志位置位
5. 预创建 MsgAssistant 占位消息
6. 调用 onSubmit(input) 回调（go 协程）
7. 外部通过 tea.Program.Send(StreamMsg(text)) 发送流式内容
8. Update 处理 StreamMsg：解析特殊标记，累积文本，调用 updateLastAI()
9. 流式结束时外部发送 StreamDoneMsg
10. Update 处理：清除状态，处理 pendingInput
```

### 选项框交互流程

```
1. 外部发送 QuestionRequestMsg{Questions, ReplyCh}
2. Update → EnterQuestionMode() → questionMode = true
3. 用户通过键盘交互（↑↓/空格/Enter/Tab/Esc）
4. submitQuestionAnswers() → ReplyCh <- answers
5. 检查 questionPending 队列，有则继续
```

### @mention 自动补全流程

```
1. 用户在 textarea 输入 @
2. Update 末尾调用 CheckMention(textarea.Value(), agentList)
3. 匹配到智能体 → mentionState.Active = true
4. Update 检测 Active → HandleMentionKey 处理键盘
5. View 检测 Active → RenderMention 渲染弹出菜单
6. 用户 Enter/Tab → replaceMention() 替换 textarea 内容
```

# catcode 架构设计文档

> 版本: v0.9.3 | 2026-05-17

---

## 一、总体架构

```
┌──────────────────────────────────────────────────────────────┐
│                        UI 层 (ui/tui)                        │
│                   Bubble Tea TUI + Markdown                  │
├──────────────────────────────────────────────────────────────┤
│                     智能体层 (agent/)                         │
│  ┌─────────────────────┐  ┌──────────────────────────────┐  │
│  │   Architect (主编排)  │  │   SubAgent Pool (子智能体池)   │  │
│  │  orchestrator/       │  │   pool/ (池管理+并发控制)     │  │
│  │  (ArchitectInterface)│  │   subagent/ (接口+BaseAgent)  │  │
│  │  - 对话编排           │  │   - 独立Session+Provider     │  │
│  │  - 工具执行循环       │  │   - ToolFactory动态注册       │  │
│  │  - 子智能体委派       │  │   - 会话持久化+压缩           │  │
│  │  - 上下文压缩         │  │   - GuardReviewer审查        │  │
│  └─────────────────────┘  └──────────────────────────────┘  │
├──────────────────────────────────────────────────────────────┤
│                      AI 层 (ai/)                             │
│  ┌──────────────┐ ┌─────────┐ ┌────────────────────────┐   │
│  │ session/     │ │  llm/   │ │     compact/           │   │
│  │ 会话+消息管理 │ │ Provider│ │  上下文压缩引擎          │   │
│  │ BuildRequest │ │ 接口    │ │  - ShouldCompact        │   │
│  │ 序列化/反序列化│ │ +SSE解析│ │  - SelectCompactRange   │   │
│  │ BuildClean   │ │ +重试   │ │  - BuildCompactResult   │   │
│  └──────────────┘ └─────────┘ │  - ApplyCompactResult   │   │
│                                └────────────────────────┘   │
├──────────────────────────────────────────────────────────────┤
│                    基础设施层 (core/)                         │
│  ┌──────────────┐ ┌──────────┐ ┌──────────────────┐        │
│  │ errors/      │ │ event/   │ │   config/        │        │
│  │ CatError     │ │ EventBus │ │   DB+env+YAML    │        │
│  │ ErrorCollector│ │26事件类型│ │   多层优先级      │        │
│  │ SelfCorrect  │ │ Trigger  │ │                  │        │
│  └──────────────┘ └──────────┘ └──────────────────┘        │
├──────────────────────────────────────────────────────────────┤
│                    持久化层 (data/)                           │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  storage/ — SQLite (WAL, 10表, schema v6)            │   │
│  │  embed/   — go:embed (8角色YAML+默认配置)             │   │
│  └──────────────────────────────────────────────────────┘   │
├──────────────────────────────────────────────────────────────┤
│  工具层(tool/) │ 插件(plugin/) │ MCP(mcp/) │ 调度(schedule/) │
└──────────────────────────────────────────────────────────────┘
```

---

## 二、四层通信架构

参考 opencode/claude-code，catcode 实现了严格的四层通信分离：

```
智能体层               上下文层              通信层            远程服务
(agent/)              (ai/session/)        (ai/llm/)         (API)
────────────────────────────────────────────────────────────────────
Architect/SubAgent → Session.BuildRequest → Provider.Chat → DeepSeek
       ↑                    ↑                    ↑              API
       │                    │                    │
  对话编排              消息/工具/压缩       OpenAI兼容+SSE
  ask_architect        BuildCleanRequest    ProviderRegistry
  (双向通信)           (独立请求)           (多Provider)
```

**设计原则**：
- 智能体层只通过 Session 的公共方法操作消息，不直接构造 `llm.Message`
- `answerSubAgentQuestion` 通过 `Session.BuildCleanRequest()` 构建独立请求
- 通信层通过 `Provider` 接口抽象，支持多 Provider 注册

---

## 三、上下文分层结构

### BuildRequest 输出的消息层次

```
messages[]:
  ┌─────────────────────────────────────────────────────────┐
  │ [0] system  系统提示词 (SystemPrompt)                    │
  │    - 角色定义、行为准则、工具约束                          │
  ├─────────────────────────────────────────────────────────┤
  │ [1] system  记忆索引 (MemoryIndex)                       │
  │    - 🌐 项目全局记忆 / 📁 工作区记忆                      │
  │    - BuildIndex(contextHint) 实时构建                    │
  │    - 缓存加速 (cacheDirty 失效)                          │
  ├─────────────────────────────────────────────────────────┤
  │ [2] system  上下文索引 (Summary)                         │
  │    - 压缩产生的9段结构化摘要                              │
  │    - 增量更新 (previousSummary)                          │
  ├─────────────────────────────────────────────────────────┤
  │ [3+] user/assistant/tool  对话上下文                      │
  │    - Head+Tail 分割保留最近 2 轮 + token 预算             │
  │    - 微压缩清理旧工具输出 (保留最近6条)                     │
  │    - 压缩边界标记 (SystemCompactBoundaryMessage)          │
  └─────────────────────────────────────────────────────────┘

tools[]: 工具列表提示词 (API 独立参数)
```

### 压缩流程

```
ShouldCompact(tokenCount, contextWindow)
  │ token > 60% contextWindow
  ├── micro (msgCount < 10): TrimOldToolOutputs
  └── full  (msgCount ≥ 10):
        SelectCompactRange → head+tail 分割
        BuildCompactResult → 边界+摘要
        ApplyCompactResult → 禁用旧消息+注入边界+注入摘要
        CreateSnapshot → DB 持久化
        CleanOrphanedToolCalls → 清理配对
```

---

## 四、错误处理体系

### 统一错误类型 (CatError)

```go
type CatError struct {
    Message  string    // 错误描述
    Cause    error     // 原始错误 (Unwrap)
    Category string    // API/工具/权限/LLM/网络/内部
    stack    *stack    // 调用堆栈 (runtime.Callers)
}
```

**堆栈跟踪**：`%v` 显示消息链，`%+v` 显示完整堆栈

### 错误收集器 (ErrorCollector)

- 延迟收集工具执行错误，统一注入避免破坏 tool_calls 配对
- 自纠正限制 (maxSelfCorrect=3)

### 错误持久化 (error_logs 表)

| 字段 | 说明 |
|------|------|
| category | API/工具/权限/LLM/网络/内部 |
| severity | error/warning/info |
| stack_trace | runtime.Stack 完整堆栈 |
| source | architect/subagent/llm/startup |

### Goroutine Panic 保护

所有 7 个关键 goroutine 均有 recover + logError + 堆栈捕获：
- ProcessInput, Pool.Execute, ExecuteAsync, SubAgent.Execute
- companion saveState, bash Wait, main autoSave

---

## 五、子智能体池设计

### 架构（7种子智能体类型，含 lean4）

```
Pool (并发信号量, max=3)
  ├── SubAgent["explore"]  → Session + Provider + Tools + Perms
  ├── SubAgent["plan"]     → Session + Provider + Tools + Perms
  ├── SubAgent["general"]  → Session + Provider + Tools + Perms
  ├── SubAgent["reviewer"] → Session + Provider + Tools + Perms
  ├── SubAgent["verifier"] → Session + Provider + Tools + Perms
  ├── SubAgent["guard"]    → Session + Provider + Tools + Perms
  └── SubAgent["lean4"]    → Session + Provider + Tools + Perms
```

### 生命周期

```
Pool.Execute(task)
  ├── 加载历史会话 (LoadConversation "subagent-{type}")
  ├── 上下文构建 (ContextBuilder.BuildContext — 5层注入)
  ├── LLM 对话循环:
  │     processStream → toolCalls? → executeToolCalls
  │       └── continueConversation → 压缩检查 → processStream
  └── 持久化保存 (SaveConversation)
```

### 超时机制（空闲超时）

| 场景 | 超时 | 说明 |
|------|------|------|
| 主智能体等待子智能体 | 10min 空闲超时 | task 工具等待子智能体返回，收到输出行即重置计时器 |
| Guard 审查 LLM 调用 | 60s | guard 子智能体的审查 LLM 调用超时 |
| HTTP SSE 流式 | 180s 空闲超时 | 每次收到 SSE chunk 重置计时器 |

### 安全机制

| 层 | 机制 |
|----|------|
| 正则检查 | guardCheck() — 拦截 rm -rf / dd mkfs 等 |
| LLM审查 | GuardReviewer — guard 子智能体语义审查（带缓存） |
| 权限系统 | PermissionChecker — Allow/Ask/Deny 三级 |

---

## 六、Hook 系统（yaegi 沙箱）

### 概述

Hook 系统允许使用 Go 脚本动态扩展子智能体行为，无需重新编译主程序。Hook 文件存放在 `~/.catcode/hooks/<agent_type>.go`，由 yaegi 解释器加载执行，受沙箱安全策略限制。

```
agent/subagent/hook/     # (规划中，目录已预留)
├── bridge.go    # 类型桥接 — 将 Go 类型注册到 yaegi 解释器符号表
├── builder.go   # 适配器 — YaegiContextBuilder 将编译后的 Hook 适配为 ContextBuilder 接口
├── engine.go    # 引擎 — HookEngine 单例，管理编译缓存和热重载（mtime 检测）
├── loader.go    # 加载器 — HookLoader 从磁盘加载、编译 Hook 文件，检测注入点函数
└── sandbox.go   # 沙箱 — SandboxPolicy 限制可用包（fmt/strings/time/json + catcode）
```

### ContextBuilder 接口

Hook 通过 `ContextBuilder` 接口注入到子智能体的执行生命周期：

```go
type ContextBuilder interface {
    Name() string
    BuildContext(ctx context.Context, sa *session.Session, input *ContextBuildInput) (*ContextBuildResult, error)
}
```

- **ContextBuildInput** — 输入包含任务描述、上下文摘要、智能体类型、主会话上下文、记忆索引、指令文件、主模型、扩展字段
- **ContextBuildResult** — 输出可覆盖 SystemPrompt、MemoryIndex，或附加 ExtraSystemMessages
- 子智能体在 `Execute()` 中调用 `contextBuilder.BuildContext(...)`，失败时回退到 `DefaultContextBuilder`

### Hook 注入点（4 个生命周期钩子）

| 注入点 | 函数签名 | 触发时机 |
|--------|---------|---------|
| `before_context` | `BeforeContext(sa, input)` | 上下文构建前 |
| `build_context` | `BuildContext(sa, input) *ContextBuildResult` | 上下文构建（可覆盖系统提示） |
| `after_execute` | `AfterExecute(sa, response)` | 执行完成后（仅通知） |
| `on_error` | `OnError(sa, err)` | 发生错误时 |

### 安全沙箱

Hook 脚本运行在 yaegi 的 `Unrestricted: false` 模式下，且 `SandboxPolicy` 白名单只允许：`fmt`, `strings`, `strconv`, `time`, `encoding/json`, `catcode/agent/subagent/subagent`, `catcode/ai/session/session`, `catcode/core/errors/errors`。`BuildContext` 调用有 5 秒超时限制。

### 热重载

HookEngine 通过文件 mtime 监测 Hook 文件变更，检测到修改后自动重新编译并替换 ContextBuilder，无需重启主程序。

---

## 七、插件系统

### 符号表注册 (零依赖部署)

```
buildCatcodeSymbols() → interp.Exports (136 个符号, 9 个包)
  ├── catcode/tool       (13) — Tool, FuncDef, Schema, PermissionLevel
  ├── catcode/core/event (43) — EventBus, Trigger, 31个事件常量
  ├── catcode/agent/role (15) — RoleDef, ThinkingConfig, ModelLimit
  ├── catcode/core/errors(19) — CatError, ErrorCollector, 9个类别
  ├── catcode/ai/llm     (27) — Provider, ChatRequest, StreamEvent
  ├── catcode/ai/session (4)  — Session, Message, New
  ├── catcode/ai/compact (5)  — CompactDecision, ShouldCompact
  ├── catcode/data/storage(10) — ConversationRow, MemoryService
  └── catcode/core/buffer(2)  — Buffer, New
```

---

## 八、数据库 Schema

**版本**: v6 | **表数**: 9

| 表 | 用途 |
|----|------|
| schema_version | 版本迁移追踪 |
| settings | 键值对配置 |
| agent_definitions | 智能体定义 |
| conversations | 会话元数据 |
| messages | 对话消息 (reasoning_content + enabled) |
| memory | 长期记忆 (scope + type + importance) |
| context_snapshots | 压缩快照 |
| scheduled_tasks | 周期任务 |
| error_logs | 错误日志 (category + stack_trace) |

---

## 九、SubAgent 接口化

### 概述

`agent/subagent/` 包定义了子智能体的标准化接口，将原先耦合在 `pool/` 中的 SubAgent 实现提取为独立、可测试的单元。

### 文件结构

```
agent/subagent/
├── interface.go   # SubAgent 接口 + GuardReviewer 接口 + AgentSnapshot
├── config.go      # Config 配置结构体（类型/模型/提示词/工具/权限）
├── base.go        # BaseAgent（原 pool.go SubAgent 的完整实现，923行）
└── hook/          # yaegi Hook 引擎（见第六章）
```

> **设计说明**：7 种子智能体类型（explore/plan/general/reviewer/verifier/guard/lean4）共享同一个 `BaseAgent` 实现，通过 `Config` 中的 `Type`、`SystemPrompt`、`Tools` 等字段区分行为。不再需要为每种类型创建独立文件（如 `explore.go`、`plan.go` 等），配置差异由 `AgentConfigsFromDB()` / `DefaultAgentConfigs()` 管理。

### SubAgent 接口

```go
type SubAgent interface {
    ID() string
    Type() string
    Execute(ctx context.Context, task, contextSummary string) (<-chan string, error)
    IsBusy() bool
    Snapshot() AgentSnapshot
    SetWorkspaceDB(wdb storage.WorkspaceDB)
    SetMemoryService(ms storage.MemoryService)
    SetGuardReviewer(reviewer GuardReviewer)
    SetAskArchitectCallback(fn func(question string) string)
    SetContextLimit(limit int)
    RegisterTool(t *tool.Tool) error
    ResetSession()
    GetSession() *session.Session
}
```

### GuardReviewer 接口

为避免 `agent/subagent/` 与 `agent/pool/` 的循环依赖，`SetGuardReviewer` 接受接口而非具体 `*Pool`：

```go
type GuardReviewer interface {
    Execute(ctx context.Context, agentType, task, contextSummary string) (<-chan string, error)
    GetOrCreate(agentType string) (SubAgent, error)
}
```

`agent/pool.Pool` 隐式实现此接口（已有 `Execute` 和 `GetOrCreate` 方法）。

### PoolInterface 更新

`agent/pool.PoolInterface` 中 `GetOrCreate` 的返回类型已从 `*SubAgent`（具体类型）改为 `subagent.SubAgent`（接口）。`Pool.Snapshot()` 改为通过 `inst.Snapshot()` 接口方法获取快照，而非直接访问内部字段。

### 向后兼容

`agent/pool/pool.go` 向后兼容，内部通过 `subagent.New()` 创建实例。`AgentConfigsFromDB()` 和 `DefaultAgentConfigs()` 函数仍留在 `pool` 包中，返回 `map[string]subagent.Config`。

### 5 层上下文注入

Execute() 向子智能体注入 5 层上下文，确保 contextSummary 不再为空：

```
[0] system  SystemPrompt (从 embed YAML 读取)
[1] system  MemoryIndex (数据库记忆索引)
[2] system  上下文摘要 (contextSummary — 主智能体传入)
[3] system  指令文件 (AGENTS.md/CLAUDE.md/.catcode/instructions.md)
[4] user    任务描述 (task)
```

---

## 十、压缩设计（借鉴 claude-code + opencode）

### 核心类型

```
CompactDecision   — 压缩决策 (needed/level/reason)
CompactResult     — 压缩结果 (boundary+summary+tailStart)
SplitResult       — Head+Tail 分割 (headStart/headEnd/tailStart)
```

### 算法流程

```
1. ShouldCompact → token > 60% contextWindow?
2. SelectCompactRange → 保留 2轮 + 25% token预算
3. BuildCompactResult → 创建边界标记 + 摘要
4. ApplyCompactResult → 禁用旧消息、注入边界、注入摘要
5. TrimOldToolOutputs → 清理旧 tool 输出
6. CleanOrphanedToolCalls → 确保 API 消息配对合法
```

### 关键参数

| 参数 | 值 | 含义 |
|------|-----|------|
| AutoCompactBufferRatio | 0.60 | 60% 占用触发 |
| PreserveTurns | 2 | 保留最近2轮 |
| PreserveTokenFraction | 25% | 25% token预算 |
| MicroKeepTools | 6 | 保留最近6条工具输出 |

### 空闲超时机制

所有等待类操作均采用**空闲超时**策略（收到数据时重置计时器），避免在网络波动或子智能体持续输出时误杀：

| 场景 | 旧超时 | 新空闲超时 |
|------|:---:|:---:|
| HTTP SSE 流式 | 120s 硬超时 | 180s 空闲超时 |
| 子智能体等待 (task工具) | 5min 硬超时 | 10min 空闲超时 |
| Guard 审查 LLM 调用 | 15s | 60s |

空闲超时实现：每次收到流式数据（SSE chunk / 子智能体输出行）时重置 `time.Timer`，只有连续无数据超过阈值才判定超时。

---

## 十一、TUI 组件化架构

### 概述

`ui/tui/` 包采用组件化架构，将原先单一 `tui.go`（2000+ 行）拆分为 Component / Manager / Plugin 三层。

### 架构

```
ui/tui/
├── tui.go              # 主模型（Model），Bubble Tea Update/View 入口
├── renderer.go         # Markdown 渲染 + 语法高亮 + 折叠
├── at_parser.go        # @ 提及解析器
├── mention.go          # 提及自动补全
├── question.go         # Question 工具 UI 处理（内嵌于 Model）
├── adapters.go         # Component 接口的适配器实现
├── component/          # UI 组件层
│   ├── component.go    #   Component/Scrollable/DialogComponent 接口 + Rect + BaseComponent
│   ├── renderable.go   #   渲染接口（Renderable/ChatDisplay/SidebarDisplay/QuestionDisplay 等）
│   ├── status.go       #   StatusBarComponent — 底部状态栏
│   └── question_component.go  # QuestionComponent — 实现 DialogComponent + QuestionDisplay
├── manager/            # 控制层 ✅ (v0.9.2 已实现)
│   ├── manager.go      #   UIManager — 中央控制器（布局/焦点/全局状态）
│   └── mouse.go        #   鼠标事件路由（覆盖层→输入区→侧边栏→聊天区）
└── plugin/             # 插件 API 层 ✅ (v0.9.2 已实现)
    └── api.go          #   UIAPI 接口（7个方法）
```

### Component 接口

```go
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

### 6 个核心组件

| 组件 | 类型 | 说明 |
|------|------|------|
| ChatComponent | Scrollable | 聊天消息列表，流式追加，支持折叠/展开 |
| SidebarComponent | Scrollable | 多 Tab 侧边栏，鼠标点击切换 Tab |
| StatusBarComponent | Component | 底部状态信息（模型/Token/子智能体状态） |
| InputComponent | Component | 输入区域，@mention 自动补全 |
| QuestionComponent | DialogComponent | 选项对话框覆盖层，Result() channel 返回答案 |
| ChatMessage | — | 单条消息渲染（支持 reasoning 折叠卡片） |

### UIManager 中央控制器

```go
type UIManager struct {
    chatArea     *component.ChatComponent
    sidebar      *component.SidebarComponent
    statusBar    *component.StatusBarComponent
    inputArea    *component.InputComponent
    overlay      component.Component          // 覆盖层（QuestionComponent 等）
    showThinking bool                          // 全局推理显示开关
}
```

核心职责：
- **布局计算**：根据终端尺寸动态分配各组件 bounds
- **焦点管理**：输入区 ↔ 覆盖层 ↔ 侧边栏焦点切换
- **鼠标路由**：`dispatchMouse(msg)` 按优先级路由鼠标事件（覆盖层 > 输入区 > 侧边栏 > 聊天区）
- **全局开关**：Alt+T 或 `/thinking` 命令切换 `showThinking` 状态

### 鼠标支持

`manager/mouse.go` 实现全局鼠标事件路由：
1. 覆盖层优先 — QuestionComponent 对话框
2. 输入区 — 点击定位光标、文字选择
3. 侧边栏 — 点击 Tab 切换、滚轮滚动
4. 聊天区 — 滚轮滚动消息历史

鼠标事件转换为局部坐标后分发给对应组件，实现独立组件的鼠标交互。

### UIAPI 插件接口

```go
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

插件可通过 `UIAPI` 添加自定义侧边栏 Tab、弹出提问对话框、向聊天区追加内容，无需直接操作 TUI 内部状态。

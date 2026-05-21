# cmd/catcode — 应用入口

## 包概述

`cmd/catcode` 是 catcode 项目的可执行入口包（`package main`）。它负责：

1. **命令行参数解析** — 支持 `--repl`、`--model`、`--temperature`、`--workspace` 等选项，以及对应的短选项
2. **工作区发现** — 自动从当前目录向上搜索 `.catcode/` 或 `.git/` 目录作为工作区根目录
3. **模块初始化链** — 按依赖顺序初始化工作区数据库、配置、事件总线、角色系统、Provider、子智能体池、规划引擎、插件系统、MCP 连接
4. **工具注册** — 注册所有内置工具（read、write、edit、glob、grep、diff、webfetch、skill、task、todo 等）到主智能体
5. **双模式启动** — 默认启动 TUI（终端用户界面）模式，也可通过 `-r` 参数进入 REPL（命令行交互）模式
6. **会话持久化** — 退出时自动保存对话历史到工作区数据库

### 初始化流程图

```
main()
  ├── 解析命令行参数 (--repl, --model, --temperature, --workspace, --help)
  ├── printBanner()                                 // 打印启动横幅
  ├── discoverWorkspace()                           // 发现工作区根目录
  ├── storage.OpenWorkspace(workDir)                // 打开/创建工作区数据库
  ├── storage.InitMachineSecret(workDir)            // 初始化机器密钥
  ├── wdb.Seed()                                    // 写入种子数据/迁移配置
  ├── builtin.LoadGuardPatterns(wdb)                // 加载自定义守卫规则
  ├── storage.NewMemoryService(wdb)                 // 初始化记忆服务
  ├── builtin.LoadCompanionState(wdb)               // 加载猫猫状态
  ├── config.LoadFromWorkspace(wdb, model, temp)     // 加载配置(DB+环境+CLI)
  ├── event.NewBus()                                // 初始化事件总线
  ├── schedule.NewScheduler(...)                    // 初始化空闲调度器
  ├── role.NewRegistry(bus)                         // 初始化角色注册表
  ├── roleReg.LoadFromWorkspace(wdb, workDir)       // 从 DB 和 .catcode/roles/ 加载角色
  ├── role.WatchUserRoles(workDir, callback)        // 启用角色文件热重载
  ├── initProviders(cfg, wdb)                       // 初始化所有 LLM Provider
  ├── agent.NewPool(...)                            // 初始化子智能体池
  ├── plan.NewEngine(bus)                           // 初始化规划引擎
  ├── orchestrator.NewArchitect(...)                // 创建主智能体
  ├── arch.LoadHistory(msgs)                        // 恢复历史会话(如有)
  ├── registerBuiltinTools(arch, app)               // 注册内置工具
  ├── plugin.NewManager(...).LoadAll()              // 加载插件
  ├── mcp.NewManager().ConnectServer(...)           // 连接 MCP 服务器
  ├── 注册事件回调 (role.list, tui.agents, tui.plan, etc.)
  ├── 打印就绪状态 (工具数/子智能体数/陪伴数)
  ├── [分支]
  │   ├── runTUI(arch, cfg, app)                    // TUI 模式
  │   └── runREPL(arch, app)                        // REPL 模式
  └── cleanup(arch, app)                            // 退出清理
```


### 文件结构

包实现已按功能拆分为 6 个源文件：

| 文件 | 说明 |
|------|------|
| `main.go` | 入口函数 `main()`、`Application` 结构体、`Version` 常量、`discoverWorkspace()`、`printBanner()`、`printCLIHelp()`、`printHelp()` |
| `main_init.go` | Provider 初始化（`initProviders()`、`promptForAPIKey()`） |
| `main_tui.go` | TUI 模式启动（`runTUI()`），包含 Bubble Tea 程序初始化、用户输入处理、子智能体委派 |
| `main_repl.go` | REPL 模式启动（`runREPL()`），包含命令行交互循环、`handleCommand()`、`listRolesHandler()`、`printStatus()` |
| `main_register.go` | 工具注册（`registerBuiltinTools()`），通过 `BuiltinRegistry` 遍历注册内置工具 + `task`/`todo` 内联工具 |
| `main_events.go` | 事件注册（`registerEventCallbacks()`），订阅事件总线并将状态变更转发到 TUI |


---

## 常量

### `Version`

```
const Version = "0.9.2"
```

当前 catcode 版本号，用于横幅显示和帮助信息输出。

---

## 类型

### `Application`

```
type Application struct {
    Wdb           storage.WorkspaceDB
    MemoryService storage.MemoryService
    Bus           event.EventBus
    RoleReg       role.RegistryInterface
    AgentPool     agent.PoolInterface
    PlanEngine    plan.PlanEngineInterface
    Provider      llm.Provider
    Scheduler     *schedule.Scheduler
    PluginMgr     *plugin.Manager
    McpMgr        *mcp.Manager
    WorkDir       string
    TUIProgram    *tea.Program
    TUIModel      *tui.Model
}
```

应用依赖容器，集中管理所有模块引用。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Wdb` | `storage.WorkspaceDB` | 工作区数据库实例，存储配置、会话、智能体定义、记忆等 |
| `MemoryService` | `storage.MemoryService` | 记忆服务接口，提供记忆的增删改查操作 |
| `Bus` | `event.EventBus` | 事件总线，用于模块间解耦通信 |
| `RoleReg` | `role.RegistryInterface` | 角色注册表，管理主智能体和子智能体的角色定义 |
| `AgentPool` | `agent.PoolInterface` | 子智能体池，管理子智能体的并发执行 |
| `PlanEngine` | `plan.PlanEngineInterface` | 规划引擎，管理任务分解与进度跟踪 |
| `Provider` | `llm.Provider` | 默认 LLM Provider，用于 AI 模型调用 |
| `Scheduler` | `*schedule.Scheduler` | 空闲调度器，在用户无操作 30 秒后触发周期任务 |
| `PluginMgr` | `*plugin.Manager` | 插件管理器，加载和管理 `.catcode/plugins/` 中的插件 |
| `McpMgr` | `*mcp.Manager` | MCP 管理器，管理与外部 MCP 服务器的连接 |
| `WorkDir` | `string` | 工作区根目录的绝对路径 |
| `TUIProgram` | `*tea.Program` | Bubble Tea TUI 程序实例，仅在 TUI 模式下非 nil |
| `TUIModel` | `*tui.Model` | TUI 数据模型，管理 TUI 的状态和渲染，仅在 TUI 模式下非 nil |

---

## 函数

### `main()`

```
func main()
```

**参数**: 无（通过 `os.Args` 读取命令行参数）

**返回值**: 无（通过 `os.Exit` 或自然结束）

**功能描述**:

catcode 应用的唯一入口函数。按以下顺序完成初始化和启动：

1. 解析命令行参数，支持 `--repl`/`-r`（REPL 模式）、`--model`/`-m`（模型名称）、`--temperature`/`-t`（温度参数）、`--workspace`/`-w`（工作区目录）、`--help`/`-h`（帮助）
2. 打印启动横幅
3. 创建工作区目录并打开/初始化工作区数据库
4. 加载配置（数据库设置 → 环境变量 → CLI 参数，优先级递增）
5. 初始化事件总线、空闲调度器、角色系统、Provider、子智能体池、规划引擎
6. 创建主智能体并恢复历史会话
7. 注册内置工具，加载插件，连接 MCP 服务器
8. 注册事件回调
9. 根据 `useREPL` 标志选择进入 TUI 或 REPL 模式
10. 退出时调用 `cleanup()` 保存会话并释放资源

---

### `discoverWorkspace()`

```
func discoverWorkspace() string
```

**参数**: 无

**返回值**: `string` — 工作区根目录的绝对路径

**功能描述**:

从当前目录向上搜索工作区根目录，按以下优先级：

1. **环境变量** — 检查 `CATCODE_WORKSPACE` 环境变量，若设置则直接返回
2. **向上搜索 `.catcode/`** — 从当前工作目录开始，逐级向上查找 `.catcode/` 目录（已初始化的 catcode 工作区）
3. **向上搜索 `.git/`** — 若未找到 `.catcode/`，则继续向上查找 `.git/` 目录（git 仓库根目录），因为 catcode 工作区可能与 git 仓库根目录一致
4. **回退** — 若搜索到文件系统根目录仍未找到，则返回当前工作目录作为工作区

搜索终止条件：到达文件系统根目录（`parent == dir`）时停止。

---

### `initProviders()`

```
func initProviders(cfg *config.Config, wdb storage.WorkspaceDB) *llm.ProviderRegistry
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `cfg` | `*config.Config` | 应用配置，包含 `Providers` map（name → ProviderConfig），每个 ProviderConfig 包含 Name、BaseURL、APIKey |
| `wdb` | `storage.WorkspaceDB` | 工作区数据库，用于交互式输入 API Key 后持久化存储 |

**返回值**: `*llm.ProviderRegistry` — 包含所有已初始化的 LLM Provider 的注册表

**功能描述**:

遍历配置中的所有 Provider，逐一初始化。对每个 Provider：

1. **获取 API Key**（4级回退）：
   - 首先从配置的 `APIKey` 字段读取
   - 若为空，从环境变量 `<NAME>_API_KEY` 读取（变量名 = Provider 名称大写 + `_API_KEY`）
   - 若仍为空，回退到 `OPENAI_API_KEY` 环境变量
   - 若仍为空，从数据库 `settings` 表读取（key: `providers.<name>.api_key`），并通过 `storage.DecryptAPIKey()`（定义于 `data/storage/secrets.go`）解密
   - 若全部为空，跳过该 Provider 并打印警告
2. **获取 Base URL**：若 Base URL 为空，跳过该 Provider
3. **创建 Provider**：调用 `llm.NewOpenAI(name, baseURL, apiKey)` 创建 OpenAI 兼容的 Provider 实例
4. **注册到注册表**：第一个 Provider 作为默认 Provider 构造注册表，后续 Provider 通过 `registry.Register(name, prov)` 注册
5. **无可用 Provider 时**：若循环结束后没有成功初始化任何 Provider，调用 `promptForAPIKey()` 进行交互式 API Key 输入

---

### `promptForAPIKey()`

```
func promptForAPIKey(cfg *config.Config, wdb storage.WorkspaceDB) *llm.ProviderRegistry
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `cfg` | `*config.Config` | 应用配置，用于显示 Provider 名称和存储 API Key 到运行时配置 |
| `wdb` | `storage.WorkspaceDB` | 工作区数据库，用于持久化存储用户输入的 API Key（key: `providers.<name>.api_key`） |

**返回值**: `*llm.ProviderRegistry` — 成功时返回包含第一个成功初始化的 Provider 的注册表；所有 Provider 都跳过时返回 `nil`

**功能描述**:

当没有任何 Provider 可通过环境变量或配置文件获取 API Key 时，进入交互式 API Key 输入模式：

1. 打印提示框，列出所有配置了 Base URL 的 Provider
2. 逐个提示用户输入 API Key（直接回车跳过）
3. 用户输入后：
   - 先通过 `storage.EncryptAPIKey()`（定义于 `data/storage/secrets.go`）对 API Key 进行 AES-256-GCM 加密
   - 将加密后的 API Key 存入工作区数据库（setting key: `providers.<name>.api_key`）
   - 更新运行时配置中的 `cfg.Providers[name].APIKey`
   - 立即创建对应的 Provider 实例并返回注册表
4. 若所有 Provider 都被跳过，返回 `nil`，调用方将退出程序

---

### `registerBuiltinTools()`

```
func registerBuiltinTools(arch orchestrator.ArchitectInterface, app *Application)
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `arch` | `orchestrator.ArchitectInterface` | 主智能体，工具注册的目标对象 |
| `app` | `*Application` | 应用容器，为工具提供所需的依赖（Bus、Wdb、MemoryService、Provider、RoleReg、PlanEngine、AgentPool） |

**返回值**: 无

**功能描述**:

向主智能体注册所有内置工具及特殊工具。工具统一由 `builtin.BuiltinRegistry`（定义于 `tool/builtin/register.go`）管理，通过 `init()` 函数将 29 个工具注册到全局 `map[string]ToolFactoryFunc`。`registerBuiltinTools()` 遍历 `BuiltinRegistry`，跳过主智能体禁用的工具（如 `bash`），逐工厂调用 `ToolDeps` 注入依赖并创建工具实例，再通过 `arch.RegisterTool(t)` 注册到主智能体。`task`、`todo` 工具因需访问 `app` 字段而在 `main_register.go` 的 `registerBuiltinTools()` 中内联定义。注册分为三组：

**第一组：基本内置工具**（来自 `builtin.BuiltinRegistry`，通过 `ToolDeps` 依赖注入创建）

| 工具名 | 工厂函数 | 功能 |
|--------|----------|------|
| `read` | `builtin.ReadTool()` | 读取文件内容 |
| `write` | `builtin.WriteTool()` | 写入文件 |
| `edit` | `builtin.EditTool()` | 精确编辑文件 |
| `glob` | `builtin.GlobTool()` | 文件名模式匹配搜索 |
| `grep` | `builtin.GrepTool()` | 文件内容正则搜索 |
| `diff` | `builtin.DiffTool()` | 查看文件差异 |
| `webfetch` | `builtin.WebFetchTool()` | 获取网页内容 |
| `skill` | `builtin.SkillTool()` | 技能管理 |
| `apply_patch` | `builtin.ApplyPatchTool()` | 应用补丁文件 |
| `log_issue` | `builtin.LogIssueTool()` | 记录问题到错误日志 |
| `send_message` | `builtin.SendMessageTool(bus)` | 向 TUI 发送消息 |
| `question` | `builtin.QuestionTool(bus)` | 向用户提问（通过 TUI 选项框） |
| `schedule_create` | `builtin.ScheduleCreateTool(wdb)` | 创建周期任务 |
| `schedule_list` | `builtin.ScheduleListTool(wdb)` | 列出周期任务 |
| `schedule_delete` | `builtin.ScheduleDeleteTool(wdb)` | 删除周期任务 |
| `schedule_toggle` | `builtin.ScheduleToggleTool(wdb)` | 启用/禁用周期任务 |
| `db_query` | `builtin.DBQueryTool(wdb)` | 查询工作区数据库 |
| `db_exec` | `builtin.DBExecTool(wdb)` | 执行数据库写操作 |
| `db_tables` | `builtin.DBTablesTool(wdb)` | 列出数据库表 |
| `go_run` | `builtin.GoRunTool(wdb)` | 运行 Go 代码 |
| `companion_talk` | `builtin.CompanionTalkTool(provider, roleReg, bus)` | 与陪伴角色猫猫交互 |
| `memory_set` | `builtin.MemorySetTool(memSvc)` | 设置记忆 |
| `memory_get` | `builtin.MemoryGetTool(memSvc)` | 获取记忆 |
| `memory_search` | `builtin.MemorySearchTool(memSvc)` | 搜索记忆 |
| `memory_list` | `builtin.MemoryListTool(memSvc)` | 列出记忆 |
| `memory_delete` | `builtin.MemoryDeleteTool(memSvc)` | 删除记忆 |

> **注意**: `bash` 工具在主智能体中被禁用（已注释），命令执行通过子智能体委派实现。

**第二组：Task 工具（内联定义）**

- **工具名**: `task`
- **功能**: 委派任务给子智能体
- **参数**:
  - `subagent_type`（string，必填）：子智能体类型，可选值: `"explore"`、`"plan"`、`"general"`、`"reviewer"`、`"verifier"`、`"guard"`、`"lean4"`
  - `description`（string，必填）：任务描述
- **实现**: 调用 `app.AgentPool.ExecuteAsync(ctx.Ctx, ...)` 异步启动子智能体执行，传入请求上下文以支持取消传播

**第三组：Todo/Plan 工具（内联定义）**

- **工具名**: `todo`
- **功能**: 管理任务列表（创建/更新/查看规划中的任务进度）
- **参数**:
  - `action`（string，必填）：操作类型，可选值: `"create"`、`"update"`、`"list"`
  - `description`（string）：规划描述（`create` 时必填）
  - `todos`（string）：JSON 格式的任务列表（`create` 时必填）

- **工具名**: `plan_enter` / `plan_exit`
- **功能**: 进入/退出规划模式
- **注册条件**: 仅在 `app.PlanEngine != nil` 时注册

---

### `runTUI()`

```
func runTUI(arch orchestrator.ArchitectInterface, cfg *config.Config, app *Application)
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `arch` | `orchestrator.ArchitectInterface` | 主智能体，处理用户输入 |
| `cfg` | `*config.Config` | 应用配置（当前保留用于后续扩展） |
| `app` | `*Application` | 应用容器，提供所有模块引用 |

**返回值**: 无

**功能描述**:

启动 TUI（Terminal User Interface）模式。TUI 基于 Bubble Tea 框架构建，提供完整的终端图形界面。

**初始化步骤**：

1. **侧边栏宽度** — 从数据库读取 `tui.sidebar_width` 设置，默认 28 列
2. **创建 TUI Model** — 调用 `tui.New()` 创建数据模型，绑定用户输入处理回调
3. **@mention 自动补全** — 从数据库获取所有启用的子智能体定义，传递给 TUI 供 `@` 命令自动补全
4. **空闲任务 Tick** — 设置定时回调，每次 Tick 时检查调度器并执行到期任务，输出到 TUI 日志
5. **周期任务列表** — 从数据库加载周期任务信息，发送到侧边栏显示
6. **会话信息** — 发送工作区路径、插件数、MCP 服务数等会话信息到侧边栏
7. **历史会话列表** — 从数据库加载所有历史会话并更新侧边栏
8. **启动 Bubble Tea Program** — 配置 Alt Screen、TTY 输入、鼠标支持后调用 `Run()`

**用户输入处理机制**：

回调函数处理两种输入：

- **@ 命令**（如 `@explore 搜索代码`）：
  1. 标记用户活动（`Scheduler.Detector().Touch()`）
  2. 解析 `@agent_type task` 格式
  3. 异步调用 `AgentPool.Execute()`，流式输出到 TUI
  4. 执行完成后将结果注入主会话（system 消息）
  5. 异步保存会话到数据库

- **普通输入**（自然语言对话）：
  1. 标记用户活动
  2. 调用 `arch.ProcessInput()` 流式处理
  3. 输出完成后更新 TUI 消息计数
  4. 异步保存会话到数据库
  5. 更新规划状态和子智能体状态到侧边栏

**侧边栏宽度变更回调**：当用户拖动侧边栏宽度时，自动将新宽度保存到 DB（key: `tui.sidebar_width`）。

---

### `runREPL()`

```
func runREPL(arch orchestrator.ArchitectInterface, app *Application)
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `arch` | `orchestrator.ArchitectInterface` | 主智能体，处理用户输入 |
| `app` | `*Application` | 应用容器，用于 `/status`、`/roles` 等命令 |

**返回值**: 无

**功能描述**:

启动 REPL（Read-Eval-Print Loop）交互模式。通过命令行进行纯文本对话，不使用图形界面。

**架构**：
- 创建 3 个 channel 实现异步输入处理：
  - `quitCh` — 接收系统信号（SIGINT、SIGTERM）
  - `inputCh` — 接收用户输入（通过 goroutine 中的 `bufio.Scanner` 持续读取 stdin）
  - context cancel — 中断正在进行的 AI 回复

**循环逻辑**：
1. 打印 `> ` 提示符
2. `select` 等待信号或输入：
   - 收到退出信号 → 取消 context 并返回
   - 收到用户输入 → 去除首尾空白
3. 首先检查命令（`handleCommand()`），支持的命令见下方
4. 若非命令，调用 `arch.ProcessInput(ctx, input)` 流式输出 AI 回复
5. 在流式输出期间也可被 `SIGINT` 中断

**支持的命令**（通过 `handleCommand()` 处理）：

| 命令 | 功能 |
|------|------|
| `/quit`, `/exit` | 退出程序 |
| `/help` | 显示帮助信息（调用 `printHelp()`） |
| `/roles` | 列出已加载的角色（调用 `listRolesHandler()`） |
| `/status` | 显示当前状态：消息数、工具数、子智能体池状态、规划进度、事件总线订阅者（调用 `printStatus()`） |
| `/clear` | 清空对话历史和规划引擎 |
| `/save` | 手动保存当前会话到工作区数据库 |

---

### `cleanup()`

```
func cleanup(arch orchestrator.ArchitectInterface, app *Application)
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `arch` | `orchestrator.ArchitectInterface` | 主智能体，需要保存其会话数据 |
| `app` | `*Application` | 应用容器，需要关闭其管理的资源 |

**返回值**: 无

**功能描述**:

退出前执行资源清理，保证数据完整性和资源释放。按以下顺序执行：

1. **保存会话** — 若 `app.Wdb != nil`：
   - 将当前会话（`conv` 和 `msgs`）写入工作区数据库
   - 保存侧边栏宽度（`tui.sidebar_width`）到 DB
   - 关闭工作区数据库连接（`app.Wdb.Close()`）

> **关联方法**：`storage.DeleteConversation(id)` 用于删除会话时，已实现级联删除——先通过 `DELETE FROM messages WHERE conversation_id = ?` 删除关联消息，再删除 conversation 记录，保证数据完整性。
2. **关闭规划引擎** — 若 `app.PlanEngine != nil`，调用 `app.PlanEngine.Close()`
3. **关闭子智能体池** — 若 `app.AgentPool != nil`，调用 `app.AgentPool.Shutdown()`

---

### `handleCommand()`

```
func handleCommand(input string, arch orchestrator.ArchitectInterface, app *Application) bool
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `input` | `string` | 用户输入的文本 |
| `arch` | `orchestrator.ArchitectInterface` | 主智能体（用于 `/clear` 和 `/save` 操作） |
| `app` | `*Application` | 应用容器（用于 `/status`、`/roles`） |

**返回值**: `bool` — `true` 表示输入被识别为命令并已处理，调用方应跳过 AI 处理；`false` 表示不是命令，应正常调用 AI

**功能描述**:

REPL 模式下的命令路由分发器。根据输入匹配以下命令：

- `/quit`、`/exit` → 返回 `true`（调用方退出循环）
- `/help` → 调用 `printHelp()`
- `/roles` → 调用 `listRolesHandler(app)`
- `/status` → 调用 `printStatus(arch, app)`
- `/clear` → 清空会话历史，重置规划引擎
- `/save` → 手动保存会话到数据库
- 其他输入 → 返回 `false`

---

### `listRolesHandler()`

```
func listRolesHandler(app *Application)
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `app` | `*Application` | 应用容器，用于获取角色注册表 |

**返回值**: 无（直接打印到 stdout）

**功能描述**:

列出所有已加载的角色信息，包括：
- 角色图标（🔧 普通角色，💝 陪伴角色）
- 显示名称（`DisplayName`）
- 模式标签（⭐ 主智能体 / 📎 子智能体 / 💤 后台）
- 绑定的工具列表

同时通过事件总线发布 `role.list` 事件，触发 TUI 中的角色列表更新。

---

### `printStatus()`

```
func printStatus(arch orchestrator.ArchitectInterface, app *Application)
```

**参数**:

| 参数 | 类型 | 说明 |
|------|------|------|
| `arch` | `orchestrator.ArchitectInterface` | 主智能体，获取会话信息 |
| `app` | `*Application` | 应用容器，获取池、规划、事件总线状态 |

**返回值**: 无（直接打印到 stdout）

**功能描述**:

打印当前运行时状态摘要：
- 会话：消息数和 token 预估数
- 工具：已注册工具数量
- 子智能体池：活跃数 / 总实例数
- 规划引擎：活跃规划的描述和完成进度百分比（若无活跃规划则显示"无活跃规划"）
- 事件总线：订阅者总数

---

### `printBanner()`

```
func printBanner()
```

**参数**: 无

**返回值**: 无

**功能描述**: 打印 catcode 启动横幅，包含程序名称、版本号和简介。

---

### `printCLIHelp()`

```
func printCLIHelp()
```

**参数**: 无

**返回值**: 无

**功能描述**: 打印命令行帮助信息，包含：
- 所有命令行选项（`-r`/`--repl`、`-m`/`--model`、`-t`/`--temperature`、`-w`/`--workspace`、`-h`/`--help`）
- 配置优先级说明（`go:embed 默认值 → DB settings → 环境变量 → CLI 参数`）
- 环境变量列表（`CATCODE_MODEL`、`CATCODE_WORKSPACE`、`DEEPSEEK_API_KEY`、`OPENAI_API_KEY`、`CATCODE_BASE_URL`、`CATCODE_THEME`）

---

### `printHelp()`

```
func printHelp()
```

**参数**: 无

**返回值**: 无

**功能描述**: 打印 REPL 模式下运行时帮助信息，列出所有 `/` 前缀命令（`/help`、`/quit`、`/roles`、`/status`、`/clear`、`/save`）及可用工具列表。

---

## 事件注册说明

在 `main()` 中注册了以下事件回调（全部订阅到 `app.Bus`）：

| 订阅者 ID | 事件类型 | 功能 |
|-----------|----------|------|
| `role.list` | `role.list`（自定义） | 响应角色列表查询，触发 `listRolesHandler()` |
| `tui.agents` | `EventAgentStatusChanged` | 子智能体状态变更时更新 TUI 侧边栏的智能体列表 |
| `tui.plan` | `EventPlanCreated` | 规划创建时更新 TUI 待办事项列表 |
| `tui.plan` | `EventPlanStepStart` | 规划步骤开始时更新 TUI 待办事项列表 |
| `tui.plan` | `EventPlanStepDone` | 规划步骤完成时更新 TUI 待办事项列表 |
| `tui.plan` | `EventPlanCompleted` | 规划完成时更新 TUI 待办事项列表 |
| `companion` | `EventCompanionRespond` | 猫猫陪伴角色状态更新时，同步心情、亲密度、兴奋度、害羞度、疲劳度到 TUI 侧边栏 |
| `dialog` | `EventDialogSend` | `send_message` 工具发送对话框消息时，转发到 TUI |
| `question` | `EventQuestionAsked` | `question` 工具向用户提问时，解析问题 JSON 并发送到 TUI 显示选项框，通过 `replyCh` 回传答案 |

---

## 命令行参数

| 参数 | 短选项 | 功能 |
|------|--------|------|
| `--repl` | `-r` | 启动 REPL 命令行交互模式（默认启动 TUI） |
| `--model` | `-m` | 指定使用的 AI 模型名称 |
| `--temperature` | `-t` | 设置 AI 模型温度参数（浮点数） |
| `--workspace` | `-w` | 指定工作区根目录（覆盖自动发现） |
| `--help` | `-h` | 显示命令行帮助并退出 |

**配置优先级**（从低到高）：`go:embed 默认值 → 数据库 settings 表 → 环境变量 (CATCODE_*) → CLI 参数`

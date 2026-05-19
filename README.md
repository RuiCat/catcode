# 🐱 catcode — TUI AI 辅助编程工具

> 纯 Go、工作区隔离、主/子智能体协作的 TUI AI 辅助编程工具  
> 版本: v0.9.3 | [架构设计文档](ARCHITECTURE.md)

---

## 📋 目录

- [架构总览](#架构总览)
- [核心模块](#核心模块)
- [工作区与数据存储](#工作区与数据存储)
- [启动与会话恢复](#启动与会话恢复)
- [主/子智能体系统](#主子智能体系统)
- [角色系统](#角色系统)
- [工具系统](#工具系统)
- [上下文压缩](#上下文压缩)
- [记忆系统](#记忆系统)
- [权限与守卫](#权限与守卫)
- [错误处理与日志](#错误处理与日志)
- [双向通信](#双向通信)
- [开发指南](#开发指南)
- [更新日志](#更新日志)

---

## 架构总览

```
catcode/
├── cmd/catcode/main.go              # 入口（启动编排 + 会话恢复）
├── agent/                           # 智能体系统
│   ├── orchestrator/architect.go    #   主智能体编排（递归工具执行+压缩+纠错）
│   ├── pool/pool.go                 #   子智能体池（会话持久化+压缩+GuardReviewer）
│   ├── role/                        #   角色系统（定义/注册表/3层合并/热重载）
│   ├── subagent/                    #   子智能体接口+BaseAgent（interface/config/base）
│   │   └── hook/                    #     Hook 系统（规划中，目录预留）
│   └── plan/engine.go               #   规划引擎 + 工作流FSM
├── ai/                              # AI 层
│   ├── llm/                         #   LLM 提供商（OpenAI兼容+流式+重试+堆栈错误）
│   ├── session/session.go           #   会话管理（消息预编码+序列化/反序列化+清理）
│   └── compact/compact.go           #   上下文压缩（60%触发+双级压缩+记忆选择器）
├── core/                            # 基础设施
│   ├── errors/                      #   统一错误处理（堆栈跟踪+类别+自纠正+收集器）
│   ├── event/event.go               #   事件总线（发布-订阅+TriggerManager+深度限制）
│   └── config/                      #   配置系统（embed→DB→env→CLI多层优先级）
├── data/                            # 数据持久化
│   ├── storage/                     #   SQLite (9张表 + 版本迁移 v6)
│   │   ├── schema.go                #     DDL + v1→v6 迁移逻辑
│   │   ├── conversations.go         #     会话+消息持久化（reasoning_content+enabled）
│   │   ├── memory.go                #     长期记忆（多级索引+智能选择器）
│   │   ├── error_logs.go            #     错误日志（类别/堆栈/来源）
│   │   └── ...
│   └── embed/                       #   go:embed 嵌入（8角色YAML+默认配置）
├── tool/                            # 工具系统（30个内置工具+工厂）
├── ui/tui/                          # TUI 界面 (component/manager/plugin 三层)
│   ├── component/                  #   组件层 (Chat/Sidebar/Status/Input/Question)
│   ├── manager/                    #   控制层 (UIManager + 鼠标事件路由)
│   └── plugin/                     #   插件 API 层 (UIAPI 接口)
├── schedule/ plugin/ mcp/           # 周期任务 / 插件 / MCP
├── vendor/                          # go mod vendor 依赖锁（含 lipgloss 等）
```

### 启动流程

```
1. 工作区发现 → 2. SQLite 打开/迁移(schema v6) → 3. 配置加载
→ 4. 角色加载(DB+YAML+热重载) → 5. ProviderRegistry 初始化
→ 6. 子智能体池创建 → 7. 注册内置工具(30个) → 8. 恢复历史会话
→ 9. 记忆索引注入 → 10. 插件/MCP 加载 → 11. 启动 TUI/REPL
```

### 核心特性

| 特性 | 说明 |
|------|------|
| SubAgent 接口化 | `agent/subagent/` 定义 `SubAgent` + `ContextBuilder` 接口，`BaseAgent` 共享实现，7 种 Agent 类型 |
| Application 依赖注入 | `Application` 结构体集中管理 13 个模块引用，消除包级全局变量，便于测试和扩展 |
| yaegi Hook 系统 | `agent/subagent/hook/` 引擎，沙箱安全，热重载。用户在 `.catcode/hooks/<agent_type>.go` 编写自定义上下文注入 |
| TUI 组件化 | 6 个 Component（Chat/Sidebar/Status/Input/Question）+ UIManager 中央控制 + 全局鼠标事件路由 |
| 上下文命中修复 | 5 层上下文注入确保子智能体 contextSummary 不再为空；`AGENTS.md`/`CLAUDE.md` 自动发现 |
| 空闲超时机制 | HTTP SSE 180s / 子智能体等待 10min / Guard 审查 60s — 收到数据重置计时器 |
| 思考过程显示 | 推理内容流式展示（🧠 折叠卡片），Alt+T 或 `/thinking` 切换 |
| 硬编码提示词清除 | 所有 SystemPrompt 从 `embed/prompts/` YAML 读取，`embed.GetPrompt()` / `embed.GetAgentTools()` API |
| Guard 审查缓存 | 相同命令缓存审查结果，避免重复 LLM 调用；审查后重置 guard 会话 |
| lipgloss 本地化 | `go mod vendor` 将所有依赖锁入 `vendor/` 目录 |

---

## 核心模块

### 数据持久化层 (9张表，schema v6)

| 表 | 用途 |
|----|------|
| `schema_version` | 版本管理（v1→v6迁移链） |
| `settings` | 键值对配置 |
| `agent_definitions` | 智能体定义 |
| `conversations` | 会话元数据（含summary） |
| `messages` | 对话消息（含reasoning_content/enabled） |
| `memory` | 长期记忆（多级索引：scope+type+importance） |
| `context_snapshots` | 压缩快照（含完整消息JSON+摘要） |
| `scheduled_tasks` | 周期任务 |
| `error_logs` | 错误日志（类别/严重程度/堆栈/来源/会话ID） |

---

## 启动与会话恢复

### 启动恢复

启动时自动从 DB 加载上次会话：
1. `LoadConversation("architect-main")` 加载主会话
2. `LoadHistory(msgs)` 恢复消息历史（含 tool_calls、reasoning_content、enable 状态）
3. `SetSummary(summary)` 恢复压缩摘要
4. `InjectMemoryIndex()` 注入记忆索引到 SystemPrompt

### 自动保存

- **TUI 模式**：每次 LLM 响应完成后异步保存
- **退出**：cleanup() 最终保存
- **子智能体**：每次执行完成后异步保存（固定ID `subagent-{type}`）

### 完整恢复覆盖

| 数据 | 修复前 | 修复后 |
|------|:--:|:--:|
| 消息内容 (content) | ✅ | ✅ |
| 工具调用 (tool_calls) | ✅ | ✅ |
| 推理内容 (reasoning_content) | ❌ | ✅ |
| 消息启用状态 (enabled) | ❌ | ✅ |
| 压缩摘要 (summary) | ❌ | ✅ |
| 记忆索引 (memory index) | ❌ | ✅ |

---

## 主/子智能体系统

### 主智能体 (Architect)

- **纯规划与编排**：不直接使用 edit/write/bash 工具
- **递归工具执行**：最多20轮工具调用，3次自纠正
- **上下文压缩**：60% token 占用触发，micro（清理旧工具输出）+ full（注入压缩提示词）
- **子智能体委派**：通过 `task` 工具同步委派，空闲超时 10 分钟（收到输出行重新计时）
- **panic 保护**：goroutine recover + 错误日志写入

### 子智能体 (SubAgent)

#### 接口化设计

子智能体通过 `agent/subagent/` 包标准化接口：

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

`BaseAgent` 实现所有共享逻辑（Session/工具注册/压缩/Guard审查），7 种子智能体类型（explore/plan/general/reviewer/verifier/guard/lean4）共享同一实现，通过 `Config` 中的 `Type`/`SystemPrompt`/`Tools` 字段区分行为。配置差异由 `AgentConfigsFromDB()` / `DefaultAgentConfigs()` 管理。`agent/pool/pool.go` 向后兼容，内部通过 `subagent.New()` 创建实例。

#### 5 层上下文注入

Execute() 在 LLM 调用前通过 `ContextBuilder` 注入 5 层上下文，确保 contextSummary 不再为空：

```
[0] system  SystemPrompt (embed YAML 角色定义)
[1] system  MemoryIndex (数据库记忆索引)
[2] system  ContextSummary (主智能体传入的上下文摘要)
[3] system  指令文件 (AGENTS.md / CLAUDE.md / .catcode/instructions.md)
[4] user    任务描述 (task)
```

#### 执行特性

- **独立会话**：每个子智能体拥有独立的 LLM 会话和工具集
- **会话持久化**：固定 ID 持久化，启动时自动恢复历史
- **工具执行循环**：支持多轮工具调用（read→write→bash→verify），最多10轮
- **上下文压缩**：与主智能体相同的60%触发机制
- **GuardReviewer**：bash 命令执行前自动调用 guard 子智能体审查（相同命令缓存结果，审查后重置 guard 会话避免消息累积）
- **空闲超时**：主智能体等待 10 分钟空闲超时（收到输出重置），guard 审查 LLM 调用 60s 超时
- **panic 保护**：recover + 错误日志

### 子智能体配置

| 子智能体 | 工具集 | 模型 | 用途 |
|---------|--------|------|------|
| explore | read, glob, grep, bash | deepseek-v4-flash | 代码搜索探索 |
| plan | read, glob, grep | deepseek-v4-pro | 架构设计规划 |
| general | read, write, edit, glob, grep, bash | deepseek-v4-pro | 通用任务执行 |
| reviewer | read, glob, grep, bash | deepseek-v4-pro | 代码安全审查 |
| verifier | read, glob, grep, bash, diff | deepseek-v4-pro | 对抗性验证 |
| guard | bash, read | deepseek-chat | 命令安全审查 |
| lean4 | read, write, edit, glob, grep, bash | deepseek-v4-pro | Lean4 数学证明与形式化验证 |

---

## 角色系统

### 加载优先级（3层）

```
用户文件 (.catcode/roles/*.yaml)  >  DB user 定义  >  DB builtin 默认
    (最高优先级，热重载)               (运行时修改)      (go:embed 种子)
```

### 内置角色 (8个)

| 角色 | 模式 | 说明 |
|------|------|------|
| architect | primary | 主编排器，纯规划不执行代码修改 |
| explore | subagent | 代码探索，只读模式 |
| plan | subagent | 架构规划，只读模式 |
| general | subagent | 通用执行，多步操作 |
| reviewer | subagent | 安全/正确性/性能/规范审查 |
| verifier | subagent | 对抗性验证（PASS/FAIL/PARTIAL） |
| guard | subagent | 命令安全审查，三层防御 |
| lean4 | subagent | Lean4 数学证明与形式化验证 |
| companion | background | 猫猫陪伴，线2监督 |

### 提示词嵌入系统

所有角色的 SystemPrompt、压缩模板、Guard 审查提示词均从 `data/embed/` 的 go:embed 文件读取，**无硬编码字符串**：

| 资源 | 嵌入路径 | API |
|------|---------|-----|
| 角色定义 (8个) | `embed/roles/*.yaml` | `embed.GetAgentPrompt(name)` |
| 角色工具列表 | `embed/roles/*.yaml` | `embed.GetAgentTools(name)` |
| 压缩模板 | `embed/prompts/compaction.txt` | `embed.GetPrompt("compaction")` |
| Guard 审查 | `embed/prompts/guard_review.txt` | `embed.GetPrompt("guard_review")` |

---

## 工具系统

### 工具清单 (30个)

主智能体通过 `registerBuiltinTools` 注册全部 30 个工具（bash 已注释禁用，命令执行通过子智能体委派）。

| 工具 | 说明 |
|------|------|
| `read` | 读取文件 |
| `write` | 写入文件 |
| `edit` | 精确文本替换 |
| `glob` | 文件模式搜索 |
| `grep` | 正则内容搜索 |
| `diff` | 文件差异比较 |
| `webfetch` | 获取 URL 内容，支持 text/markdown/html 格式 |
| `skill` | 加载专业化技能（.catcode/skills/ 目录） |
| `apply_patch` | 批量应用文件编辑补丁，原子性回滚 |
| `task` | 子智能体委派（explore/plan/general/reviewer/verifier/guard/lean4） |
| `todo` | 任务列表管理 |
| `ask_architect` | 子智能体→主智能体双向通信（子智能体专用） |
| `send_message` | 发送对话框消息 |
| `question` | 选项框提问 |
| `bash` | 命令执行（🔒主智能体禁用，子智能体通过 guard 子智能体审查） |
| `companion_talk` | 猫猫陪伴对话 |
| `memory_set/get/search/list/delete` | 记忆 CRUD（5个工具） |
| `schedule_create/list/delete/toggle` | 周期任务管理（4个工具） |
| `db_query/db_exec/db_tables` | 数据库操作（3个工具） |
| `go_run` | Go 脚本执行 (yaegi) |
| `log_issue` | 问题日志追加 |
| `plan_enter` | 进入计划模式，暂停自动执行 |
| `plan_exit` | 退出计划模式，提交规划结果 |

### 工具注册架构

```
主智能体: registerBuiltinTools(arch) → 注册 26 个 builtin + task + todo + plan_enter/exit（bash 已注释禁用）
子智能体: ToolFactory(name) → 按角色 tools 配置动态创建
MCP 工具: mcp.Manager.ConnectServer() → 自动适配注册
插件工具: plugin.Manager.LoadAll() → yaegi 动态加载
```

---

## 上下文压缩

### 触发条件

- **60% 阈值**：token 占用超过上下文窗口60%时自动触发
- 消息数 < 10 时只执行 micro 压缩

### 压缩级别

| 级别 | 说明 |
|------|------|
| `micro` | 清理旧工具输出（保留最近6条，设置 Enable=false） |
| `full` | 注入结构化压缩提示词（GOAL/PROGRESS/KEY_DECISIONS/NEXT_STEPS/CRITICAL_CONTEXT/RELEVANT_FILES/SUMMARY）+ DB 快照 |

### 压缩快照

- 触发时创建 `context_snapshots` 表记录
- 支持 `GetLatestSnapshot(convID)` 加载最近快照
- `ListSnapshotsFull(convID)` 查看完整快照历史

### 适用范围

- **主智能体** ✅
- **子智能体** ✅ （与主智能体相同的压缩机制）

---

## 记忆系统

### 记忆索引

- `BuildIndex(contextHint)` 实时构建紧凑记忆索引
- 有上下文时使用 `SelectRelevantMemories` 智能选择（相关性评分）
- 无上下文时使用 `ScanMemoryHeaders` 简单扫描
- **索引缓存**：避免频繁 DB 扫描，写入新记忆后自动失效

### 注入时机

| 时机 | 说明 |
|------|------|
| 启动恢复 | `LoadHistory` 后 `InjectMemoryIndex()` |
| 每次用户输入 | `ProcessInput` 入口 |
| 工具循环继续 | `continueConversation` |

### 记忆 CRUD

5 个工具覆盖完整记忆生命周期，支持 `global`/`workspace` 双范围。

---

## 指令文件系统

### 自动发现

`data/storage/instructions.go` 在启动时从工作目录向上搜索 `.git` 确定项目根，自动读取以下指令文件（每个最多 8000 字符）：

| 文件 | 查找位置 |
|------|---------|
| `AGENTS.md` | 项目根目录 |
| `CLAUDE.md` | 项目根目录 |
| `instructions.md` | `.catcode/instructions.md` |

### 上下文注入

指令文件内容通过 `InstructionFiles.FormatContext()` 格式化为 LLM 上下文，注入到：
- **主智能体**：附加到 SystemPrompt 末尾
- **子智能体**：通过 Execute() 的 5 层上下文注入（第4层）

主智能体和子智能体均可获得项目级别的编码规范、技术决策和约束信息，无需在每次对话中重复说明。

---

## 权限与守卫

### 三层 bash 防御

```
bash 命令执行
    │
    ├── 第1层: guardCheck() 正则匹配
    │   rm -rf /, dd, mkfs, fork bomb → 直接拒绝
    │
    ├── 第2层: GuardReviewer (guard 子智能体 LLM 审查)
    │   CRITICAL/HIGH → 拒绝, MEDIUM/LOW/SAFE → 放行
    │   审查结果缓存（相同命令不重复调用 LLM），60s 超时
    │
    └── 第3层: 命令执行
```

### Guard 审查缓存

- 相同命令的审查结果（approved/reason）缓存到内存 map，避免重复 LLM 调用
- 缓存上限 100 条，超出时逐出最旧条目
- 审查完成后自动调用 `ResetSession()` 清空 guard 子智能体会话，防止消息累积

### 主智能体 bash 控制

- 主智能体 **不注册** bash 工具
- 命令执行通过 `task` 委派 general 子智能体
- 子智能体每次 bash 前自动通过 guard 审查
- guard 子智能体自身**跳过审查**（避免循环）

### 权限级别

| 级别 | 行为 |
|------|------|
| `allow` | 直接执行 |
| `ask` | 需用户确认 |
| `deny` | 返回权限拒绝 |

---

## 思考过程显示

### 流式展示

LLM 回复中的推理内容（reasoning_content）以 `🧠` 前缀流式传输到 TUI：

- **显示**：推理内容渲染为折叠卡片（默认展开），以 `🧠 思考过程:` 标题标识
- **折叠**：可折叠为 `🧠 思考过程 (Enter 展开)` 一行提示
- **样式**：使用 muted 颜色和圆角边框与普通回复区分

### 切换开关

| 方式 | 说明 |
|------|------|
| `Alt+T` | 全局切换推理显示开/关 |
| `/thinking` | 命令方式切换 |
| `showThinking` | UIManager 全局状态，所有消息响应 |

关闭后已渲染的思考卡片保持可见，但后续新消息不再显示推理内容。

---

## 错误处理与日志

### 错误系统 (`core/errors`)

| 组件 | 说明 |
|------|------|
| `CatError` | 统一错误类型（消息+堆栈+类别+原始错误链） |
| `ErrorCollector` | 延迟错误收集器（避免破坏 tool_calls 配对） |
| `SelfCorrect` | 自纠正计数器（默认3次上限） |
| `IsRetryable()` | 可重试判断（错误链解包+类别匹配） |
| `Wrap/Wrapf` | 包装错误并捕获调用堆栈 |

### 错误持久化 (`error_logs` 表)

| 字段 | 说明 |
|------|------|
| `category` | API / 工具 / 权限 / LLM / 网络 / 内部 |
| `severity` | error / warning / info |
| `message` | 错误消息 |
| `stack_trace` | 完整调用堆栈 |
| `source` | architect / subagent / llm / startup |
| `conversation_id` | 关联会话ID |

### 日志记录覆盖

| 来源 | 触发点 |
|------|--------|
| architect | `handleError`, `collectToolError`, LLM API 失败, panic recover |
| subagent | `collectToolError`, LLM API 失败, panic recover |
| startup | MCP 连接失败等关键错误 |
| pool | goroutine panic recover |

### 堆栈跟踪格式

```
%v / Error():  "llm: API 错误 400: missing field 'content'"
%+v:          完整错误链 + 调用堆栈
              堆栈跟踪:
                at (*OpenAIClient).Chat (llm/llm.go:214)
                at (*Architect).continueConversation (orchestrator/architect.go:411)
```

### Goroutine Panic 保护

所有关键 goroutine 均有 recover：
- `ProcessInput` goroutine ✅
- `Pool.Execute`/`ExecuteAsync` goroutine ✅
- `SubAgent.Execute` goroutine ✅

---

## 双向通信

### ask_architect 工具

子智能体在任务执行中遇到不确定性时可主动向主智能体提问：

```
子智能体: "修改配置文件，应该用 JSON 还是 YAML？"
  → ask_architect("配置格式选择")

主智能体: (同步 LLM 调用，可 read 项目代码后) "保持 YAML 格式"

子智能体: "收到" → 继续执行
```

**实现**：子智能体通过 `askArchitectFn` 回调同步等待主智能体应答，主智能体通过 `Session.BuildCleanRequest()` 构建独立请求调用 LLM 获取回答。

---

## 四层通信架构

```
智能体层 (agent/)        上下文层 (ai/session/)    通信层 (ai/llm/)      远程服务
─────────────────       ──────────────────      ──────────────       ────────
Architect/SubAgent  →   Session.BuildRequest  →  Provider.Chat()  →  DeepSeek
       ↑                    ↑                       ↑                   API
       │                    │                       │
  业务逻辑             消息/工具/压缩           OpenAI兼容+SSE
  ask_architect        BuildCleanRequest        ProviderRegistry
  (双向通信)           (独立请求)               (多Provider)
```

| 层 | 职责 | 实现 |
| 智能体层 | 对话编排、工具执行、压缩调度 | `agent/orchestrator`, `agent/pool`, `agent/subagent` |
| 上下文层 | 消息管理、请求构建、序列化 | `ai/session` |
| 通信层 | LLM API 调用、SSE 解析、重试 | `ai/llm` (Provider接口) |
| 远程服务 | DeepSeek/OpenAI API | 外部 |

---

## 插件系统

### 符号表注册（零依赖部署）

插件通过 yaegi 解释器加载 `.catcode/plugins/*.go` 文件。catcode 内部包符号通过 `buildCatcodeSymbols()` 预注册到 yaegi，**无需 GOPATH 或源码树**。

| 注册包 | 符号数 | 插件可访问的类型/函数 |
|--------|:--:|------|
| `catcode/tool` | 13 | Tool, FuncDef, Schema, Property, Context, PermissionLevel, MustMarshalSchema, 权限系统 |
| `catcode/core/event` | 43 | 31 个事件常量, EventBus, Event, Trigger, TriggerManager, NewBus, NewTriggerManager |
| `catcode/agent/role` | 15 | RoleDef, ModelConfig, ThinkingConfig, ModelLimit, TriggerDef, StateDef, ParseYAML |
| `catcode/core/errors` | 19 | CatError, ErrorCollector, SelfCorrect, Wrap/Wrapf, 9 个类别常量 |
| `catcode/ai/llm` | 27 | ChatRequest, Message, ToolCall, StreamEvent, Provider 接口, 5 个流常量 |
| `catcode/ai/session` | 4 | Session, Message, New, FromConversationRow |
| `catcode/ai/compact` | 5 | CompactDecision, ShouldCompact, BuildCompactionPrompt, 常量 |
| `catcode/data/storage` | 10 | ConversationRow, MessageRow, MemoryService, MemoryEntry（不含 WorkspaceDB） |

### 插件接口

```go
type Plugin interface { Name() string; Version() string }
type ToolPlugin interface { Plugin; Tools(bus) []*tool.Tool }
type RolePlugin interface { Plugin; RoleDef() role.RoleDef }
```

### 配置

在 `.catcode/plugins/` 放置 `.go` 文件，定义 `var Plugin` 变量，启动时自动加载。

---

## 开发指南

### 编译

```bash
go build -o catcode ./cmd/catcode
```

### 运行

```bash
./catcode                     # 当前目录为工作区
./catcode --workspace /path   # 指定工作区
./catcode --repl              # REPL 模式
export DEEPSEEK_API_KEY="sk-..."  # 设置 API Key
```

### 数据库 Schema 升级

当前版本: **v6**。升级流程：

1. 修改 `schemaV1` DDL 添加新表/列
2. 更新 `currentSchemaVersion`
3. 在 `migrateDB` 中添加 `if version < N` 迁移逻辑

### 添加新工具

1. 在 `tool/builtin/` 创建 `xxx_tool.go`
2. 在 `register.go` 的 `RegisterAll()`/`Names()`/`ToolFactory()` 中注册
3. 如需主智能体可用，在 `main.go` 的 `registerBuiltinTools()` 中注册
4. 如需子智能体可用，在对应 YAML 的 `tools:` 列表中添加

### 添加新角色

1. 在 `data/embed/roles/` 创建 `name.yaml`
2. 或放在 `.catcode/roles/` 中（热重载）

---

## 更新日志

### v0.9.1
- 🔌 **插件符号表**：注册 9 个 catcode 内部包到 yaegi（136 个符号），移除 GOPATH/symlink 依赖
- 🏗️ **四层通信分离**：智能体-上下文-通信-远程四层架构，`BuildCleanRequest` 解耦独立请求
- 🗜️ **压缩系统重构**：借鉴 claude-code buildPostCompactMessages + SystemCompactBoundaryMessage
  - CompactResult + ApplyCompactResult 压缩后消息重建
  - SelectCompactRange Head+Tail 分割（保留2轮+token预算）
  - 9段结构化压缩 prompt + 增量更新 (previousSummary)
  - 压缩边界标记 (压缩时间/token数/tail索引)
- 🧱 **上下文分层结构**：系统提示词 → 记忆索引 → 上下文索引 → 对话上下文
- 📋 **精简实现补全**：EnableHotReload、yaegi wdb 注入、DBTask.Run/Condition、Ask 权限
- 🎨 **选项框侧边栏修复**：question mode 中 sidebarVP 不再接收滚动键；进入/退出时 refreshSidebar
- 📊 **ToolFactory 扩展**：子智能体支持 13 种新工具 (DB/Memory/Schedule/Companion)
- 🛡️ **goroutine 全面保护**：7 处关键 goroutine 添加 recover + logError + 堆栈
- 🔧 **错误系统统一**：全项目 158 处 fmt.Errorf → cerr.Wrap，所有错误带堆栈跟踪
- 🔧 **符号表路径验证**：yaegi `interp.Exports` key 格式确认为 `"importPath/packageName"`
- 🐛 **companion 状态持久化**：fire-and-forget 改为闭包形式

### v0.9.0
- 🔄 会话持久化完整修复：reasoning_content/enabled 字段完整保存恢复
- 🗜️ 压缩阈值 85%→60%；子智能体全功能（工具循环+持久化+压缩）
- 🛡️ bash 三层防御 + GuardReviewer（LLM审查）
- 💬 主↔子双向通信（ask_architect）
- 📊 错误日志系统（error_logs 表 + CatError 堆栈跟踪）
- 🛡️ Panic 保护、推理显示修复、DB工具修复、子智能体工具集修复
- 🧠 记忆系统增强、权限修复

### v0.8.2
- 🧩 双线工作模式 + 提示词系统升级
- 🔧 question工具修复、TUI修复
- 📝 log_issue 工具

### v0.8.1
- 🔧 buffer悬空指针、parseSSE资源泄漏、QuestionTool超时等修复

### v0.8.0
- 🗄️ db_query/db_exec/db_tables/go_run 工具
- 🛡️ guardCheck正则化

### v0.7.0
- 🔄 自我纠正机制 + ⏰周期任务 + 🧠thinking模式

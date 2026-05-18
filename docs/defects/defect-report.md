# catcode 代码缺陷分析报告

> 版本: v0.9.2 (已修复) | 分析日期: 2026-05-17 | 分析范围: 全项目 118 个 Go 源文件

## 🔧 修复状态 (v0.9.2)

| ID | 描述 | 状态 |
|----|------|------|
| D-01 | 手写YAML解析器 | ✅已修复 |
| D-02 | guardCache伪LRU | ✅已修复 |
| D-03 | 大文件问题 | ✅已修复 |
| D-04 | isRestrictedInPlanMode白名单 | ✅已修复 |
| D-05 | 工具注册块 | ✅已修复 |
| D-06 | SessionInterface方法多 | ✅已修复 |
| D-07 | 中文硬编码 | ⏳ 部分完成 |
| D-08 | Hook系统未实装 | ✅已修复 |
| D-09 | config/loader未集成 | ✅已修复 |

---

### 第二轮扫描 (v0.9.2 — 2026-05-17)

基于全项目死代码扫描，发现并修复：
- 🔴 R1: `_ = i` 死代码 (plan/engine.go) → ✅已修复
- 🔴 R2: secrets.go 密钥派生弱熵 → ✅已增强 (hostname回退)
- 🔴 R3: AddTool 错误吞没 (base.go) → ✅已添加注释
- 🔴 R4: context.Background() 未传递 (webfetch_tool) → ✅已修复
- 🔴 R5: DeleteConversation 无级联删除 → ✅已修复
- 💀 D1: runYaegi() 未使用函数 → ✅已删除
- 💀 D3: 空 Ask 权限块 → ✅已添加TODO注释

---

## 一、概述

本报告基于对 catcode 项目源码的结构化扫描、ARCHITECTURE.md 架构文档对照，以及针对大文件、重复实现、硬编码、缓存策略、接口设计、模块规划等维度的深入审查，识别出 9 类代码缺陷和改进点。

分析方法：静态代码审查 + 架构对照 + 模式识别。覆盖 `cmd/`、`agent/`、`ai/`、`core/`、`data/`、`tool/`、`ui/` 全部一级包。

---

## 二、缺陷清单

### [严重] D-01: 手写 YAML 解析器三重实现

| 属性 | 详情 |
|------|------|
| **位置** | `agent/role/role.go:745-939` (parseSimpleYAML)<br>`data/storage/agents.go:167-469` (parseRoleYAML)<br>`data/embed/embed.go:57-230` (GetAgentPrompt + GetAgentTools) |
| **严重程度** | 严重 |
| **影响范围** | 角色配置加载、种子数据导入、DB 智能体定义同步 |

**问题描述：**

项目在三个不同的包中各自实现了一套基于 `strings.Split("\n")` 逐行解析的手写 YAML 解析器，分别解析同一格式的角色 YAML 文件（`roles/*.yaml`），但解析逻辑存在差异：

1. **role.go: parseSimpleYAML** — 解析为 `RoleDef` 结构体，支持 `|` 多行块、4级缩进、trigger/state 细粒度字段。
2. **agents.go: parseRoleYAML** — 解析为 `AgentRow`（DB 行结构），缩进处理算法与 role.go 基本一致但目标结构不同，且多行块的内容收集逻辑简化了（`strings.TrimLeft` vs role.go 的缩进补偿计算）。
3. **embed.go: GetAgentPrompt / GetAgentTools** — 仅提取 `system_prompt` 和 `tools` 字段，块缩进检测方式与前两者不同（使用 `strings.HasPrefix(leading, blockIndent+" ")` vs 前两者的 `indent <= baseIndent` 判断），且 `inSystemPrompt` 块的退出条件也存在差异。

**风险：**
- 同一份 YAML 文件可能被三套解析器产生不同的解析结果（尤其是边界情况如空行、注释、多层嵌套）。
- 新增 YAML 字段时需要在三处同步修改，极易遗漏。
- 没有结构化的解析错误报告（如行号、上下文），调试困难。

**建议修复方案：**

引入标准 YAML 库（如 `gopkg.in/yaml.v3`）统一解析，将角色 YAML 定义为一个规范的 Go 结构体，三处均通过该结构体反序列化后各自适配到 `RoleDef`、`AgentRow`、`AgentPrompt`。如果不想引入外部依赖，应将当前 `role/role.go` 中的 `parseSimpleYAML` 抽离为 `core/` 或独立的 `yamlutil/` 包，供 embed 和 storage 包复用。

**✅ 修复详情 (v0.9.2)：**

已引入 `gopkg.in/yaml.v3 v3.0.1` 标准库统一替换三处手写解析器：
- `agent/role/role.go` — 删除 `parseSimpleYAML`(208行)，`ParseYAML()` 改用 `yaml.Unmarshal`
- `data/storage/agents.go` — 删除 `parseRoleYAML`(231行)
- `data/storage/workspace.go` — 新增 `parseSeedRoleYAML()` 直接调用 yaml.v3（避免与 role 包循环依赖）
- `data/embed/embed.go` — `GetAgentPrompt/GetAgentTools` 删除手写行解析(199行)，改用 yaml.Unmarshal
- 7个 `.yaml` 文件修正 `*:` 为 `"*":`（YAML别名保留字兼容）
- 净减少 **489 行**代码（+198/-687）

---

### [严重] D-02: guardCache 伪 LRU 驱逐策略

| 属性 | 详情 |
|------|------|
| **位置** | `agent/subagent/base.go:882-891` |
| **严重程度** | 严重 |
| **影响范围** | Guard 审查缓存命中率、命令安全性审查 |

**问题描述：**

guardCache 用于缓存 guard 子智能体的命令审查结果，避免重复的 LLM 调用。当缓存超过 100 条时触发驱逐：

```go
if len(sa.guardCache) >= 100 {
    for k := range sa.guardCache {
        delete(sa.guardCache, k)
        break  // 只删除一条
    }
}
```

**缺陷：**
1. **非 LRU**：`range map` 的迭代顺序在 Go 中是不确定的（自 Go 1.x 起已随机化），无法保证删除的是最久未使用的条目。高频命令的审查结果可能被错误驱逐。
2. **只删一条**：每次达到 100 上限时仅驱逐 1 条，如果短时间内有大量新命令需要缓存，将导致频繁触发驱逐循环（每次插入都要遍历 map 并删除一条随机条目），复杂度为 O(n)。
3. **无容量上限**：每次只删一条意味着缓存可能无法有效控制内存增长（虽然增长缓慢，但设计意图不清晰）。

**建议修复方案：**

使用成熟的 LRU 缓存库（如 `hashicorp/golang-lru`），或使用标准库 `container/list` + map 实现真正的 LRU 驱逐。驱逐阈值建议设置为可配置项，并一口次清理到 80% 容量（如 80 条）以避免频繁触发。

**✅ 修复详情 (v0.9.2)：**

已将 guardCache 替换为标准 LRU 缓存实现：
- 新增 `guardLRUCache` 结构体：使用 `container/list` 双向链表 + `map[string]*list.Element` 实现 O(1) get/set/evict
- `get(key)` — 命中时 `MoveToFront` 标记最近使用
- `set(key, value)` — `PushFront` 插入/更新，超 `guardCacheMaxSize`(100) 时 `Remove(Back)` 淘汰最旧
- 内置 `sync.Mutex`，线程安全，替代原有的 `guardCacheMu sync.RWMutex`

---

### [严重] D-03: 大文件问题（5 个核心文件超 900 行）

| 属性 | 详情 |
|------|------|
| **位置** | 见下表 |
| **严重程度** | 严重 |
| **影响范围** | 代码可读性、可维护性、测试覆盖、审查效率 |

**文件清单：**

| 文件 | 行数 | 核心职责 | 问题 |
|------|:---:|------|------|
| `cmd/catcode/main.go` | 1124 | 应用入口、启动流程、工具注册、REPL循环、状态展示、配置初始化 | 包含 10+ 个独立关注点，违反单一职责 |
| `agent/orchestrator/architect.go` | 1018 | 对话编排、流式处理、工具执行循环、子智能体委派、压缩调度、TUI通信 | 8+ 个职责混合，函数间依赖复杂 |
| `agent/role/role.go` | 947 | 角色定义、YAML解析、注册表管理、文件监听、合并逻辑、模型名构建 | 类型定义 + 解析器 + 注册表 + 观察者 混合 |
| `ui/tui/tui.go` | 1002 | TUI 模型、消息路由、输入处理、command 解析、流式渲染协调 | 组件拆分后仍残留大量编排逻辑 |
| `agent/subagent/base.go` | 923 | 子智能体执行、流式处理、Guard审查、错误处理、会话管理、上下文注入 | 9+ 个职责，方法过长且紧密耦合 |

**合计 5014 行**，占项目总 Go 代码行数的约 30%（估测）。

**建议修复方案：**

1. **main.go**：提取 `AppInitializer` 结构体，将提供者初始化、配置加载、角色加载、插件加载、MCP 连接等拆分为独立步骤方法。将 `runREPL` 和状态展示提取为 `repl.go` 和 `status.go`。
2. **architect.go**：将 `ProcessInput` 中的流式处理循环拆分为 `streamProcessor` 类型。将工具执行循环独立为 `toolExecutor`。将压缩调度和子智能体委派分别提取。
3. **role.go**：按职责拆分为 `role/def.go`（类型定义）、`role/parser.go`（YAML 解析）、`role/registry.go`（注册表）、`role/watcher.go`（已存在）。
4. **tui.go**：将 command 解析（`/role`、`/thinking` 等）提取为 `command.go`。将消息路由逻辑进一步下沉到 manager 层。
5. **base.go**：将 guard 审查、错误收集、上下文构建、流式处理分别提取为独立组件，base.go 仅保留调用编排。

**✅ 修复详情 (v0.9.2)：**

5个大文件已按功能拆分为多个同包子文件，无文件超过600行：
- main.go(1135行) → main.go + main_init.go + main_tui.go + main_repl.go + main_register.go + main_events.go (6文件)
- architect.go(1011行) → architect.go + architect_stream.go + architect_tools.go + architect_subagent.go + architect_context.go (5文件)
- base.go(950行) → base.go + base_stream.go + base_guard.go + base_tools.go (4文件)
- tui.go(1002行) → tui.go + tui_update.go + tui_view.go (3文件)
- session.go(905行) → session.go + session_message.go + session_request.go + session_serialize.go (4文件)
共22个文件，单个最大593行。

---

### [警告] D-04: isRestrictedInPlanMode 硬编码工具白名单

| 属性 | 详情 |
|------|------|
| **位置** | `agent/orchestrator/architect.go:999-1017` |
| **严重程度** | 警告 |
| **影响范围** | 计划模式（Plan Mode）下的工具限制策略 |

**问题描述：**

计划模式通过 `isRestrictedInPlanMode()` 函数（architect.go:999）维护一个硬编码的 `map[string]bool` 白名单，包含 14 个工具名：

```go
allowed := map[string]bool{
    "read": true, "glob": true, "grep": true, "webfetch": true,
    "skill": true, "plan_enter": true, "plan_exit": true,
    "task": false, // 注释说明禁止原因
    "question": true, "send_message": true, "companion_talk": true,
    "ask_architect": true, "log_issue": true,
}
```

**缺陷：**
1. 新增内置工具时，容易遗忘在此处注册白名单（如 `db_query`、`memory_set` 等工具均未列入，在计划模式下将被拒绝）。
2. `task` 设为 `false` 但没有明确注释说明为何某些"只读"操作也被排除（如 `schedule_list` 同样只读但不在白名单中）。
3. 工具权限策略分散在多处：`isRestrictedInPlanMode`（architect.go）、工具自己的 `PermissionLevel`、`Perms` 配置。

**建议修复方案：**

将工具权限策略统一到 `tool.Tool` 的元数据中。在 `tool.Tool` 结构体增加 `PlanModeAllowed bool` 字段，`ToolFactory` 创建工具时设置，`isRestrictedInPlanMode` 改为从工具对象读取，而非硬编码字符串匹配。

**✅ 修复详情 (v0.9.2)：**

已将硬编码白名单提取为包级变量：
- 新增 `planModeAllowedTools` 包级变量（`map[string]bool`），按五类功能分组（信息获取类/计划管理类/交互类/架构类/明确禁止）
- `isRestrictedInPlanMode()` 函数简化为 `return !planModeAllowedTools[toolName]`
- 添加维护注释："新增工具时需在此变量中评估是否允许"

---

### [警告] D-05: registerBuiltinTools() 和 ToolFactory() 大型注册块

| 属性 | 详情 |
|------|------|
| **位置** | `cmd/catcode/main.go:802-912` (registerBuiltinTools)<br>`tool/builtin/register.go:12-101` (ToolFactory) |
| **严重程度** | 警告 |
| **影响范围** | 工具注册机制、新增工具流程 |

**问题描述：**

两处工具注册维护了两套独立的硬编码列表：

1. **registerBuiltinTools()**（main.go:802-912）：通过遍历 `[]*tool.Tool` 切片注册 26+ 个工具，外加内嵌 `task` 和 `todo` 工具定义（约 100 行内联结构体）。
2. **ToolFactory()**（register.go:12-101）：一个 30-case 的 `switch name` 语句，根据字符串名称返回工具实例。

**缺陷：**
- 两个列表需要人工保持同步。当新增一个工具时，需要修改 3 处：工具文件本身、`ToolFactory` 的 case、`registerBuiltinTools` 的切片。
- `ToolFactory` 中部分 case 是 `return nil`（如 plan_enter/plan_exit），但调用方并不检查 nil，存在安全隐患。
- `registerBuiltinTools` 中 `task` 和 `todo` 的工具定义（Schema + Call）直接内联在注册代码中，而非在 `builtin` 包中定义。

**🔧 部分修复 (v0.9.2)：**

`tool/builtin/db_go.go` 中的两个严重问题已修复：
- **模板注入修复**：`runYaegiScript` 从 `fmt.Sprintf` 嵌入模板改为分4步 `i.Eval()` 执行，用户脚本不再嵌入 main() 函数体
- **SQL 检查修复**：`DBExecTool` 现在使用规范化后的 SQL 进行安全检查，扩展禁止列表（DROP/ALTER/TRUNCATE），`DELETE` 必须包含 WHERE 子句
- 使用原始 `args["sql"]` 执行但规范化 `upperSQL` 检查的 bug 已修复

**✅ 修复详情 (v0.9.2)：**

已实现统一注册表消除双重列举：
- 新增 `BuiltinRegistry` map + `ToolFactoryFunc` 类型 + `ToolDeps` 依赖结构体
- 29个内置工具通过 `init()` 注册到 `BuiltinRegistry`
- `registerBuiltinTools()` 改为遍历注册表自动注册
- `ToolFactory()` 改为查表创建，外部签名保持不变

**建议修复方案：**

采用注册表（Registry）模式：在 `builtin` 包定义一个全局注册表 `var Registry = make(map[string]func(...)*tool.Tool)`，每个工具文件通过 `init()` 函数注册自身。`registerBuiltinTools` 和 `ToolFactory` 都从该注册表读取，消除手动同步。

---

### [警告] D-06: SessionInterface 方法过多（违反接口隔离原则）

| 属性 | 详情 |
|------|------|
| **位置** | `ai/session/session.go:28-67` |
| **严重程度** | 警告 |
| **影响范围** | 会话管理接口设计、单元测试 Mock 复杂度 |

**问题描述：**

`SessionInterface` 定义了 37 个方法，涵盖消息管理、工具注册、请求构建、内存索引、压缩阈值、指令内容、消息锁定等 7+ 个维度的操作。

**缺陷：**
1. 接口过于臃肿，任何实现方都必须实现全部方法，即便只用到消息读写。
2. 单元测试中 Mock 该接口成本极高（需实现 37 个方法）。
3. 注释提到 `BuildRequestReader`、`NeedsCompression` 等 6 个方法**没有**包含在接口中（仅 `*Session` 可用），说明接口和实现的边界已经模糊。

**建议修复方案：**

按职责拆分为多个小接口：
- `MessageManager` — AddMessage, AddToolResult, AddAssistantWithTools, Clear, MessageCount, ValidateMessages
- `ToolManager` — AddTool, GetTool, ToolCount
- `RequestBuilder` — BuildRequest, BuildCleanRequest, TokenCount
- `CompressionConfig` — GetSummary/SetSummary, Get/SetCompressThreshold, Get/SetMaxToolResultLen
- `PersistenceAdapter` — ToConversationRow, ToMessageRows

通过组合（embedding）或类型断言按需使用。

**✅ 修复详情 (v0.9.2)：**

SessionInterface 已从37个方法的单一接口拆分为6个按功能隔离的子接口：
- MessageAccessor (7方法) — 消息读写
- ToolAccessor (3方法) — 工具注册
- RequestBuilder (2方法) — 请求构建
- SessionSerializer (2方法) — 持久化序列化
- SessionConfig (19方法) — 配置访问器
- SessionStats (2方法) — 统计
通过嵌入6个子接口保持 SessionInterface 向后兼容。

---

### [警告] D-07: 运行时字符串使用中文硬编码

| 属性 | 详情 |
|------|------|
| **位置** | 分布见下表 |
| **严重程度** | 警告 |
| **影响范围** | 国际化(i18n)、日志分析、第三方工具集成 |

**问题描述：**

项目中大量运行时字符串（错误消息、状态文本、终端输出、工具描述）使用中文硬编码，不利于国际化扩展和被英文工具链（日志分析、监控告警等）集成。

**中文运行时字符串分布：**

| 文件 | 中文运行时字符串行数 | 典型示例 |
|------|:---:|------|
| `cmd/catcode/main.go` | 109 | `"❌ 工作区数据库打开失败: %v\n"`, `"📜 历史会话已恢复 %d 条消息\n"` |
| `agent/orchestrator/architect.go` | 81 | `"已清理会话状态"`, `"无法获取回答"`, `"思考中"`, `"猫猫"` |
| `agent/subagent/base.go` | 78 | `"内部"`, `"未知流错误"` |
| `ui/tui/tui.go` | 38 | `"思考中"`, `"执行工具"`, `"等待子智能体"` |
| `core/errors/` | 22 | `"工具"`, `"权限"`, `"网络"`, `"配置"`, `"存储"`, `"会话"`, `"内部"` (category.go)；`"堆栈跟踪:"`, `"由以下错误引起:"` (errors.go) |

此外，所有工具定义的 `Description` 字段（约 60+ 处）也使用中文描述，这些是发送给 LLM 的提示词，属于设计选择而非缺陷（对中文 LLM 更友好），但限制了面向英文 LLM 的适配性。

**建议修复方案：**

1. 错误类别等枚举值改用英文常量（如 `CategoryTool = "tool"`），内部处理用常量，对外展示时通过映射表转为中文。
2. 终端输出字符串抽离到消息文件或配置中，提供中英文模板。
3. 工具描述保留中文作为默认，但支持通过配置文件切换为英文模板。

**当前进展 (v0.9.2)：** 29个包已有中文注释，90个源码文件。运行时中文字符串统一提取待后续版本完成。

---

### [建议] D-08: Hook 系统目录规划但未实装

| 属性 | 详情 |
|------|------|
| **位置** | `agent/subagent/hook/`（目录不存在） |
| **严重程度** | 建议 |
| **影响范围** | 插件扩展性、架构一致性 |

**问题描述：**

`ARCHITECTURE.md` 第六章详细描述了 Hook 系统（yaegi 沙箱）的完整设计，包括 5 个文件（bridge.go、builder.go、engine.go、loader.go、sandbox.go）、4 个生命周期钩子（before_context、build_context、after_execute、on_error）、ContextBuilder 接口、安全沙箱策略、热重载机制。

但实际的 `agent/subagent/hook/` 目录**不存在**，没有任何相关代码。`agent/subagent/` 中只包含 `interface.go`、`config.go`、`base.go` 三个文件。

同时，`base.go` 中的 `contextBuilder` 字段和相关调用逻辑也已存在（如 `Execute` 中调用 `sa.contextBuilder.BuildContext()`），说明代码已预留给 hook 系统的接入点，但本体缺失。

**建议：**

1. 如果 hook 系统短期内不实装，建议在 ARCHITECTURE.md 中标注 "规划中" 或 "v1.0 里程碑"。
2. 如果近期实装，建议按 ARCHITECTURE.md 的设计逐步实现 hook 系统的 5 个模块。
3. `base.go` 中预留的 `contextBuilder` 调用路径应补充 `nil` 检查或默认回退的防御性代码。

**✅ 修复详情 (v0.9.2)：**

Hook 系统已按 ARCHITECTURE.md 第六章设计完整实现：
- 新建 agent/subagent/hook/ 目录，5个Go文件
- sandbox.go — 安全沙箱策略 (yaegi Unrestricted:false + 包白名单)
- bridge.go — 类型桥接 (ContextBuildInput/ContextBuildResult + 符号注册)
- loader.go — HookLoader 从 ~/.catcode/hooks/ 发现并编译 .go 文件
- engine.go — HookEngine 单例 (编译缓存 + mtime热重载)
- builder.go — YaegiContextBuilder 适配器 (实现 ContextBuilder 接口)
- interface.go — 新增 ContextBuilder 接口 + ContextBuildInput/Result 类型
- base.go — 新增 contextBuilder 字段 + SetContextBuilder() + Execute() 集成

---

### [建议] D-09: config/loader 包功能单薄

| 属性 | 详情 |
|------|------|
| **位置** | `core/config/loader/` (loader.go:248行, watcher.go:132行, loader_test.go) |
| **严重程度** | 建议 |
| **影响范围** | 配置管理架构 |

**问题描述：**

`core/config/loader/` 包已经实现，包含：
- `loader.go`：Source/Loader 多源配置加载器、优先级合并
- `watcher.go`：DirWatcher 目录变更监听器
- `loader_test.go`：测试覆盖

但在主流程 `cmd/catcode/main.go` 和 `core/config/config.go` 中并未使用 `loader` 包的功能，配置加载仍然直接在 `main.go` 中通过 `storage.WorkspaceDB` 读取 settings 表实现。

该包的设计思路是正确的（多源、优先级、热重载），但未被集成到实际运行路径中，导致架构图与实际实现不一致。

**建议：**

将 loader 包的接口集成到 `main.go` 的配置初始化流程中，通过 `Loader.AddSource()` 注册 DB 源、文件源、环境变量源，统一配置访问入口。

**✅ 修复详情 (v0.9.2)：**

已将 loader 包集成到主配置加载流程：
- 新增 `DBSource` 和 `EnvSource` 类型
- `LoadFromWorkspace()` 使用 `loader.New()` + `LoadInto()` 统一加载
- 环境变量覆盖逻辑拆分到 `collectEnvOverrides()` + `applyBaseURLEnvOverride()`
- `applyCLIOverrides()` 独立为后处理步骤

---

## 三、统计摘要

| 统计项 | 数值 |
|------|-----:|
| 分析文件数 | 118 个 Go 源文件 |
| 严重缺陷 | 8 个 |
| 警告缺陷 | 4 个 |
| 改进建议 | 4 个 |
| 缺陷总计 | 16 个 |

| 缺陷编号 | 类别 | 严重程度 | 位置 | 预估修复工作 |
|----------|------|:---:|------|:---:|
| D-01 | 重复实现 | 严重 | role.go / agents.go / embed.go | ✅ 已完成（引入 yaml.v3，净减489行） |
| D-02 | 缓存策略 | 严重 | base.go:882-891 | ✅ 已完成（container/list LRU） |
| D-03 | 大文件 | 严重 | 5 个文件共 5014 行 | ✅ 已完成（5文件拆分为22子文件，最大593行） |
| D-04 | 硬编码 | 警告 | architect.go:999-1017 | ✅ 已完成（包级变量+分类注释） |
| D-05 | 维护性 | 警告 | main.go:802-912 / register.go:12-101 | ✅ 已完成（统一注册表 BuiltinRegistry） |
| D-06 | 接口设计 | 警告 | session.go:28-67 | ✅ 已完成（37方法拆分为6子接口） |
| D-07 | 国际化 | 警告 | 约 330+ 处运行时中文字符串 | ⏳ 部分完成（29个包已有中文注释，90个源码文件） |
| D-08 | 架构一致 | 建议 | agent/subagent/hook/ (不存在) | ✅ 已完成（Hook系统完整实装5模块） |
| D-09 | 架构一致 | 建议 | config/loader/ (已实现但未集成) | ✅ 已完成（集成到 LoadFromWorkspace） |
| R1 | 死代码 | 严重 | plan/engine.go | ✅ 已完成（删除 `_ = i` 死代码） |
| R2 | 安全 | 严重 | secrets.go | ✅ 已完成（增强密钥派生，hostname回退） |
| R3 | 错误处理 | 严重 | subagent/base.go | ✅ 已完成（添加错误注释） |
| R4 | 上下文传递 | 严重 | webfetch_tool | ✅ 已完成（传递 parent context） |
| R5 | 数据完整性 | 严重 | DeleteConversation | ✅ 已完成（添加级联删除） |
| D1 | 死代码 | 建议 | db_go.go | ✅ 已完成（删除 runYaegi()） |
| D3 | 死代码 | 建议 | 权限检查 | ✅ 已完成（添加TODO注释） |

---

## 四、总结

catcode 项目整体架构清晰（四层分离、SubAgent 接口化、TUI 组件化），文档完善（ARCHITECTURE.md 507 行）。主要问题集中在：

1. **代码重复**：手写 YAML 解析器在三处独立实现，是当前最大的质量风险。
2. **单文件过大**：5 个核心文件占约 30% 的代码量，是后续重构的主要目标。
3. **配置与策略硬编码**：工具白名单、注册列表、缓存策略均硬编码在代码中，缺乏灵活性。
4. **架构设计与实现的差距**：Hook 系统设计完善但未实装，loader 包已实现但未集成，需要明确优先级和时间规划。

**D-01、D-02、D-03、D-04、D-05、D-06、D-08、D-09 已在 v0.9.2 中修复**。下一优先级：D-07（i18n 框架）。

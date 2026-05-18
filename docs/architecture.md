# catcode 架构总览

> 精简自 [ARCHITECTURE.md](../ARCHITECTURE.md) v0.9.2 | 2026-05-16

---

## 概述

catcode 是一个 Go 语言终端 AI 编程助手。核心理念：**主编排+子智能体池**、**接口解耦**、**上下文分层**、**安全优先**。

---

## 架构分层

```
┌──────────────────────────────────────────────────────────────┐
│                 入口层  cmd/catcode — main 启动               │
├──────────────────────────────────────────────────────────────┤
│                 UI 层  ui/tui — Bubble Tea TUI               │
│   ChatComponent / Sidebar / Input / StatusBar / Question     │
│   UIManager(布局/焦点/鼠标路由)  UIAPI(插件接口)               │
├──────────────────────────────────────────────────────────────┤
│                     智能体层  agent/                          │
│  ┌──────────────────────┐  ┌────────────────────────────┐   │
│  │ Architect (主编排)    │  │ SubAgent Pool (子智能体池)   │   │
│  │ orchestrator/        │  │ pool/ (池管理,信号量=3)      │   │
│  │ 对话编排·工具循环     │  │ subagent/ (接口+BaseAgent)   │   │
│  │ 子智能体委派·压缩触发 │  │ 7种类型: explore/plan/gen/   │   │
│  │ ArchitectInterface   │  │ reviewer/verify/guard/lean4  │   │
│  └──────────────────────┘  │ 独立Session·5层上下文注入     │   │
│                            └────────────────────────────┘   │
├──────────────────────────────────────────────────────────────┤
│                    AI 层  ai/                                │
│  ┌─────────────┐  ┌─────────────┐  ┌───────────────────┐   │
│  │ session/    │  │ llm/        │  │ compact/          │   │
│  │ 会话+消息    │  │ Provider接口 │  │ 上下文压缩引擎     │   │
│  │ BuildRequest│  │ SSE解析+重试 │  │ 阈值60%·2轮保留   │   │
│  │ 消息禁用清理 │  │ ProviderReg  │  │ 边界标记+摘要注入  │   │
│  └─────────────┘  └─────────────┘  └───────────────────┘   │
├──────────────────────────────────────────────────────────────┤
│               基础设施层  core/                               │
│  errors/ (CatError+堆栈+ErrorCollector)  event/ (EventBus+26事件) │
│  config/ (env>DB>YAML三级)  buffer/ (环形缓冲区)              │
├──────────────────────────────────────────────────────────────┤
│               持久化层  data/                                 │
│  storage/ (SQLite WAL, 9表, schema v6)  embed/ (go:embed YAML) │
├──────────────────────────────────────────────────────────────┤
│       扩展层  tool/  plugin/(yaegi)  mcp/  schedule/         │
└──────────────────────────────────────────────────────────────┘
```

---

## 各层职责简述

**UI 层** — Bubble Tea 驱动，Component/Manager/Plugin 三层组件化。Chat 流式渲染、Sidebar 多标签页、Input @mention 补全、Question 对话框覆盖层、StatusBar 状态信息、UIManager 中央控制布局/焦点/鼠标路由。

**智能体层** — Architect 主编排负责对话循环、工具执行、子智能体委派；7 种子智能体共享 BaseAgent 实现，通过 Config 区分行为；Pool 管理创建/复用/空闲超时（10min）。

**AI 层** — Session 构建 5 层消息上下文（系统提示→记忆索引→压缩摘要→指令文件→对话历史）；Provider 接口支持 OpenAI 兼容 + SSE 流式 + 自动重试；Compact 智能压缩：token 估算→60% 阈值触发→head+tail 分割→摘要注入→旧消息禁用。

**基础设施层** — CatError 统一错误（消息+类别+堆栈），ErrorCollector 延迟收集避免破坏 tool_calls 配对；EventBus 26 种事件贯穿全系统；Config 三级优先级覆盖。

**持久化层** — SQLite WAL 模式管理 conversations/messages/memory/context_snapshots/scheduled_tasks/error_logs 等 9 表；go:embed 嵌入 8 种角色 YAML。

**扩展层** — tool 工具插件（Bash/Read/Write/Glob/Grep 等内置工具）；plugin yaegi 沙箱 (136 符号 9 包)；mcp MCP 客户端；schedule 定时调度。

---

## 关键设计模式

| 模式 | 说明 |
|------|------|
| 接口解耦 | SubAgent/Provider/GuardReviewer/ArchitectInterface 均为接口，打破循环依赖 |
| 事件驱动 | EventBus 26 事件类型，UI/插件通过事件与核心松耦合 |
| 配置优先级 | `env > 数据库 settings > embed YAML` 三级覆盖 |
| 零拷贝部署 | go:embed 嵌入 YAML + 插件符号表编译时注册 |
| 延迟错误收集 | ErrorCollector 在 LLM 回复完成后统一注入，保证 tool_calls 配对合法 |
| 空闲超时 | SSE 180s / 子智能体 10min / Guard 60s，收到数据时重置计时器 |
| 多层安全 | 正则拦截(rm -rf/dd/mkfs) → Guard LLM审查(带缓存) → 三级权限(Allow/Ask/Deny) → yaegi沙箱 |
| Hook 热重载 | yaegi 解释器 + mtime 检测，无需重启即可更新子智能体行为 |

---

## 数据流

```
用户输入 → TUI InputComponent
  │
  ▼
Architect.ProcessInput()
  │
  ├── 非工具调用:
  │     session.BuildRequest() → Provider.Chat(SSE) → 流式文本 → TUI 增量渲染
  │     → 途中触发 ShouldCompact(>60%) → 压缩 → 可能重试
  │
  └── 工具调用:
        ├── 普通工具 → 执行 → ErrorCollector → 结果注入消息 → 继续循环
        ├── task 工具 → Pool.Execute(子智能体) → 独立Session+Provider → 工具循环 → 流回
        └── question → TUI QuestionComponent 弹窗 → 用户选择 → 继续
```

---

## 包依赖关系

```
cmd/catcode
 ├── ui/tui ──────── agent/orchestrator, agent/plan, tool
 ├── agent
 │    ├── orchestrator ── pool, role, ai/llm, ai/session, ai/compact, core/*
 │    ├── pool ────────── agent/subagent
 │    ├── subagent ────── ai/session, ai/llm, core/*, tool, data/storage
 │    └── role ────────── core/event
 ├── ai
 │    ├── session ── core/config, core/errors
 │    ├── llm ────── core/event, core/errors
 │    └── compact ── ai/session, core/errors
 ├── core
 │    ├── errors ── 叶子包
 │    ├── event ─── 叶子包
 │    ├── config ── core, data
 │    └── buffer ── 叶子包
 ├── data
 │    ├── storage ── core/config, core/errors
 │    └── embed ──── 叶子包
 ├── tool/builtin ──── core, data/storage
 ├── plugin/mcp/schedule
```

**依赖原则**：上层依赖下层，`core/errors`、`core/event`、`core/buffer`、`data/embed` 为叶子包。

---

## 技术栈

| 类别 | 技术 |
|------|------|
| 语言 | Go 1.26 |
| TUI | Bubble Tea + Bubbles + Lipgloss |
| 数据库 | SQLite WAL (modernc.org/sqlite 纯 Go) |
| LLM 协议 | OpenAI 兼容 API + SSE 流式 |
| 插件 | yaegi Go 解释器 (沙箱) |
| 嵌入 | go:embed (8 角色 YAML + 默认配置) |
| 并发 | channel + semaphore (池最大 3 并发) |
| 渲染 | Glamour Markdown + Chroma 语法高亮 |
| 安全 | 正则拦截 + Guard 审查 + 三级权限 + 沙箱 |

# catcode 项目文档

> catcode 是一个基于 Go 的 AI 编程助手，支持多智能体编排、插件热加载和终端交互。

项目整体架构说明请查看根目录下的 [ARCHITECTURE.md](../ARCHITECTURE.md)。
> 当前版本: v0.9.2

---

## 文档目录结构

```
docs/
├── README.md                          # 本文件：文档索引与导航
├── api/                               # API 文档目录
│   ├── agent/                         # 智能体相关包
│   │   ├── role.md                    # agent/role — 角色系统
│   │   ├── subagent.md                # agent/subagent — 子智能体接口与实现
│   │   ├── pool.md                    # agent/pool — 子智能体并发执行池
│   │   ├── orchestrator.md            # agent/orchestrator — 主智能体编排器
│   │   ├── plan.md                    # agent/plan — 规划追踪引擎
│   │   └── hook.md                    # agent/subagent/hook — Hook 系统 (yaegi 沙箱)
│   ├── ai/                            # AI 相关包
│   │   ├── llm.md                     # ai/llm — LLM 提供商抽象层
│   │   ├── session.md                 # ai/session — LLM 会话管理
│   │   └── compact.md                 # ai/compact — 上下文压缩引擎
│   ├── core/                          # 核心基础设施包
│   │   ├── config.md                  # core/config — 配置管理
│   │   ├── errors.md                  # core/errors — 统一错误处理
│   │   ├── buffer.md                  # core/buffer — 零拷贝缓冲区
│   │   └── event.md                   # core/event — 事件总线
│   ├── data/                          # 数据层包
│   │   ├── storage.md                 # data/storage — SQLite 持久化层
│   │   ├── embed.md                   # data/embed — 编译时嵌入数据
│   │   └── secrets.md                 # data/storage/secrets — API Key 加密模块
│   ├── tool/
│   │   └── tool.md                    # tool — 工具系统
│   ├── cmd.md                         # cmd/catcode — 应用入口
│   ├── mcp.md                         # mcp — MCP 协议实现
│   ├── plugin.md                      # plugin — 插件系统
│   ├── schedule.md                    # schedule — 调度系统
│   └── ui.md                          # ui/tui — 终端用户界面
├── defects/                           # 缺陷与安全分析
│   ├── defect-report.md               # 代码缺陷分析报告（v0.9.2）
│   └── security-review.md             # 安全审查报告
└── guides/                            # 用户指南（待补充）
```


> **API 文档总数**: 22 份

---

## 文档分类索引

### 架构文档

项目整体架构说明请参考根目录下的 [ARCHITECTURE.md](../ARCHITECTURE.md)，涵盖模块划分、依赖关系和整体设计思路。

### API 文档

#### agent/ — 智能体包

| 文件 | 标题 | 描述 |
|------|------|------|
| [agent/role.md](api/agent/role.md) | agent/role — 角色系统 | 实现侧加载角色系统，支持功能型智能体（agent）和陪伴型角色（companion）的定义、加载与合并。 |
| [agent/subagent.md](api/agent/subagent.md) | agent/subagent — 子智能体接口与实现 | 定义子智能体的统一抽象接口及其基础实现 BaseAgent，包含工具调用循环、权限检查和流式处理。 |
| [agent/pool.md](api/agent/pool.md) | agent/pool — 子智能体并发执行池 | 实现子智能体并发执行池，通过信号量控制并发数量，支持同步/异步执行和优雅关闭。 |
| [agent/orchestrator.md](api/agent/orchestrator.md) | agent/orchestrator — 主智能体编排器 | 实现 Architect 主智能体，负责顶层协调、工具调用循环和记忆索引注入。 |
| [agent/plan.md](api/agent/plan.md) | agent/plan — 规划追踪引擎 | 实现任务分解与状态追踪引擎，支持任务依赖管理和工作流状态机。 |
| [agent/hook.md](api/agent/hook.md) | agent/subagent/hook — Hook 系统 (yaegi 沙箱) | 实现事件钩子注册与触发机制，支持生命周期事件的拦截与处理。 |

#### ai/ — AI 包

| 文件 | 标题 | 描述 |
|------|------|------|
| [ai/llm.md](api/ai/llm.md) | ai/llm — LLM 提供商抽象层 | 提供 LLM 统一抽象接口，支持多 Provider 注册、流式响应解析和工具调用增量累积。 |
| [ai/session.md](api/ai/session.md) | ai/session — LLM 会话管理 | 实现对话会话的完整生命周期管理，包含消息预编码缓存和零拷贝请求构建。 |
| [ai/compact.md](api/ai/compact.md) | ai/compact — 上下文压缩引擎 | 实现 LLM 上下文的自动压缩与多级记忆索引，支持结构化摘要生成和增量更新。 |

#### core/ — 核心基础设施包

| 文件 | 标题 | 描述 |
|------|------|------|
| [core/config.md](api/core/config.md) | core/config — 配置管理 | 实现多源配置加载系统，支持默认配置、数据库、环境变量和 CLI 参数的四级优先级覆盖。 |
| [core/errors.md](api/core/errors.md) | core/errors — 统一错误处理 | 提供统一错误类型和分类系统，包含堆栈追踪、重试判断和自纠正计数器。 |
| [core/buffer.md](api/core/buffer.md) | core/buffer — 零拷贝缓冲区 | 实现零拷贝连续字节缓冲区，通过指针切片引用预编码片段，避免字符串拼接和内存复制。 |
| [core/event.md](api/core/event.md) | core/event — 事件总线 | 实现发布-订阅事件总线和条件触发器系统，支持通配符匹配、优先级排序和递归深度保护。 |

#### data/ — 数据层包

| 文件 | 标题 | 描述 |
|------|------|------|
| [data/storage.md](api/data/storage.md) | data/storage — SQLite 持久化层 | 提供基于 SQLite 的工作区数据持久化，包含 9 张核心表和 6 个版本的 schema 演进。 |
| [data/embed.md](api/data/embed.md) | data/embed — 编译时嵌入数据 | 通过 go:embed 指令将默认角色和配置文件嵌入二进制，首次运行时写入工作区数据库。 |
| [data/secrets.md](api/data/secrets.md) | data/storage/secrets — API Key 加密模块 | 提供 API Key 的 AES-256-GCM 对称加密保护，密钥从主机 hostname+uid 通过 SHA-256 派生。 |

#### tool/ — 工具包

| 文件 | 标题 | 描述 |
|------|------|------|
| [tool/tool.md](api/tool/tool.md) | tool — 工具系统 | 实现工具注册与调度系统，支持 OpenAI function calling 协议和双回调架构。 |

#### 其他包

| 文件 | 标题 | 描述 |
|------|------|------|
| [cmd.md](api/cmd.md) | cmd/catcode — 应用入口 | 实现命令行参数解析、工作区发现、模块初始化链和双模式（TUI/REPL）启动。 |
| [mcp.md](api/mcp.md) | mcp — MCP 协议实现 | 实现 MCP 客户端，支持 Stdio 和 HTTP+SSE 传输，可同时连接多个 MCP 服务器。 |
| [plugin.md](api/plugin.md) | plugin — 插件系统 | 实现基于 yaegi 解释器的 Go 插件热加载系统，支持动态扩展工具、角色和命令。 |
| [schedule.md](api/schedule.md) | schedule — 调度系统 | 实现周期任务调度与空闲检测，支持数据库持久化的定时任务和命令执行。 |
| [ui.md](api/ui.md) | ui/tui — 终端用户界面 | 基于 Bubble Tea 框架的终端聊天界面，包含 Markdown 渲染、代码高亮和组件系统。 |

### 缺陷与安全分析

| 文件 | 标题 | 描述 |
|------|------|------|
| [defect-report.md](defects/defect-report.md) | catcode 代码缺陷分析报告 | 基于全项目静态审查的代码缺陷报告，识别出 9 类缺陷，涵盖重复实现、硬编码、缓存策略等维度。 |
| [security-review.md](defects/security-review.md) | catcode 安全审查报告 | 基于全项目安全审查的安全报告，覆盖密钥管理、网络防护、权限控制等安全维度。 |

---

## 快速导航指南

### 新加入的开发者

建议按以下顺序阅读：

1. [ARCHITECTURE.md](../ARCHITECTURE.md) — 了解项目整体架构
2. [cmd.md](api/cmd.md) — 理解应用启动流程
3. [core/config.md](api/core/config.md) — 了解配置系统
4. [core/event.md](api/core/event.md) — 理解事件驱动通信模型
5. [agent/role.md](api/agent/role.md) — 了解角色/智能体系统

### 智能体系统开发者

重点阅读 agent/ 和 ai/ 子目录：

- [agent/orchestrator.md](api/agent/orchestrator.md) — 主智能体编排逻辑
- [agent/pool.md](api/agent/pool.md) — 子智能体并发执行
- [agent/plan.md](api/agent/plan.md) — 规划追踪引擎
- [ai/llm.md](api/ai/llm.md) — LLM 提供商抽象
- [ai/session.md](api/ai/session.md) — 会话管理
- [ai/compact.md](api/ai/compact.md) — 上下文压缩

### 工具与插件开发者

- [tool/tool.md](api/tool/tool.md) — 工具注册与调用机制
- [plugin.md](api/plugin.md) — 插件热加载系统
- [mcp.md](api/mcp.md) — MCP 外部工具集成

### UI 开发者

- [ui.md](api/ui.md) — TUI 界面实现
- [core/buffer.md](api/core/buffer.md) — 零拷贝缓冲优化

### 运维与质量保障

- [defect-report.md](defects/defect-report.md) — 已知缺陷与改进建议
- [core/errors.md](api/core/errors.md) — 错误处理机制
- [data/storage.md](api/data/storage.md) — 数据库 schema 与持久化

---

## 文档维护说明

### 文档生成

API 文档基于源码结构手工编写，结合包级注释和函数签名生成。

### 缺陷报告更新

缺陷报告位于 `defects/defect-report.md`，分析范围覆盖全项目 Go 源文件。当项目有重大重构或版本迭代时，应重新执行静态分析并更新该报告。

### 指南类文档

`guides/` 目录用于存放面向用户的操作指南和最佳实践文档，目前尚待补充。欢迎提交 PR 贡献以下主题的指南：

- 快速入门
- 角色系统配置
- 插件开发教程
- MCP 服务器接入
- 性能调优

### 格式约定

- 所有文档使用 Markdown 格式
- 包文档标题格式：`包路径 — 简要描述`
- 类型和函数使用代码块展示签名
- 使用表格整理结构化信息（如优先级层级、数据库表、接口方法等）

### 贡献文档

1. 新建或修改文档文件
2. 更新本 README 的目录结构和分类索引
3. 确保所有文档链接可正常跳转

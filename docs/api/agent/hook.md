# agent/subagent/hook — Hook 系统

> 版本: v0.9.2 | 新建于第二轮迭代

## 包概述
Hook 系统允许使用 Go 脚本动态扩展子智能体行为，无需重新编译主程序。Hook 文件存放在 `~/.catcode/hooks/<agent_type>.go`，由 yaegi 解释器加载执行，受沙箱安全策略限制。

## 文件结构

| 文件 | 说明 |
|------|------|
| `sandbox.go` | 安全沙箱策略 `SandboxPolicy`，限制可用包白名单 |
| `bridge.go` | 类型桥接，注册 ContextBuildInput/Result 到 yaegi |
| `loader.go` | `HookLoader` 从磁盘发现并编译 .go 文件 |
| `engine.go` | `HookEngine` 单例，编译缓存 + mtime 热重载 |
| `builder.go` | `YaegiContextBuilder` 适配器，实现 ContextBuilder 接口 |

## 导出类型

### SandboxPolicy
安全沙箱策略，白名单: fmt/strings/strconv/time/encoding/json + catcode/core/errors
yaegi 模式: `Unrestricted: false`

### ContextBuildInput
Hook 上下文构建输入: Task, ContextSummary, AgentType, Extra

### ContextBuildResult
Hook 上下文构建输出: SystemPrompt, MemoryIndex, ExtraSystemMessages

### HookLoader
从 `~/.catcode/hooks/` 目录发现 `<agent_type>.go` 文件

### HookEngine
编译缓存管理 + 热重载 (mtime 检测文件变更自动重新编译)

### YaegiContextBuilder
实现 `subagent.ContextBuilder` 接口，执行 `hooks.BeforeContext` 和 `hooks.BuildContext` 两个注入点，5秒超时，无 Hook 时回退到默认构建器

## ContextBuilder 接口 (定义于 subagent 包)

```go
type ContextBuilder interface {
    Name() string
    BuildContext(ctx context.Context, sa *session.Session, input *ContextBuildInput) (*ContextBuildResult, error)
}
```

## 安全设计
- yaegi Unrestricted: false 模式
- 包白名单限制
- BuildContext 5秒超时
- 编译失败时静默跳过，不影响主流程

## 依赖方向
hook → subagent (ContextBuilder接口) → 无循环依赖

# core/errors — 统一错误处理

## 包概述

`core/errors` 包为 catcode 项目提供统一的错误处理机制，纯标准库实现，无外部依赖。核心设计包括：

- **`CatError`**：统一错误类型，整合错误消息、原始错误链、堆栈追踪和错误分类
- **错误类别系统**：通过预设常量将错误划分为 API、工具、权限、LLM、网络、配置、存储、会话、内部九大类
- **堆栈追踪**：在错误创建和包装时自动捕获调用堆栈，支持 `%+v` 格式化输出完整堆栈信息
- **重试判断**：`IsRetryable` 函数遍历错误链，根据错误类别和消息模式判断是否可重试
- **自纠正计数器**：`SelfCorrect` 跟踪连续错误次数，超过上限时停止自动纠正行为
- **延迟错误收集器**：`ErrorCollector` 在工具执行循环中收集多个错误，统一注入而不破坏消息配对

---

## 导出的类型

### CatError

统一错误类型，实现 `error`、`fmt.Formatter`、`errors.Unwrap` 接口。

| 字段 | 类型 | 说明 |
|------|------|------|
| `Message` | `string` | 错误描述消息 |
| `Cause` | `error` | 原始错误，支持 `errors.Unwrap` 解包 |
| `Category` | `string` | 错误类别，值为 `Category*` 常量之一（默认 `CategoryInternal`） |

**CatError 方法：**

#### `Error() string`
实现 `error` 接口。若有 `Cause`，返回格式为 `Message: Cause.Error()`；否则返回 `Message`。

#### `Unwrap() error`
实现 `errors.Unwrap` 接口，返回 `Cause` 字段，支持 `errors.Is` / `errors.As` 错误链遍历。

#### `Format(s fmt.State, verb rune)`
实现 `fmt.Formatter` 接口。

| 格式化动词 | 行为 |
|-----------|------|
| `%s` / `%v` | 与 `Error()` 相同 |
| `%+v` | 输出完整错误消息链 + 堆栈跟踪（若 `Cause` 也是 `CatError`，递归显示其堆栈） |

#### `StackTrace() string`
返回当前错误的堆栈跟踪字符串（格式为 `at 函数名 (文件:行号)`）。若错误或堆栈为 nil，返回空字符串。

#### `WithCategory(category string) *CatError`
为错误设置类别标签并返回自身，支持链式调用。

---

### Frame

单个堆栈帧的信息。

| 字段 | 类型 | 说明 |
|------|------|------|
| `File` | `string` | 文件名（含上一级目录，如 `orchestrator/architect.go`） |
| `Line` | `int` | 行号 |
| `Function` | `string` | 简短函数名（如 `(*Architect).processStream`） |
| `FullFile` | `string` | 完整文件路径 |

---

### SelfCorrect

自纠正计数器，跟踪连续错误次数并在超过上限时停止自动纠正。

#### `NewSelfCorrect(maxRetries int) *SelfCorrect`
创建自纠正计数器。`maxRetries` 指定最大允许的自纠正次数。

**SelfCorrect 方法：**

| 方法 | 签名 | 说明 |
|------|------|------|
| `Record` | `() bool` | 记录一次错误，返回 `true`（尚未超限）或 `false`（已达上限） |
| `CanContinue` | `() bool` | 返回是否可以继续自纠正（不改变计数） |
| `Count` | `() int` | 返回当前连续错误计数 |
| `MaxRetries` | `() int` | 返回最大自纠正次数 |
| `Reset` | `()` | 重置计数为 0 |

---

### ErrorCollector

延迟错误收集器，在工具执行循环中收集多个错误，支持统一注入 LLM session 而不破坏 tool 消息配对。

#### `NewCollector(maxErrors int) *ErrorCollector`
创建错误收集器。`maxErrors` 指定最大允许的错误数，超过后 `Add` 将返回 `false`。

**ErrorCollector 方法：**

| 方法 | 签名 | 说明 |
|------|------|------|
| `Add` | `(category string, err error, context string) (string, bool)` | 添加一个错误。先检查 `errorCount >= maxErrors`，若已达上限则返回 `("", false)`；否则递增 `errorCount` 并追加错误消息到内部列表。返回错误描述字符串和 `true`。当 `context` 非空时，错误消息中附加换行和上下文。 |
| `Count` | `() int` | 返回当前已收集的错误数，基于 `len(ec.errors)` 而非 `ec.errorCount` |
| `IsEmpty` | `() bool` | 返回是否尚未收集到任何错误 |
| `HasReachedLimit` | `() bool` | 返回是否已达到错误上限 |
| `FormatFeedback` | `() string` | 格式化错误反馈消息，使用 `len(ec.errors)` 获取错误编号，格式为 `【错误反馈 #n】...请分析错误原因...`。无错误时返回空字符串 |
| `MaxErrors` | `() int` | 返回最大允许的错误数 |
| `Reset` | `()` | 重置收集器，清空所有已收集的错误和计数 |

---

## 导出的构造函数

| 函数 | 签名 | 说明 |
|------|------|------|
| `New` | `(message string) *CatError` | 创建带堆栈跟踪的新错误，类别默认为 `CategoryInternal` |
| `Newf` | `(format string, args ...any) *CatError` | 创建带堆栈跟踪的格式化错误（`fmt.Sprintf` 包装） |
| `Wrap` | `(err error, message string) *CatError` | 包装现有错误并添加堆栈跟踪和消息。若 `err` 已是 `*CatError`，保留其原始堆栈和类别 |
| `Wrapf` | `(err error, format string, args ...any) *CatError` | 包装错误并格式化消息 |
| `IsRetryable` | `(err error) bool` | 遍历错误链判断是否可重试（见下方重试判断逻辑） |
| `NewSelfCorrect` | `(maxRetries int) *SelfCorrect` | 创建自纠正计数器 |
| `NewCollector` | `(maxErrors int) *ErrorCollector` | 创建错误收集器 |

---

## 重试判断逻辑 (`IsRetryable`)

`IsRetryable` 遍历错误链，满足以下任一条件即返回 `true`：

1. **类别匹配**：`CatError.Category` 为 `CategoryNetwork` 或 `CategoryAPI`
2. **消息模式匹配**：错误消息中包含以下任一关键词：
   - HTTP 状态码：`429`、`500`、`502`、`503`
   - 网络异常：`timeout`、`connection`、`TLS`、`reset`、`refused`、`EOF`、`no such host`、`network`
   - 重试提示：`重试`

---

## 错误类别常量

| 常量 | 值 | 说明 |
|------|-----|------|
| `CategoryAPI` | `"API"` | API 请求错误（400/500 等 HTTP 状态码） |
| `CategoryTool` | `"工具"` | 工具执行错误 |
| `CategoryPermission` | `"权限"` | 权限拒绝 |
| `CategoryLLM` | `"LLM"` | LLM 提供商标识错误 |
| `CategoryNetwork` | `"网络"` | 网络连接/超时错误 |
| `CategoryConfig` | `"配置"` | 配置文件错误 |
| `CategoryStorage` | `"存储"` | 数据库/文件存储错误 |
| `CategorySession` | `"会话"` | 会话状态错误 |
| `CategoryInternal` | `"内部"` | 内部/非预期错误（`New` 的默认类别） |

---

## 内部辅助函数（未导出）

以下函数仅包内使用，不对外暴露：

- `categoryFromError(err error) string` — 从已有错误中提取或猜测类别。对 `*CatError` 直接取其 `Category`；否则根据消息关键词匹配（HTTP 状态码 → `CategoryAPI`，网络关键字 → `CategoryNetwork`，权限关键字 → `CategoryPermission`）；以上都不匹配则返回 `CategoryInternal`
- `callers(skip int) *stack` — 捕获调用堆栈，`skip` 参数控制跳过帧数
- `matchRetryablePattern(msg string) bool` — 检查错误消息是否匹配可重试模式
- `containsAny(s string, substrs ...string) bool` — 字符串子串包含检查
- `skipFrame(function string) bool` — 过滤 runtime 和 errors 包自身的堆栈帧
- `shortFuncName(full string) string` — 从完整函数标识中提取简短名称
- `trimPath(full string) string` — 从完整路径中提取 `父目录/文件名` 格式

---

## 使用示例

```go
import "catcode/core/errors"

// 创建新错误
err := errors.New("配置加载失败")
// → CategoryInternal，带堆栈跟踪

// 格式化创建
err := errors.Newf("工具 %s 执行失败: %v", toolName, reason)

// 包装已有错误并指定类别
err := errors.Wrap(originalErr, "LLM 调用失败").WithCategory(errors.CategoryLLM)

// 判断是否可重试
if errors.IsRetryable(err) {
    // 重试逻辑
}

// 格式化详细输出（含堆栈）
fmt.Printf("%+v\n", err)

// 自纠正计数器
sc := errors.NewSelfCorrect(3)
for {
    if !sc.Record() {
        break // 超过 3 次，不再自纠正
    }
    // 尝试自纠正...
}

// 延迟错误收集器
ec := errors.NewCollector(5)
for _, tool := range tools {
    result, toolErr := executeTool(tool)
    if toolErr != nil {
        msg, ok := ec.Add(errors.CategoryTool, toolErr, result)
        if !ok {
            break // 达到上限，停止
        }
    }
}
feedback := ec.FormatFeedback() // 统一注入 LLM session
```

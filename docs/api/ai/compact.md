# ai/compact — 上下文压缩引擎

## 包概述

`ai/compact` 包实现了 LLM 对话上下文的压缩与多级记忆索引系统，借鉴 claude-code 的 compaction 系统与 opencode 的 overflow 检测机制。核心能力包括：

- **上下文压缩决策**：基于 token 占用率和消息数量自动判断是否需要压缩，区分"微压缩"（仅裁剪工具输出）和"完整压缩"（生成摘要并禁用旧消息）
- **Head+Tail 分割**：将对话历史划分为待压缩区域（head）和保留区域（tail），保留最近的用户轮次并控制在 token 预算内
- **结构化摘要生成**：通过 LLM 提示词将旧对话压缩为上下文索引，支持增量更新（合并前一次摘要）
- **消息重建**：压缩完成后禁用旧消息，注入压缩边界标记和摘要消息到会话中
- **微压缩**：轻量级清理旧的工具输出，保留最近 N 条
- **记忆选择器**：多级索引相关性评分，结合 Importance 权重、TF-IDF 关键词增强、描述匹配和时效性衰减，从全局和工作区记忆中筛选最相关的条目
- **智能工具输出裁剪**：从后向前保留最近的工具输出，对包含错误/警告信息的输出保持不裁剪，超长输出替换为简短摘要

## 导出的常量

| 常量名 | 值 | 说明 |
|--------|------|------|
| `AutoCompactBufferRatio` | `0.50` | 自动压缩缓冲比例，超过 50% 上下文窗口占用时触发 |
| `MinMessagesForCompact` | `10` | 触发完整压缩所需的最小消息数 |
| `PreserveTurns` | `2` | 压缩时保留的最近用户对话轮次 |
| `PreserveTokenFraction` | `0.25` | 保留 token 预算占可用上下文的比率 |
| `MaxPreserveTokens` | `8000` | 最多保留 token 数 |
| `MinPreserveTokens` | `2000` | 最少保留 token 数 |
| `MicroKeepTools` | `6` | 微压缩保留的工具输出条数 |
| `PruneMaxKeep` | `12` | 智能裁剪后最多保留的工具输出条数 |
| `PruneLongOutputThreshold` | `3000` | 长输出阈值（字符），超此长度的工具输出将被裁剪 |

## 核心类型

### CompactDecision

压缩决策结构体，由 `ShouldCompact` 返回。

```go
type CompactDecision struct {
    Needed   bool
    Level    string
    Reason   string
    TokenCnt int
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `Needed` | `bool` | 是否需要执行压缩 |
| `Level` | `string` | 压缩级别：`"none"`（无需）、`"micro"`（微压缩）、`"full"`（完整压缩） |
| `Reason` | `string` | 触发压缩的原因描述 |
| `TokenCnt` | `int` | 当前已使用的 token 数 |

### CompactResult

完整压缩的结果，由 `BuildCompactResult` 返回，用于 `ApplyCompactResult` 应用到会话。

```go
type CompactResult struct {
    BoundaryMsg    *session.Message
    SummaryContent string
    TailStartIndex int
    TokensBefore   int
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `BoundaryMsg` | `*session.Message` | 压缩边界标记消息（system 角色），标记在此消息之前的对话已被压缩 |
| `SummaryContent` | `string` | 结构化摘要内容（实际为 LLM 压缩提示词文本） |
| `TailStartIndex` | `int` | tail 区域起始索引，从此索引开始的消息被保留 |
| `TokensBefore` | `int` | 压缩前的 token 总数 |

### SplitResult

Head+Tail 分割结果，由 `SelectCompactRange` 返回。

```go
type SplitResult struct {
    HeadStart  int
    HeadEnd    int
    TailStart  int
    TailTokens int
}
```

| 字段 | 类型 | 说明 |
|------|------|------|
| `HeadStart` | `int` | head 区域的起始索引（待压缩的消息），固定为 0 |
| `HeadEnd` | `int` | head 区域的结束索引，等于 TailStart |
| `TailStart` | `int` | tail 区域的起始索引（保留的消息从此开始） |
| `TailTokens` | `int` | tail 区域估计的 token 数 |

## 函数 API

### ShouldCompact

检查是否需要执行上下文压缩。

```go
func ShouldCompact(sess *session.Session, contextWindow int) CompactDecision
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `sess` | `*session.Session` | 当前会话实例 |
| `contextWindow` | `int` | 模型的上下文窗口大小（token）；若传入 ≤0 则使用默认值 65536 |

**返回值**

`CompactDecision` — 包含压缩决策信息的结构体。

**功能描述**

1. 若 `contextWindow ≤ 0`，自动回退为 65536 token 的默认窗口。
2. 计算触发阈值：`threshold = contextWindow × (1 − AutoCompactBufferRatio)`，即上下文窗口的 50%。
3. 若当前 token 数未超过阈值，返回 `Level="none"`。
4. 若超过阈值但消息数不足 `MinMessagesForCompact`（10 条），返回 `Level="micro"`，建议执行微压缩。
5. 若超过阈值且消息数足够，返回 `Level="full"`，建议执行完整压缩。

---

### SelectCompactRange

选择压缩范围，将消息列表分割为 head（待压缩）和 tail（保留）两部分。

```go
func SelectCompactRange(messages []*session.Message, contextWindow int) SplitResult
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `messages` | `[]*session.Message` | 会话消息列表 |
| `contextWindow` | `int` | 上下文窗口大小（token） |

**返回值**

`SplitResult` — head/tail 分割结果。

**功能描述**

1. 计算保留 token 预算：`budget = contextWindow × PreserveTokenFraction`（默认 25%），钳制在 `[MinPreserveTokens, MaxPreserveTokens]` 即 `[2000, 8000]` 之间。
2. 从后向前遍历消息，仅计数 `Enable=true` 的消息，统计用户轮次（`role="user"`）和累计 token。
3. 当用户轮次达到 `PreserveTurns`（2 轮）或累计 token 超过预算时，该位置即为 tail 起始索引。
4. 若 tail 覆盖了全部消息（`TailStart ≤ 0`）或累计 token 不足预算的一半，将 tail 起始重置为 0（不压缩）。
5. Head 区域固定从索引 0 开始到 `TailStart` 结束。

---

### BuildCompactionPrompt

构建 LLM 压缩提示词，将 head 范围内的消息转换为可被模型总结的文本。

```go
func BuildCompactionPrompt(messages []*session.Message, previousSummary string, split SplitResult) string
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `messages` | `[]*session.Message` | 会话消息列表 |
| `previousSummary` | `string` | 前一次压缩的摘要；非空时使用增量更新模板 |
| `split` | `SplitResult` | 由 `SelectCompactRange` 返回的分割结果 |

**返回值**

`string` — LLM 压缩提示词文本；若 head 范围为空或无有效消息则返回空字符串。

**功能描述**

1. 若 `split.HeadEnd ≤ split.HeadStart`，直接返回空字符串。
2. 遍历 head 范围内的消息（最多取 4000 字符），每条消息截取前 200 个字符，加上角色前缀（用户/助手/工具名）后拼接为历史文本。
3. 若 `previousSummary` 非空，从嵌入资源加载 `compaction_incremental` 模板，格式化输出：`fmt.Sprintf(tmpl, previousSummary, history)`。
4. 若 `previousSummary` 为空，从嵌入资源加载 `compaction` 模板（首次压缩），格式化输出：`fmt.Sprintf(tmpl, history)`。
5. 角色前缀映射：`user → "用户: "`，`assistant → "助手: "`，`tool → "[工具 <name>]: "`，其他 → `"<role>: "`。

---

### BuildCompactResult

执行完整压缩流程，生成 `CompactResult`，包含边界标记和摘要内容。

```go
func BuildCompactResult(messages []*session.Message, previousSummary string,
    contextWindow int, tokenCnt int) *CompactResult
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `messages` | `[]*session.Message` | 会话消息列表 |
| `previousSummary` | `string` | 前一次压缩的摘要，非空时执行增量更新 |
| `contextWindow` | `int` | 上下文窗口大小（token） |
| `tokenCnt` | `int` | 压缩前的 token 总数 |

**返回值**

`*CompactResult` — 压缩结果，包含边界消息、摘要内容、tail 起始索引和压缩前 token 数。

**功能描述**

1. 调用 `SelectCompactRange` 确定 head/tail 分割。
2. 调用 `BuildCompactionPrompt` 构建压缩提示词。
3. 创建边界标记消息（`system` 角色），记录压缩时间、压缩前 token 数和保留消息起始索引。
4. 返回 `CompactResult`，其中 `SummaryContent` 字段存放的是压缩提示词（供后续 LLM 调用生成最终摘要），而非最终总结文本。

---

### ApplyCompactResult

将压缩结果应用到会话实例。

```go
func ApplyCompactResult(sess *session.Session, result *CompactResult)
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `sess` | `*session.Session` | 目标会话实例（会原地修改） |
| `result` | `*CompactResult` | 由 `BuildCompactResult` 返回的压缩结果 |

**返回值**

无。

**功能描述**

按顺序执行四个步骤：
1. **禁用旧消息**：将 `result.TailStartIndex` 之前的所有消息的 `Enable` 设为 `false`。
2. **注入边界标记**：将 `result.BoundaryMsg` 追加到会话消息列表末尾，并调用 `Update()` 刷新时间戳。
3. **注入摘要消息**：创建一条 `system` 角色消息，内容为 `"[上下文索引]\n" + result.SummaryContent`，追加到消息列表并调用 `Update()`。
4. **更新持久化摘要**：调用 `sess.SetSummary(result.SummaryContent)` 将摘要写入 Session 的持久化字段。

---

### TrimOldToolOutputs

微压缩：清理旧的工具输出消息，仅保留最近 `MicroKeepTools`（6）条。

```go
func TrimOldToolOutputs(sess *session.Session)
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `sess` | `*session.Session` | 目标会话实例 |

**返回值**

无。

**功能描述**

从后向前遍历会话消息，统计 `role="tool"` 的消息；当计数超过 `MicroKeepTools`（6）时，将更早的工具输出消息的 `Enable` 设为 `false`。这是一种轻量级压缩，不生成摘要，仅裁剪不再需要的工具输出。

---

### SessionMessagesToJSON

将会话消息序列化为 JSON 字符串，适用于快照导出。

```go
func SessionMessagesToJSON(messages []*session.Message) string
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `messages` | `[]*session.Message` | 会话消息列表 |

**返回值**

`string` — JSON 格式的字符串，每行包含 `role` 和 `content` 字段，`content` 截取前 500 字符。仅包含 `Enable=true` 的消息。

---

### SelectRelevantMemories

多级索引相关性评分，从工作区数据库中选择与当前上下文最相关的记忆条目。

```go
func SelectRelevantMemories(wdb storage.WorkspaceDB, context string, maxResults int) ([]*storage.MemoryEntry, error)
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `wdb` | `storage.WorkspaceDB` | 工作区数据库接口，提供记忆扫描和读取能力 |
| `context` | `string` | 当前上下文文本，用于提取关键词进行相关性匹配 |
| `maxResults` | `int` | 最大返回结果数 |

**返回值**

| 返回值 | 类型 | 说明 |
|------|------|------|
| `result` | `[]*storage.MemoryEntry` | 按相关性评分降序排列的记忆条目列表 |
| `err` | `error` | 错误信息（目前始终为 nil，内部错误被静默忽略） |

**功能描述**

1. **扫描记忆头部**：从 `global` 和 `workspace` 两个作用域扫描所有记忆头部（MemoryHeader），统一收集。
2. **评分维度**（每条记忆的最终分数由以下因素加权求和）：
   - **Importance 权重**（40%）：`Importance × 0.4`
   - **TF-IDF 增强**（0-15 分）：基于关键词在记忆描述中的词频与逆文档频率计算额外加分
   - **描述关键词匹配**：关键词在记忆描述中出现一次加 20 分
   - **时效性奖励/衰减**：≤1 天 +10 分，≤3 天 +5 分，>30 天 −10 分
3. 仅保留总分为正数的条目，按分数降序排列（选择排序）。
4. 调用 `wdb.GetMemory` 获取完整的 MemoryEntry 返回。

---

### PruneToolOutputs

智能裁剪工具输出，在写锁保护下清理冗余的工具输出消息。

```go
func PruneToolOutputs(sess *session.Session)
```

**参数**

| 参数 | 类型 | 说明 |
|------|------|------|
| `sess` | `*session.Session` | 目标会话实例。**调用方必须持有 Session 的写锁（mu.Lock）** |

**返回值**

无。

**功能描述**

1. 若 `sess` 为 nil 或消息列表为空，直接返回。
2. 从后向前最多保留 `PruneMaxKeep`（12）条工具输出消息。
3. **关键信息保护**：包含 `"error"`、`"Error"`、`"失败"`、`"拒绝"`、`"denied"`、`"permission"` 等关键词的工具输出始终保留，不计入保留限制。
4. **裁剪策略**：
   - 内容长度 ≤ `PruneLongOutputThreshold`（3000 字符）：直接将 `Enable` 设为 `false`
   - 内容长度 > 阈值：用 `summarizeToolOutput` 生成摘要替换原内容（截取前 400 字符并追加 `"...(工具输出已被裁剪)"`）

---

## 内部实现细节

### TF-IDF 评分算法

`scoreByTFIDFBoost` 函数实现了轻量级纯 Go 的 TF-IDF 评分，用于增强记忆相关性评估：

1. **IDF 计算**：`IDF(term) = 1 + log₂(N / docsWithTerm)`，其中 N 为记忆头部总数。
2. **TF 计算**：关键词在记忆描述中的出现次数。
3. **Boost 公式**：`Boost = TF × IDF × 3.0`，单条记忆的 TF-IDF 加分上限约 15 分。
4. `log₂` 使用自定义实现，通过循环除以 2 计算整数部分，`(n−1) × 1.442695` 近似小数部分。

### 关键词提取

`extractKeywords` 函数负责从上下文中提取有意义的索引词：

- 按空白和标点分割英文单词，过滤停用词（中英文均覆盖）和长度 < 3 的词
- 对 CJK（中日韩）字符进行 bigram（二元组）提取，识别中文汉字范围内的相邻字符对
- 所有关键词转为小写并去重

### 工具输出摘要

`summarizeToolOutput` 对超过 400 字符的长工具输出进行截断摘要：保留前 400 个字符并追加 `"...(工具输出已被裁剪)"` 标记。

### 角色前缀映射

`rolePrefix` 函数将消息角色转为中文友好的前缀字符串，用于压缩提示词的构建：

| 角色 | 前缀 |
|------|------|
| `user` | `用户: ` |
| `assistant` | `助手: ` |
| `tool` | `[工具 <name>]: ` |
| 其他 | `<role>: ` |

## 典型调用流程

```
1. ShouldCompact(sess, contextWindow)
   │
   ├─ Level="none" → 无需操作
   ├─ Level="micro" → TrimOldToolOutputs(sess) 或 PruneToolOutputs(sess)
   └─ Level="full"  → 完整压缩流程:
       │
       2. result := BuildCompactResult(sess.Messages, oldSummary, contextWindow, tokenCnt)
       │
       3. 将 result.SummaryContent 发送给 LLM 生成最终摘要文本
       │
       4. result.SummaryContent = 最终摘要文本
       │
       5. ApplyCompactResult(sess, result)
```

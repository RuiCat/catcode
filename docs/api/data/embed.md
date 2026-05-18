# data/embed — 编译时嵌入数据

## 包概述

`data/embed` 包使用 Go 1.16+ 的 `//go:embed` 指令，在编译时将默认角色定义和默认配置文件嵌入到二进制中。首次运行 catcode 时，这些种子数据会被写入工作区数据库。

该包避免了与 `role` 包的循环依赖，使用 `gopkg.in/yaml.v3` 标准库进行 YAML 反序列化提取角色文件中的关键信息。

### 嵌入的文件

| 嵌入路径 | 变量 | 说明 |
|----------|------|------|
| `default_settings.json` | `DefaultSettingsJSON` | 默认应用配置（JSON 格式） |
| `roles/*.yaml` | `DefaultRolesFS` | 默认角色定义文件 |
| `prompts/*.txt` | `DefaultPromptsFS` | 提示词模板文件 |

---

## 导出变量

### DefaultSettingsJSON

```go
var DefaultSettingsJSON []byte
```

编译时嵌入的 `default_settings.json` 原始字节内容。包含应用的默认配置项。

### DefaultRolesFS

```go
var DefaultRolesFS embed.FS
```

编译时嵌入的 `roles/*.yaml` 角色定义文件系统。包含以下角色文件：

| 角色名 | 文件 | 说明 |
|--------|------|------|
| explore | `roles/explore.yaml` | 代码探索子智能体，用于搜索代码库、查找文件和代码模式 |
| plan | `roles/plan.yaml` | 规划子智能体，负责制定执行计划和策略 |
| general | `roles/general.yaml` | 通用智能体，处理各类通用任务 |
| reviewer | `roles/reviewer.yaml` | 代码审查子智能体，检查和评审代码变更 |
| verifier | `roles/verifier.yaml` | 验证子智能体，验证代码的正确性和完整性 |
| guard | `roles/guard.yaml` | 安全守卫子智能体，审查和拦截危险操作 |
| companion | `roles/companion.yaml` | 陪伴角色（猫猫），提供轻松的互动体验 |
| lean4 | `roles/lean4.yaml` | Lean4 定理证明助手子智能体 |
| architect | `roles/architect.yaml` | 架构师子智能体，负责系统设计和架构规划 |

### DefaultPromptsFS

```go
var DefaultPromptsFS embed.FS
```

编译时嵌入的 `prompts/*.txt` 提示词模板文件系统。包含以下模板：

| 模板名 | 文件 | 说明 |
|--------|------|------|
| compaction | `prompts/compaction.txt` | 上下文压缩摘要模板 |
| compaction_incremental | `prompts/compaction_incremental.txt` | 增量式上下文压缩摘要模板 |
| guard_review | `prompts/guard_review.txt` | 安全守卫审查提示词模板 |

### DefaultSettings

```go
var DefaultSettings map[string]any
```

解析后的默认配置 map，在 `init()` 中由 `DefaultSettingsJSON` 反序列化生成。用于扁平化写入 `settings` 数据库表。

---

## 导出类型

### AgentPrompt

```go
type AgentPrompt struct {
    SystemPrompt string  // 角色系统提示词
    ModelName    string  // 完整的模型名，格式为 "provider:modelname"
    Temperature  float64 // 模型温度参数
    ContextLimit int     // 上下文窗口大小（token 数）
    OutputLimit  int     // 最大输出 token 数
    Provider     string  // 模型提供商名称
}
```

`AgentPrompt` 表示从角色 YAML 文件中提取出的智能体配置信息，包括系统提示词和模型参数。

**默认值：**

- `Temperature`：`0.1`
- `ContextLimit`：`131072`
- `OutputLimit`：`8000`

**ModelName 组合规则：**
当 `Provider` 和单独的 `ModelName` 均不为空时，`GetAgentPrompt` 会自动将它们组合为 `"provider:modelname"` 格式存入最终返回的 `AgentPrompt.ModelName` 字段。

---

## 导出函数

### GetAgentPrompt

```go
func GetAgentPrompt(name string) (*AgentPrompt, error)
```

**参数：**
- `name` — 角色名（不含 `.yaml` 后缀），如 `"explore"`、`"general"`

**返回值：**
- `*AgentPrompt` — 解析后的智能体提示词和模型配置
- `error` — 文件不存在或解析失败时返回错误

**功能描述：**
从编译时嵌入的 `roles/<name>.yaml` 文件中读取角色定义，使用 `gopkg.in/yaml.v3` 的 `yaml.Unmarshal` 进行标准 YAML 解析，提取以下字段：

- `system_prompt:` 块（支持 YAML 多行字面量 `|` 语法）
- `model.name:` — 模型名称
- `model.provider:` — 模型提供商
- `model.temperature:` — 温度参数
- `model.limit.context:` — 上下文窗口限制
- `model.limit.output:` — 输出 token 限制

**内部机制：**
使用中间结构体 `agentYAML` 承载 YAML 反序列化目标，再将其字段映射到 `AgentPrompt` 返回值。该结构体定义了与角色 YAML 文件结构对应的字段标签，`yaml.Unmarshal` 会自动处理缩进层级、多行字面量块和嵌套映射的解析。

### GetAgentTools

```go
func GetAgentTools(name string) []string
```

**参数：**
- `name` — 角色名（不含 `.yaml` 后缀）

**返回值：**
- `[]string` — 工具名称列表；文件不存在或无工具列表时返回 `nil`

**功能描述：**
从编译时嵌入的 `roles/<name>.yaml` 文件中读取角色定义，使用 `gopkg.in/yaml.v3` 的 `yaml.Unmarshal` 解析 `tools:` 字段（YAML 列表格式），返回工具名称字符串切片。

解析过程由中间结构体 `agentYAML` 的 `Tools []string` 字段承载，`yaml.Unmarshal` 自动处理列表项的反序列化。

**示例（来自 explore.yaml）：**

```yaml
tools:
  - glob
  - grep
  - read
  - bash
```

调用 `GetAgentTools("explore")` 返回 `[]string{"glob", "grep", "read", "bash"}`。

### GetPrompt

```go
func GetPrompt(name string) (string, error)
```

**参数：**
- `name` — 提示词模板名（不含 `.txt` 后缀），如 `"compaction"`、`"guard_review"`

**返回值：**
- `string` — 提示词模板的完整文本内容
- `error` — 文件不存在时返回错误

**功能描述：**
从编译时嵌入的 `prompts/<name>.txt` 文件中读取完整的提示词模板文本。模板中可能包含 `%s` 等格式占位符，供调用方使用 `fmt.Sprintf` 等进行格式化填充。

---

## 内部常量与默认值

- **默认 Temperature**：`0.1`
- **默认 ContextLimit**：`131072`
- **默认 OutputLimit**：`8000`

---

## 循环依赖设计说明

该包的设计目标之一是**避免与 `role` 包的循环依赖**。因此：

1. `GetAgentPrompt` 和 `GetAgentTools` 使用 `gopkg.in/yaml.v3` 提供的 `yaml.Unmarshal` 进行标准 YAML 解析，通过中间结构体 `agentYAML` 完成反序列化。该方案避免了导入项目内部的 `role` 包，从而解决循环依赖问题。
2. YAML 解析支持完整的 `roles/*.yaml` 文件结构，包括 `system_prompt` 字面量块、`model` 嵌套映射及其下的 `name`、`provider`、`temperature`、`limit` 子映射，以及 `tools` 列表。
3. 使用标准 YAML 库后，解析器现已完全兼容 YAML 规范（包括锚点、别名、流式集合等），不再仅限于简单子集。

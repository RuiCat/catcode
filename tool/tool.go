// Package tool 实现工具注册与调度系统
// 借鉴 catai 的 Call/CallUpdate 双回调设计：
// - Call: LLM 主动调用工具时执行
// - CallUpdate: 每次 LLM 响应后被动同步状态
package tool

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sync"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 工具定义 (OpenAI function calling 兼容)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Tool 工具定义
type Tool struct {
	Type     string  `json:"type"`     // 固定 "function"
	Function FuncDef `json:"function"` // 函数定义

	// 回调（不序列化）
	Call       ToolCallback   `json:"-"`
	CallUpdate UpdateCallback `json:"-"`
	Enable     bool           `json:"-"` // 是否启用

	// 预编码缓存（零拷贝关键）
	cachedJSON []byte `json:"-"`
}

// FuncDef 函数定义
type FuncDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ToolCallback LLM 调用工具时触发的回调
// 参数: ctx 工具上下文, args LLM 传入的参数
// 返回: 工具执行结果字符串, 错误
type ToolCallback func(ctx *Context, args map[string]any) (string, error)

// UpdateCallback LLM 每次响应后触发的被动同步回调
// 用于工具状态的自动同步（如文件快照更新、LSP 缓存刷新）
type UpdateCallback func(ctx *Context, response map[string]any)

// Context 工具执行上下文
type Context struct {
	Ctx        context.Context // 请求上下文，用于取消传播
	SessionID  string
	WorkDir    string
	ToolCallID string // LLM tool_call id
	Permission PermissionLevel
	Extra      map[string]any // 扩展数据

	// GuardReviewer bash 命令 LLM 级审查回调（可选）
	// 如果设置，在 guardCheck 正则检查通过后、命令执行前调用
	// 用于子智能体通过 guard 子智能体进行语义级安全审查
	// guard 子智能体自身应设为 nil 以避免循环
	GuardReviewer func(command string) (approved bool, reason string)
}

// PermissionLevel 权限级别
type PermissionLevel int

const (
	Allow PermissionLevel = iota // 自动允许
	Ask                          // 需用户确认
	Deny                         // 自动拒绝
)

// Update 预编码工具定义为 JSON（零拷贝关键）
func (t *Tool) Update() error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	t.cachedJSON = data
	return nil
}

// CachedJSON 返回预编码的 JSON 缓存
func (t *Tool) CachedJSON() []byte {
	return t.cachedJSON
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 工具注册表
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Registry 工具注册中心
// 线程安全，支持动态注册/注销
type Registry struct {
	mu    sync.RWMutex
	tools map[string]*Tool // name → Tool
	order []string         // 保持注册顺序
}

// NewRegistry 创建工具注册表
func NewRegistry() *Registry {
	return &Registry{
		tools: make(map[string]*Tool),
		order: make([]string, 0),
	}
}

// Register 注册工具
func (r *Registry) Register(t *Tool) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.tools[t.Function.Name]; ok {
		return cerr.Newf("tool: 重复工具名 %s", t.Function.Name)
	}

	t.Enable = true
	if err := t.Update(); err != nil {
		return cerr.Wrapf(err, "tool: 预编码失败 %s", t.Function.Name)
	}

	r.tools[t.Function.Name] = t
	r.order = append(r.order, t.Function.Name)
	return nil
}

// Unregister 注销工具
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.tools, name)
	for i, n := range r.order {
		if n == name {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
}

// Get 获取工具
func (r *Registry) Get(name string) (*Tool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tools[name]
	return t, ok
}

// All 返回所有已注册工具的切片（按注册顺序）
func (r *Registry) All() []*Tool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Tool, 0, len(r.order))
	for _, name := range r.order {
		if t, ok := r.tools[name]; ok {
			result = append(result, t)
		}
	}
	return result
}

// AllEnabled 返回所有启用的工具
func (r *Registry) AllEnabled() []*Tool {
	all := r.All()
	result := make([]*Tool, 0, len(all))
	for _, t := range all {
		if t.Enable {
			result = append(result, t)
		}
	}
	return result
}

// Enable 启用工具
func (r *Registry) Enable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tools[name]; ok {
		t.Enable = true
	}
}

// Disable 禁用工具
func (r *Registry) Disable(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, ok := r.tools[name]; ok {
		t.Enable = false
	}
}

// CallUpdateAll 对所有已注册工具执行 CallUpdate
// 借鉴 catai 设计：无论工具是否被 LLM 调用，都在响应后同步状态
func (r *Registry) CallUpdateAll(ctx *Context, response map[string]any) {
	for _, t := range r.AllEnabled() {
		if t.CallUpdate != nil {
			t.CallUpdate(ctx, response)
		}
	}
}

// Count 返回工具总数
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.tools)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 权限检查器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// PermissionRule 权限规则
type PermissionRule struct {
	Tool  string // 工具名，空字符串表示所有工具
	Path  string // 文件路径 glob 模式
	Level PermissionLevel
}

// PermissionChecker 权限检查器
type PermissionChecker struct {
	rules []PermissionRule
	mu    sync.RWMutex
}

// NewPermissionChecker 创建权限检查器
func NewPermissionChecker(rules []PermissionRule) *PermissionChecker {
	return &PermissionChecker{rules: rules}
}

// Check 检查工具对指定路径的操作权限
func (pc *PermissionChecker) Check(toolName string, path string) PermissionLevel {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	for _, rule := range pc.rules {
		if rule.Tool != "" && rule.Tool != toolName {
			continue
		}
		if rule.Path != "" && !matchGlob(rule.Path, path) {
			continue
		}
		return rule.Level
	}
	// 默认：shell 执行需确认，读取允许
	if toolName == "bash" {
		return Ask
	}
	return Allow
}

// matchGlob glob 匹配（支持 *、** 递归、? 单字符）
func matchGlob(pattern, name string) bool {
	if pattern == "*" || pattern == name {
		return true
	}
	// 使用 filepath.Match 处理标准 glob
	if matched, err := filepath.Match(pattern, name); err == nil && matched {
		return true
	}
	// 后备：简单前缀匹配
	if len(pattern) > 1 && pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(name) >= len(prefix) && name[:len(prefix)] == prefix
	}
	return false
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 权限规则解析
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// PermissionFromMap 从 map[string]any 解析权限规则
// 支持格式:
//
//	"read": "allow"              → Tool=read, Level=Allow
//	"bash": { "git *": "allow", "rm *": "deny", "*": "ask" }
//	  → Tool=bash, Path=git *, Level=Allow
//	  → Tool=bash, Path=rm *, Level=Deny
//	  → Tool=bash, Path=*, Level=Ask
func PermissionFromMap(m map[string]any) []PermissionRule {
	var rules []PermissionRule
	add := func(tool, path string, level PermissionLevel) {
		rules = append(rules, PermissionRule{Tool: tool, Path: path, Level: level})
	}

	for key, val := range m {
		// key = toolName, val = "allow"|"deny"|"ask" 或 map
		switch v := val.(type) {
		case string:
			level := parseLevel(v)
			if level >= 0 {
				add(key, "*", level)
			}
		case map[string]any:
			// 嵌套格式: bash: { "git *": "allow", "*": "ask" }
			for subKey, subVal := range v {
				if subStr, ok := subVal.(string); ok {
					level := parseLevel(subStr)
					if level >= 0 {
						add(key, subKey, level)
					}
				}
			}
		}
	}
	return rules
}

// parseLevel 解析权限级别字符串
func parseLevel(s string) PermissionLevel {
	switch s {
	case "allow":
		return Allow
	case "deny":
		return Deny
	case "ask":
		return Ask
	default:
		return -1
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// JSON Schema 构建辅助
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Schema 表示 JSON Schema 对象
type Schema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
	Enum       []string            `json:"enum,omitempty"`
}

// Property 表示 Schema 中的属性定义
type Property struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// MustMarshalSchema 将 Schema 序列化为 JSON
func MustMarshalSchema(s Schema) json.RawMessage {
	data, err := json.Marshal(s)
	if err != nil {
		panic(fmt.Sprintf("tool: Schema 序列化失败: %v", err))
	}
	return data
}

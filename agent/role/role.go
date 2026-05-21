// Package role 实现侧加载角色系统
// 角色 = 智能体配置 + 提示词，从 DB + 文件系统动态加载
// 支持功能型角色（AgentRole）和陪伴型角色（CompanionRole）
package role

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/data/storage"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// AgentRow ↔ RoleDef 转换
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// AgentRowToRoleDef 将 DB 行转换回 RoleDef
func AgentRowToRoleDef(row *storage.AgentRow) RoleDef {
	def := RoleDef{
		Name:         row.Name,
		DisplayName:  row.DisplayName,
		Type:         RoleType(row.Type),
		Mode:         RoleMode(row.Mode),
		Description:  row.Description,
		SystemPrompt: row.SystemPrompt,
		Temperature:  row.Temperature,
		SourcePath:   row.SourcePath,
		Model: ModelConfig{
			Provider: row.ModelProvider,
			Name:     row.ModelName,
		},
	}

	if row.ModelTemperature != nil {
		def.Model.Temperature = *row.ModelTemperature
	}

	if row.ThinkingEnabled {
		def.Model.Thinking = &ThinkingConfig{
			Enabled: true,
		}
		if row.ThinkingBudgetTokens != nil {
			def.Model.Thinking.BudgetTokens = *row.ThinkingBudgetTokens
		}
	}

	if row.ModelLimitContext != nil || row.ModelLimitOutput != nil {
		def.Model.Limit = &ModelLimit{}
		if row.ModelLimitContext != nil {
			def.Model.Limit.Context = *row.ModelLimitContext
		}
		if row.ModelLimitOutput != nil {
			def.Model.Limit.Output = *row.ModelLimitOutput
		}
	}

	// 解析 JSON 列
	json.Unmarshal([]byte(row.ToolsJSON), &def.Tools)
	if def.Tools == nil {
		def.Tools = []string{}
	}

	def.Permission = make(map[string]any)
	json.Unmarshal([]byte(row.PermissionJSON), &def.Permission)
	if def.Permission == nil {
		def.Permission = make(map[string]any)
	}

	// 触发器
	json.Unmarshal([]byte(row.TriggersJSON), &def.Triggers)
	if def.Triggers == nil {
		def.Triggers = []TriggerDef{}
	}

	return def
}

// RoleDefToAgentRow 将 RoleDef 转换为 DB 行结构
func RoleDefToAgentRow(def *RoleDef, source, sourcePath string) *storage.AgentRow {
	row := &storage.AgentRow{
		Name:           def.Name,
		DisplayName:    def.DisplayName,
		Type:           string(def.Type),
		Mode:           string(def.Mode),
		Description:    def.Description,
		SystemPrompt:   def.SystemPrompt,
		Temperature:    def.Temperature,
		ModelProvider:  def.Model.Provider,
		ModelName:      def.Model.Name,
		PermissionJSON: "{}",
		ToolsJSON:      "[]",
		TriggersJSON:   "[]",
		PersonaJSON:    "{}",
		StatesJSON:     "[]",
		Source:         source,
		SourcePath:     sourcePath,
		Enabled:        true,
	}

	if def.Model.Temperature != 0 {
		row.ModelTemperature = &def.Model.Temperature
	}

	if def.Model.Thinking != nil {
		row.ThinkingEnabled = def.Model.Thinking.Enabled
		row.ThinkingBudgetTokens = &def.Model.Thinking.BudgetTokens
	}

	if def.Model.Limit != nil {
		row.ModelLimitContext = &def.Model.Limit.Context
		row.ModelLimitOutput = &def.Model.Limit.Output
	}

	// 序列化 JSON 列
	if data, err := json.Marshal(def.Permission); err == nil {
		row.PermissionJSON = string(data)
	}
	if data, err := json.Marshal(def.Tools); err == nil {
		row.ToolsJSON = string(data)
	}
	if data, err := json.Marshal(def.Triggers); err == nil {
		row.TriggersJSON = string(data)
	}

	return row
}
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 角色实例
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Instance 运行时角色实例
type Instance struct {
	Def         RoleDef             // 角色定义
	Active      bool                // 是否已激活
	State       map[string]int      // 运行时状态（陪伴型角色用）
	Hooks       []Hook              // 生命周期钩子
	Tools       []*tool.Tool        // 该角色专属工具
	subscribers []*event.Subscriber // 事件总线订阅者（用于清理）
	mu          sync.RWMutex
}

// Hook 角色生命周期钩子
type Hook func(instance *Instance, evt event.Event)

// NewInstance 从定义创建角色实例
func NewInstance(def RoleDef) *Instance {
	return &Instance{
		Def:         def,
		Active:      false,
		State:       make(map[string]int),
		Hooks:       make([]Hook, 0),
		Tools:       make([]*tool.Tool, 0),
		subscribers: make([]*event.Subscriber, 0),
	}
}

// SetState 设置运行时状态
func (ins *Instance) SetState(key string, value int) {
	ins.mu.Lock()
	defer ins.mu.Unlock()
	ins.State[key] = value
}

// GetState 获取运行时状态
func (ins *Instance) GetState(key string) int {
	ins.mu.RLock()
	defer ins.mu.RUnlock()
	return ins.State[key]
}

// Activate 激活角色
func (ins *Instance) Activate() {
	ins.mu.Lock()
	ins.Active = true
	ins.mu.Unlock()
}

// Deactivate 停用角色
func (ins *Instance) Deactivate() {
	ins.mu.Lock()
	ins.Active = false
	ins.mu.Unlock()
}



// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 角色加载器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Loader 从文件系统加载角色定义
type Loader struct {
	paths []string // 搜索路径列表
	fs    EmbedFS  // 可选的嵌入文件系统（编译时打包）
}

// EmbedFS 嵌入文件系统接口（兼容 embed.FS）
type EmbedFS interface {
	ReadFile(name string) ([]byte, error)
}

// NewLoader 创建角色加载器
func NewLoader(paths []string) *Loader {
	return &Loader{paths: paths}
}

// NewLoaderWithEmbed 创建带嵌入文件系统的角色加载器
func NewLoaderWithEmbed(paths []string, embedFS EmbedFS) *Loader {
	return &Loader{paths: paths, fs: embedFS}
}

// SetPaths 设置搜索路径
func (l *Loader) SetPaths(paths []string) {
	l.paths = paths
}

// Discover 扫描所有搜索路径和嵌入文件系统，返回找到的角色定义
func (l *Loader) Discover() ([]RoleDef, error) {
	var roles []RoleDef

	// 1. 从嵌入文件系统加载（最低优先级，可被文件系统覆盖）
	if l.fs != nil {
		embedded, _ := l.loadEmbedded()
		roles = append(roles, embedded...)
	}

	// 2. 从文件系统搜索（覆盖嵌入定义）
	for _, dir := range l.paths {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			if !isRoleFile(path) {
				continue
			}
			def, err := l.Load(path)
			if err != nil {
				continue
			}
			// 覆盖同名的嵌入角色
			roles = mergeRoles(roles, def)
		}
	}
	return roles, nil
}

// loadEmbedded 从嵌入文件系统加载角色
func (l *Loader) loadEmbedded() ([]RoleDef, error) {
	// 尝试加载常见角色文件名
	roleNames := []string{
		"architect.yaml", "explore.yaml", "plan.yaml",
		"general.yaml", "reviewer.yaml", "verifier.yaml",
		"lean4.yaml",
	}
	var roles []RoleDef
	for _, name := range roleNames {
		// 尝试带 roles/ 前缀和不带前缀
		for _, prefix := range []string{"roles/", ""} {
			data, err := l.fs.ReadFile(prefix + name)
			if err != nil {
				continue
			}
			def, err := l.parseBytes(data, name)
			if err != nil {
				continue
			}
			def.SourcePath = "[embedded]/" + name
			roles = append(roles, def)
			break
		}
	}
	return roles, nil
}

// parseBytes 从字节数据解析角色定义
func (l *Loader) parseBytes(data []byte, filename string) (RoleDef, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	var def RoleDef
	var err error

	switch ext {
	case ".json":
		err = json.Unmarshal(data, &def)
	case ".yaml", ".yml":
		def, err = ParseYAML(string(data))
	default:
		return RoleDef{}, cerr.Newf("role: 不支持的文件格式 %s", ext)
	}

	if err != nil {
		return def, err
	}
	def.LoadedAt = time.Now()
	return def, def.Validate()
}

// Load 加载单个角色定义文件
func (l *Loader) Load(path string) (RoleDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RoleDef{}, cerr.Wrapf(err, "role: 读取文件失败 %s", path)
	}

	ext := strings.ToLower(filepath.Ext(path))
	var def RoleDef

	switch ext {
	case ".json":
		err = json.Unmarshal(data, &def)
	case ".yaml", ".yml":
		def, err = ParseYAML(string(data))
	default:
		return RoleDef{}, cerr.Newf("role: 不支持的文件格式 %s", ext)
	}

	if err != nil {
		return RoleDef{}, cerr.Wrapf(err, "role: 解析失败 %s", path)
	}

	def.SourcePath = path
	def.LoadedAt = time.Now()

	if err := def.Validate(); err != nil {
		return def, err
	}

	return def, nil
}

// Reload 重新加载指定路径的角色（热更新）
func (l *Loader) Reload(path string) (RoleDef, error) {
	return l.Load(path)
}

// BuildFullModelName 从 ModelConfig 构建完整 "provider:modelname" 格式
func BuildFullModelName(m ModelConfig) string {
	if m.Provider == "" {
		return m.Name
	}
	return m.Provider + ":" + m.Name
}

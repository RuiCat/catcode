// Package role 实现侧加载角色系统
// 角色 = 智能体配置 + 提示词，从 DB + 文件系统动态加载
// 支持功能型角色（AgentRole）和陪伴型角色（CompanionRole）
package role

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/data/storage"
	"catcode/tool"

	"gopkg.in/yaml.v3"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 角色定义类型
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// RoleType 角色类型
type RoleType string

const (
	RoleAgent     RoleType = "agent"     // 功能型智能体
	RoleCompanion RoleType = "companion" // 陪伴型角色
)

// RoleMode 角色运行模式
type RoleMode string

const (
	ModePrimary    RoleMode = "primary"    // 主智能体（只有一个）
	ModeSubAgent   RoleMode = "subagent"   // 子智能体（被主智能体委派）
	ModeBackground RoleMode = "background" // 后台角色（事件驱动）
)

// RoleDef 角色定义（从文件加载）
type RoleDef struct {
	Name         string         `json:"name" yaml:"name"`
	DisplayName  string         `json:"display_name" yaml:"display_name"`
	Type         RoleType       `json:"type" yaml:"type"`
	Mode         RoleMode       `json:"mode" yaml:"mode"`
	Description  string         `json:"description" yaml:"description"`
	SystemPrompt string         `json:"system_prompt" yaml:"system_prompt"`
	Model        ModelConfig    `json:"model" yaml:"model"`
	Temperature  float64        `json:"temperature" yaml:"temperature"`
	Permission   map[string]any `json:"permission" yaml:"permission"`
	Tools        []string       `json:"tools" yaml:"tools"`
	Triggers     []TriggerDef   `json:"triggers" yaml:"triggers"`

	// 陪伴型角色特有
	Persona map[string]int `json:"persona,omitempty" yaml:"persona,omitempty"`
	States  []StateDef     `json:"states,omitempty" yaml:"states,omitempty"`

	// 元数据
	SourcePath string    `json:"-" yaml:"-"`
	LoadedAt   time.Time `json:"-" yaml:"-"`
}

// ModelConfig 模型配置
type ModelConfig struct {
	Provider    string          `json:"provider" yaml:"provider"`
	Name        string          `json:"name" yaml:"name"`
	Temperature float64         `json:"temperature" yaml:"temperature"`
	Thinking    *ThinkingConfig `json:"thinking,omitempty" yaml:"thinking,omitempty"`
	Limit       *ModelLimit     `json:"limit,omitempty" yaml:"limit,omitempty"`
}

// ThinkingConfig thinking 模式
type ThinkingConfig struct {
	Enabled      bool `json:"enabled" yaml:"enabled"`
	BudgetTokens int  `json:"budget_tokens" yaml:"budget_tokens"`
}

// ModelLimit 模型限制
type ModelLimit struct {
	Context int `json:"context" yaml:"context"`
	Output  int `json:"output" yaml:"output"`
}

// TriggerDef 触发器定义
type TriggerDef struct {
	Event     string `json:"event" yaml:"event"`
	Condition string `json:"condition,omitempty" yaml:"condition,omitempty"`
	Action    string `json:"action" yaml:"action"`
	Message   string `json:"message,omitempty" yaml:"message,omitempty"`
	Priority  int    `json:"priority" yaml:"priority"`
}

// StateDef 状态定义（陪伴型角色）
type StateDef struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	RangeMin    int    `json:"range_min" yaml:"range_min"`
	RangeMax    int    `json:"range_max" yaml:"range_max"`
	Description string `json:"description" yaml:"description"`
}

// Validate 校验角色定义合法性
func (d *RoleDef) Validate() error {
	if d.Name == "" {
		return cerr.New("role: name 不能为空")
	}
	if d.Type != RoleAgent && d.Type != RoleCompanion {
		return cerr.Newf("role: 未知的 type: %s", d.Type)
	}
	if d.Mode != ModePrimary && d.Mode != ModeSubAgent && d.Mode != ModeBackground {
		return cerr.Newf("role: 未知的 mode: %s", d.Mode)
	}
	return nil
}

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
// 角色注册表
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Registry 运行时角色注册表
// RegistryInterface 角色注册表接口
type RegistryInterface interface {
	GetPrimary() *Instance
	Get(name string) (*Instance, bool)
	Count() int
	List() []*Instance
	GetAllActive() []*Instance
	Register(def RoleDef) error
	LoadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error
	ReloadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error
}


type Registry struct {
	mu     sync.RWMutex
	roles  map[string]*Instance // name → instance
	byType map[RoleType][]string
	bus    event.EventBus
	loader *Loader
}

// NewRegistry 创建角色注册表
func NewRegistry(bus event.EventBus) RegistryInterface {
	return &Registry{
		roles: make(map[string]*Instance),
		byType: map[RoleType][]string{
			RoleAgent:     {},
			RoleCompanion: {},
		},
		bus:    bus,
		loader: NewLoader(nil),
	}
}

// LoadFromWorkspace 从 DB + 文件系统加载所有角色并注册
func (r *Registry) LoadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error {
	defs, err := r.loadDefs(wdb, workDir)
	if err != nil {
		return err
	}
	for _, def := range defs {
		r.Register(def)
	}
	return nil
}

// ReloadFromWorkspace 重新加载角色（清空旧角色后重新注册）
func (r *Registry) ReloadFromWorkspace(wdb storage.WorkspaceDB, workDir string) error {
	defs, err := r.loadDefs(wdb, workDir)
	if err != nil {
		return err
	}

	r.mu.Lock()
	oldRoles := r.roles
	r.roles = make(map[string]*Instance)
	r.byType = map[RoleType][]string{
		RoleAgent:     {},
		RoleCompanion: {},
	}
	r.mu.Unlock()

	for _, inst := range oldRoles {
		r.unregisterTriggers(inst)
	}

	for _, def := range defs {
		r.Register(def)
	}
	return nil
}

// loadDefs 从 DB 和文件系统加载所有角色定义
func (r *Registry) loadDefs(wdb storage.WorkspaceDB, workDir string) ([]RoleDef, error) {
	// 1. 从 DB 加载所有 agent_definitions
	dbDefs, err := wdb.GetAllAgentDefinitions()
	if err != nil {
		return nil, cerr.Wrap(err, "role: 从 DB 加载角色失败")
	}

	// 2. 扫描 .catcode/roles/ 目录中的用户自定义角色文件
	fileDefs, err := discoverUserRoleFiles(workDir)
	if err != nil {
		return nil, cerr.Wrap(err, "role: 扫描用户角色文件失败")
	}

	// 3. 合并（3层优先级）
	return mergeAgentDefs(dbDefs, fileDefs), nil
}

// discoverUserRoleFiles 扫描 .catcode/roles/ 目录获取用户自定义角色
func discoverUserRoleFiles(workDir string) ([]RoleDef, error) {
	rolesDir := filepath.Join(workDir, ".catcode", "roles")
	entries, err := os.ReadDir(rolesDir)
	if err != nil {
		// 目录不存在不是错误
		return nil, nil
	}

	var defs []RoleDef
	l := NewLoader(nil)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := filepath.Join(rolesDir, entry.Name())
		if !isRoleFile(path) {
			continue
		}
		def, err := l.Load(path)
		if err != nil {
			continue
		}
		defs = append(defs, def)
	}
	return defs, nil
}

// Register 注册角色
func (r *Registry) Register(def RoleDef) error {
	if err := def.Validate(); err != nil {
		return err
	}

	inst := NewInstance(def)
	inst.Activate()

	r.mu.Lock()
	r.roles[def.Name] = inst
	r.byType[def.Type] = append(r.byType[def.Type], def.Name)
	r.mu.Unlock()

	// 注册角色触发器
	r.registerTriggers(inst)

	// 发布事件
	if r.bus != nil {
		r.bus.PublishAsync(event.EventRoleLoaded, map[string]any{
			"name": def.Name,
			"type": string(def.Type),
			"mode": string(def.Mode),
		})
	}

	return nil
}

// Unregister 注销角色
func (r *Registry) Unregister(name string) {
	r.mu.Lock()
	inst, ok := r.roles[name]
	if !ok {
		r.mu.Unlock()
		return
	}
	inst.Deactivate()
	delete(r.roles, name)

	// 从 byType 中移除
	typ := inst.Def.Type
	names := r.byType[typ]
	for i, n := range names {
		if n == name {
			r.byType[typ] = append(names[:i], names[i+1:]...)
			break
		}
	}
	r.mu.Unlock()

	// 清理触发器（在锁外调用，避免与事件总线产生死锁）
	r.unregisterTriggers(inst)

	if r.bus != nil {
		r.bus.PublishAsync(event.EventRoleUnloaded, map[string]any{
			"name": name,
		})
	}
}

// Get 获取角色实例
func (r *Registry) Get(name string) (*Instance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.roles[name]
	return inst, ok
}

// GetByType 获取指定类型的所有角色
func (r *Registry) GetByType(typ RoleType) []*Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Instance, 0)
	for _, name := range r.byType[typ] {
		if inst, ok := r.roles[name]; ok {
			result = append(result, inst)
		}
	}
	return result
}

// GetPrimary 获取主智能体角色
func (r *Registry) GetPrimary() *Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, inst := range r.roles {
		if inst.Def.Mode == ModePrimary && inst.Active {
			return inst
		}
	}
	return nil
}

// GetAllActive 获取所有已激活的角色
func (r *Registry) GetAllActive() []*Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*Instance, 0)
	for _, inst := range r.roles {
		if inst.Active {
			result = append(result, inst)
		}
	}
	return result
}

// List 返回所有角色实例
func (r *Registry) List() []*Instance {
	return r.GetAllActive()
}

// Count 返回角色总数
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.roles)
}

// registerTriggers 注册角色的触发器到 EventBus
func (r *Registry) registerTriggers(inst *Instance) {
	if r.bus == nil || len(inst.Def.Triggers) == 0 {
		return
	}

	for _, t := range inst.Def.Triggers {
		triggerName := fmt.Sprintf("role.%s.trigger.%s", inst.Def.Name, t.Event)
		trigger := &event.Trigger{
			Name:     triggerName,
			Event:    t.Event,
			Priority: t.Priority,
			Action: func(evt event.Event) {
				// 角色触发器被唤醒时，发布 role.dispatch 事件
				if r.bus != nil {
					r.bus.Publish(event.EventRoleActivated, map[string]any{
						"role":    inst.Def.Name,
						"trigger": triggerName,
						"message": t.Message,
						"action":  t.Action,
					})
				}
			},
		}
		// 通过 EventBus 订阅，保存返回的 Subscriber 以便后续清理
		sub := r.bus.Subscribe(triggerName, t.Event, trigger.Action, t.Priority)
		inst.subscribers = append(inst.subscribers, sub)
	}
}

// unregisterTriggers 注销角色的所有事件触发器
func (r *Registry) unregisterTriggers(inst *Instance) {
	if r.bus == nil || len(inst.subscribers) == 0 {
		return
	}
	for _, sub := range inst.subscribers {
		r.bus.Unsubscribe(sub)
	}
	inst.subscribers = nil
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

// isRoleFile 检查是否为角色定义文件
func isRoleFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".json" || ext == ".yaml" || ext == ".yml"
}

// mergeRoles 覆盖同名的已存在角色
func mergeRoles(roles []RoleDef, newDef RoleDef) []RoleDef {
	for i, r := range roles {
		if r.Name == newDef.Name {
			roles[i] = newDef // 文件系统版本覆盖嵌入版本
			return roles
		}
	}
	return append(roles, newDef)
}

// ParseYAML 从 YAML 字符串解析角色定义（公开 API）
func ParseYAML(content string) (RoleDef, error) {
	var def RoleDef
	if err := yaml.Unmarshal([]byte(content), &def); err != nil {
		return RoleDef{}, cerr.Wrap(err, "role: YAML 解析失败")
	}
	// 确保 nil 字段有默认零值
	if def.Permission == nil {
		def.Permission = make(map[string]any)
	}
	if def.Tools == nil {
		def.Tools = make([]string, 0)
	}
	if def.Triggers == nil {
		def.Triggers = make([]TriggerDef, 0)
	}
	if def.Persona == nil {
		def.Persona = make(map[string]int)
	}
	if def.States == nil {
		def.States = make([]StateDef, 0)
	}
	return def, nil
}

// BuildFullModelName 从 ModelConfig 构建完整 "provider:modelname" 格式
func BuildFullModelName(m ModelConfig) string {
	if m.Provider == "" {
		return m.Name
	}
	return m.Provider + ":" + m.Name
}

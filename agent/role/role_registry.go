package role

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/data/storage"
)

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

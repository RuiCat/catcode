package plugin

import (
	"sync"

	"catcode/agent/role"
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 插件管理器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Manager 插件生命周期管理
type Manager struct {
	mu        sync.RWMutex
	plugins   map[string]PluginInfo
	instances map[string]Plugin // 保存插件实例，用于获取工具和角色
	loader    *Loader
	ctx       *PluginContext
}

// NewManager 创建插件管理器
func NewManager(pluginsDir string, ctx *PluginContext) *Manager {
	return &Manager{
		plugins:   make(map[string]PluginInfo),
		instances: make(map[string]Plugin),
		loader:    NewLoader(pluginsDir, ctx),
		ctx:       ctx,
	}
}

// LoadAll 加载插件目录下所有插件
func (m *Manager) LoadAll() ([]PluginInfo, error) {
	files, err := m.loader.Discover()
	if err != nil {
		return nil, err
	}

	var loaded []PluginInfo
	for _, f := range files {
		info, err := m.loadOne(f)
		if err != nil {
			continue // 跳过加载失败的插件
		}
		loaded = append(loaded, info)
	}
	return loaded, nil
}

// loadOne 加载单个插件
func (m *Manager) loadOne(path string) (PluginInfo, error) {
	p, err := m.loader.Load(path)
	if err != nil {
		return PluginInfo{}, err
	}

	info := PluginInfo{
		Name:    p.Name(),
		Version: p.Version(),
		Path:    path,
		Enabled: true,
	}

	// 判断类型 — pluginWrapper 在加载时已通过方法检测确定类型
	if pw, ok := p.(*pluginWrapper); ok {
		info.Type = pw.infoType
	} else {
		// 兼容旧接口
		switch p.(type) {
		case ToolPlugin:
			info.Type = "tool"
		case RolePlugin:
			info.Type = "role"
		default:
			info.Type = "unknown"
		}
	}

	m.mu.Lock()
	m.plugins[info.Name] = info
	m.instances[info.Name] = p
	m.mu.Unlock()

	return info, nil
}

// GetTools 获取所有工具插件提供的工具
func (m *Manager) GetTools() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []PluginInfo
	for _, info := range m.plugins {
		if info.Type == "tool" && info.Enabled {
			result = append(result, info)
		}
	}
	return result
}

// GetToolInstances 获取所有工具插件的实际工具列表
// 注意：通过 yaegi 加载的插件，其 Tools() 返回的类型无法直接转换为 []*tool.Tool
// 因此目前仅支持通过 ToolPlugin 接口断言获取工具
func (m *Manager) GetToolInstances(bus event.EventBus) []*tool.Tool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var tools []*tool.Tool
	for name, p := range m.instances {
		info, ok := m.plugins[name]
		if !ok || !info.Enabled || info.Type != "tool" {
			continue
		}
		if tp, ok := p.(ToolPlugin); ok {
			tools = append(tools, tp.Tools(bus)...)
		}
	}
	return tools
}

// GetRoleInstances 获取所有角色插件的角色定义
// 注意：通过 yaegi 加载的插件，其 RoleDef() 返回的类型无法直接转换为 role.RoleDef
// 因此目前仅支持通过 RolePlugin 接口断言获取角色
func (m *Manager) GetRoleInstances() []role.RoleDef {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var defs []role.RoleDef
	for name, p := range m.instances {
		info, ok := m.plugins[name]
		if !ok || !info.Enabled || info.Type != "role" {
			continue
		}
		if rp, ok := p.(RolePlugin); ok {
			defs = append(defs, rp.RoleDef())
		}
	}
	return defs
}

// GetRoles 获取所有角色插件
func (m *Manager) GetRoles() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []PluginInfo
	for _, info := range m.plugins {
		if info.Type == "role" && info.Enabled {
			result = append(result, info)
		}
	}
	return result
}

// List 列出所有已加载插件
func (m *Manager) List() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]PluginInfo, 0, len(m.plugins))
	for _, info := range m.plugins {
		result = append(result, info)
	}
	return result
}

// Reload 重新加载指定插件
func (m *Manager) Reload(name string) error {
	m.mu.RLock()
	info, ok := m.plugins[name]
	m.mu.RUnlock()
	if !ok {
		return cerr.Newf("plugin: %s 未加载", name)
	}
	_, err := m.loadOne(info.Path)
	return err
}

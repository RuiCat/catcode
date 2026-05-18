// Package plugin 实现基于 yaegi 的 Go 插件热加载系统
// 插件可从 .catcode/plugins/ 目录动态加载，扩展工具、角色和命令
package plugin

import (
	"catcode/agent/role"
	"catcode/core/event"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 插件接口
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Plugin 所有插件必须实现的顶层接口
type Plugin interface {
	Name() string
	Version() string
}

// ToolPlugin 扩展工具的插件
type ToolPlugin interface {
	Plugin
	Tools(bus event.EventBus) []*tool.Tool
}

// RolePlugin 扩展角色的插件
type RolePlugin interface {
	Plugin
	RoleDef() role.RoleDef
}

// PluginContext 插件运行上下文
type PluginContext struct {
	WorkDir string // 工作区目录
	Bus     event.EventBus
}

// PluginInfo 插件元信息
type PluginInfo struct {
	Name    string
	Version string
	Type    string // "tool" / "role"
	Path    string // 源文件路径
	Enabled bool
}

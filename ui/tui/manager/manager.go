// Package manager 提供统一的 TUI 布局、焦点和鼠标控制器，
// 封装 renderable/input/sidebar 组件的整体布局管理。
package manager

// NOTE: UIManager 已完整实现但尚未集成到主 TUI 代码中。
// 当前主 TUI 在 tui_update.go:resizeLayout 中直接管理布局和鼠标事件，
// 与此 UIManager 是两套并行体系。
//
// 集成方案：
// 1. 将 Model.updateSidebarFocus/MouseMsg 等分散的鼠标处理迁移到 manager/mouse.go
// 2. 将 Model 的 viewport 管理替换为 UIManager 的布局计算
// 3. 逐步将 component 接口接入 UIManager
//
// 预计工作量：2-4 天。建议在 P2 代码质量阶段完成。

import (
	"catcode/ui/tui/component"
)

// UIManager 中央控制器，负责布局计算、焦点管理和全局状态
type UIManager struct {
	width  int
	height int

	// 组件引用通过回调或接口注入
	showThinking bool // 全局推理显示开关
}

// NewUIManager 创建新的 UI 管理器
func NewUIManager() *UIManager {
	return &UIManager{}
}

// SetSize 更新终端尺寸
func (m *UIManager) SetSize(width, height int) {
	m.width = width
	m.height = height
}

// Size 返回当前终端尺寸
func (m *UIManager) Size() (int, int) {
	return m.width, m.height
}

// ToggleThinking 切换思考过程显示
func (m *UIManager) ToggleThinking() bool {
	m.showThinking = !m.showThinking
	return m.showThinking
}

// IsShowThinking 返回是否显示思考过程
func (m *UIManager) IsShowThinking() bool {
	return m.showThinking
}

// LayoutChatArea 计算聊天区域 bounds（根据侧边栏宽度动态调整）
func (m *UIManager) LayoutChatArea(sidebarWidth int) component.Rect {
	return component.Rect{
		X:      0,
		Y:      0,
		Width:  m.width - sidebarWidth,
		Height: m.height - 4, // 保留输入区和状态栏
	}
}

// LayoutSidebar 计算侧边栏 bounds
func (m *UIManager) LayoutSidebar(sidebarWidth int) component.Rect {
	return component.Rect{
		X:      m.width - sidebarWidth,
		Y:      0,
		Width:  sidebarWidth,
		Height: m.height - 4,
	}
}

// LayoutInputArea 计算输入区域 bounds
func (m *UIManager) LayoutInputArea() component.Rect {
	return component.Rect{
		X:      0,
		Y:      m.height - 3,
		Width:  m.width,
		Height: 3,
	}
}

// LayoutStatusBar 计算状态栏 bounds
func (m *UIManager) LayoutStatusBar() component.Rect {
	return component.Rect{
		X:      0,
		Y:      m.height - 1,
		Width:  m.width,
		Height: 1,
	}
}

// LayoutOverlay 计算覆盖层（对话框）bounds
func (m *UIManager) LayoutOverlay() component.Rect {
	ow := m.width * 3 / 4
	oh := m.height * 2 / 3
	return component.Rect{
		X:      (m.width - ow) / 2,
		Y:      (m.height - oh) / 2,
		Width:  ow,
		Height: oh,
	}
}

package manager

// NOTE: UIManager 已完整实现但尚未集成到主 TUI 代码中。
// 这是 v0.9.2 ARCHITECTURE.md 规划的统一布局/焦点/鼠标控制器，
// 计划在未来版本中替代 tui.go 中的直接 viewport 操作。
// 当前主 TUI 的 Model 直接管理布局（tui_update.go:resizeLayout），
// 与此 UIManager 是两套并行体系，待后续统一。

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

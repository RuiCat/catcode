package manager

import (
	"catcode/ui/tui/component"

	tea "github.com/charmbracelet/bubbletea"
)

// MouseTarget 鼠标事件目标类型
type MouseTarget int

const (
	MouseTargetOverlay MouseTarget = iota // 覆盖层（对话框）
	MouseTargetInput                      // 输入区
	MouseTargetSidebar                    // 侧边栏
	MouseTargetChat                       // 聊天区
	MouseTargetNone                       // 无目标
)

// DispatchMouse 按优先级路由鼠标事件
// 路由顺序：覆盖层 > 输入区 > 侧边栏 > 聊天区
func (m *UIManager) DispatchMouse(msg tea.MouseMsg, overlayActive, inputFocused bool, mouseX, mouseY int) MouseTarget {
	// 1. 覆盖层优先
	if overlayActive {
		overlayBounds := m.LayoutOverlay()
		if isInBounds(overlayBounds, mouseX, mouseY) {
			return MouseTargetOverlay
		}
	}

	// 2. 输入区
	inputBounds := m.LayoutInputArea()
	if isInBounds(inputBounds, mouseX, mouseY) {
		return MouseTargetInput
	}

	// 3. 侧边栏（默认宽度28）
	sidebarBounds := m.LayoutSidebar(28)
	if isInBounds(sidebarBounds, mouseX, mouseY) {
		return MouseTargetSidebar
	}

	// 4. 聊天区（兜底）
	chatBounds := m.LayoutChatArea(28)
	if isInBounds(chatBounds, mouseX, mouseY) {
		return MouseTargetChat
	}

	return MouseTargetNone
}

// isInBounds 检查坐标是否在矩形内
func isInBounds(r component.Rect, x, y int) bool {
	return x >= r.X && x < r.X+r.Width && y >= r.Y && y < r.Y+r.Height
}

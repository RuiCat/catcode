package component

import tea "github.com/charmbracelet/bubbletea"

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 基础类型
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Rect 屏幕矩形区域
type Rect struct{ X, Y, Width, Height int }
func (r Rect) Contains(col, row int) bool {
	return col >= r.X && col < r.X+r.Width && row >= r.Y && row < r.Y+r.Height
}

// MessageType 消息类型
type MessageType int
const (
	MsgUser MessageType = iota
	MsgAssistant
	MsgTool
	MsgError
	MsgSystem
)

// Component 接口
type Component interface {
	Name() string
	Update(msg tea.Msg) (Component, tea.Cmd)
	View() string
	Focused() bool
	Focus() tea.Cmd
	Blur()
	HandleMouse(msg tea.MouseMsg) (bool, tea.Cmd)
	Bounds() Rect
	SetBounds(r Rect)
	Visible() bool
	SetVisible(v bool)
}

// Scrollable 可滚动组件
type Scrollable interface {
	Component
	ScrollUp(n int)
	ScrollDown(n int)
	ScrollToTop()
	ScrollToBottom()
}

// DialogComponent 对话框覆盖层
type DialogComponent interface {
	Component
	Result() <-chan interface{}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 侧边栏数据类型
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type TodoEntry struct {
	Content string
	Status  string
}

type LogEntry struct {
	Time, Content, Level string
}

type AgentEntry struct {
	Name, Status, Task string
	ToolCount int
	FullOutput string
}

type SessionInfo struct {
	ID, Title string
	MsgCount int
}

// Deprecated: QuestionInfo 使用简化字段（Header/Options []string），与主代码中
// tool.QuestionInfo（Question/Options []QuestionOption/Multiple）不兼容。
// 待统一到 tool.QuestionInfo 后移除此类型。
type QuestionInfo struct {
	Header  string
	Options []string
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// BaseComponent 默认实现
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type BaseComponent struct {
	name    string
	bounds  Rect
	visible bool
	focused bool
}

func NewBaseComponent(name string) BaseComponent {
	return BaseComponent{name: name, visible: true}
}

func (b *BaseComponent) Name() string                              { return b.name }
func (b *BaseComponent) Focused() bool                             { return b.focused }
func (b *BaseComponent) Focus() tea.Cmd                            { b.focused = true; return nil }
func (b *BaseComponent) Blur()                                     { b.focused = false }
func (b *BaseComponent) HandleMouse(msg tea.MouseMsg) (bool, tea.Cmd) { return false, nil }
func (b *BaseComponent) Bounds() Rect                              { return b.bounds }
func (b *BaseComponent) SetBounds(r Rect)                          { b.bounds = r }
func (b *BaseComponent) Visible() bool                             { return b.visible }
func (b *BaseComponent) SetVisible(v bool)                         { b.visible = v }

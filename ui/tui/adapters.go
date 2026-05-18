package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"catcode/ui/tui/component"
	"github.com/charmbracelet/lipgloss"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 适配器：将 Model 方法包装为组件接口
// 不改变渲染路径，只提供接口访问
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// statusAdapter 将 renderStatus 包装为 StatusDisplay 接口
type statusAdapter struct {
	m     *Model
	width int
}

func (s *statusAdapter) View() string {
	if s.m.statusBar != nil {
		return s.m.statusBar.View()
	}
	return s.m.renderStatus()
}
func (s *statusAdapter) SetWidth(w int)       { s.width = w }
func (s *statusAdapter) SetModelInfo(model string, tools, roles, msgs int) {
	s.m.modelName = model
	s.m.toolCount = tools
	s.m.roleCount = roles
	s.m.sessionMsgs = msgs
}
func (s *statusAdapter) SetStreamStatus(status string) { s.m.streamStatus = status }

// chatAdapter 将聊天区包装为 ChatDisplay 接口
type chatAdapter struct {
	m     *Model
	width int
}

func (c *chatAdapter) View() string          { return c.m.viewport.View() }
func (c *chatAdapter) SetWidth(w int)         { c.width = w }
func (c *chatAdapter) AddMessage(msgType component.MessageType, content string) {
	c.m.addMsg(MessageType(msgType), content)
}
func (c *chatAdapter) AppendStream(text string) {
	c.m.streamBuf.WriteString(text)
	c.m.updateLastAI()
	c.m.viewport.GotoBottom()
}
func (c *chatAdapter) StreamDone() {
	c.m.streamStatus = ""
	c.m.streamActive = false
	c.m.streamBuf.Reset()
}
func (c *chatAdapter) Refresh()              { c.m.refreshChat() }
func (c *chatAdapter) ScrollToBottom()       { c.m.viewport.GotoBottom() }
func (c *chatAdapter) ScrollUp(n int)        { c.m.viewport.ScrollUp(n) }
func (c *chatAdapter) ScrollDown(n int)      { c.m.viewport.ScrollDown(n) }
func (c *chatAdapter) ScrollToTop()          { c.m.viewport.GotoTop() }

// sidebarAdapter 将侧边栏包装为 SidebarDisplay 接口
type sidebarAdapter struct {
	m     *Model
	width int
}

func (s *sidebarAdapter) View() string {
	var parts []string
	parts = append(parts, s.m.renderSidebarTabs())
	parts = append(parts, s.m.sidebarVP.View())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
func (s *sidebarAdapter) SetWidth(w int)   { s.width = w }
func (s *sidebarAdapter) SwitchTab(tab int) { 
	s.m.sidebarTab = SidebarTab(tab)
	s.m.agentSelectedIdx = -1
	s.m.subSessionView = false
	s.m.refreshSidebar()
}
func (s *sidebarAdapter) NextTab() {
	s.m.sidebarTab = (s.m.sidebarTab + 1) % 6
	s.m.agentSelectedIdx = -1
	s.m.subSessionView = false
	s.m.refreshSidebar()
}
func (s *sidebarAdapter) SetTodos(todos []component.TodoEntry) {
	s.m.todos = convertTodoEntries(todos)
}
func (s *sidebarAdapter) SetLogs(logs []component.LogEntry) {
	s.m.logs = convertLogEntries(logs)
}
func (s *sidebarAdapter) SetAgents(agents []component.AgentEntry) {
	s.m.agents = convertAgentEntries(agents)
}
func (s *sidebarAdapter) Refresh()          { s.m.refreshSidebar() }
func (s *sidebarAdapter) ScrollUp(n int)    { s.m.sidebarVP.ScrollUp(n) }
func (s *sidebarAdapter) ScrollDown(n int)  { s.m.sidebarVP.ScrollDown(n) }
func (s *sidebarAdapter) ScrollToTop()      { s.m.sidebarVP.GotoTop() }
func (s *sidebarAdapter) ScrollToBottom()   { s.m.sidebarVP.GotoBottom() }

// inputAdapter 将输入区包装为 InputDisplay 接口
type inputAdapter struct {
	m       *Model
	width   int
	helpText string
}

func (i *inputAdapter) View() string {
	style := inputStyle
	if i.m.textarea.Focused() {
		style = inputFocusedStyle
	}
	return style.Width(i.width - 2).Render(i.m.textarea.View())
}
func (i *inputAdapter) SetWidth(w int)       { i.width = w }
func (i *inputAdapter) Focused() bool        { return i.m.textarea.Focused() }
func (i *inputAdapter) SetHelpText(text string) { i.helpText = text }
func (i *inputAdapter) HelpView() string {
	if i.helpText == "" { return "" }
	return helpStyle.Width(i.width).Render(i.helpText)
}

// 类型转换辅助函数
func convertTodoEntries(items []component.TodoEntry) []TodoEntry {
	result := make([]TodoEntry, len(items))
	for j, item := range items {
		result[j] = TodoEntry{Content: item.Content, Status: item.Status}
	}
	return result
}
func convertLogEntries(items []component.LogEntry) []LogEntry {
	result := make([]LogEntry, len(items))
	for j, item := range items {
		result[j] = LogEntry{Time: item.Time, Content: item.Content, Level: item.Level}
	}
	return result
}
func convertAgentEntries(items []component.AgentEntry) []AgentEntry {
	result := make([]AgentEntry, len(items))
	for j, item := range items {
		result[j] = AgentEntry{
			Name: item.Name, Status: item.Status, Task: item.Task,
			ToolCount: item.ToolCount, FullOutput: item.FullOutput,
		}
	}
	return result
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Component 接口补充方法（no-ops for adapters）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// chatAdapter Component methods
func (c *chatAdapter) Name() string                                 { return "chat" }
func (c *chatAdapter) Focus() tea.Cmd                               { return nil }
func (c *chatAdapter) Blur()                                        {}
func (c *chatAdapter) Focused() bool                                { return false }
func (c *chatAdapter) HandleMouse(msg tea.MouseMsg) (bool, tea.Cmd) { return false, nil }
func (c *chatAdapter) Bounds() component.Rect                       { return component.Rect{} }
func (c *chatAdapter) SetBounds(r component.Rect)                   {}
func (c *chatAdapter) Visible() bool                                { return true }
func (c *chatAdapter) SetVisible(v bool)                            {}
func (c *chatAdapter) Update(msg tea.Msg) (component.Component, tea.Cmd) { return c, nil }

// sidebarAdapter Component methods
func (s *sidebarAdapter) Name() string                                   { return "sidebar" }
func (s *sidebarAdapter) Focus() tea.Cmd                                 { return nil }
func (s *sidebarAdapter) Blur()                                          {}
func (s *sidebarAdapter) Focused() bool                                  { return false }
func (s *sidebarAdapter) HandleMouse(msg tea.MouseMsg) (bool, tea.Cmd)   { return false, nil }
func (s *sidebarAdapter) Bounds() component.Rect                         { return component.Rect{} }
func (s *sidebarAdapter) SetBounds(r component.Rect)                     {}
func (s *sidebarAdapter) Visible() bool                                  { return true }
func (s *sidebarAdapter) SetVisible(v bool)                              {}
func (s *sidebarAdapter) Update(msg tea.Msg) (component.Component, tea.Cmd) { return s, nil }

// inputAdapter Component methods
func (i *inputAdapter) Name() string                                   { return "input" }
func (i *inputAdapter) Focus() tea.Cmd                                 { return nil }
func (i *inputAdapter) Blur()                                          {}
func (i *inputAdapter) HandleMouse(msg tea.MouseMsg) (bool, tea.Cmd)   { return false, nil }
func (i *inputAdapter) Bounds() component.Rect                         { return component.Rect{} }
func (i *inputAdapter) SetBounds(r component.Rect)                     {}
func (i *inputAdapter) Visible() bool                                  { return true }
func (i *inputAdapter) SetVisible(v bool)                              {}
func (i *inputAdapter) Update(msg tea.Msg) (component.Component, tea.Cmd) { return i, nil }

// statusAdapter Component methods
func (s *statusAdapter) Name() string                                   { return "status" }

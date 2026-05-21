package component

// Renderable 可渲染组件（最小接口）
type Renderable interface {
	View() string
}

// WidthAware 需要宽度信息的组件
type WidthAware interface {
	Renderable
	SetWidth(w int)
}

// StatusDisplay 状态栏接口
type StatusDisplay interface {
	WidthAware
	SetModelInfo(model string, tools, roles, msgs int)
	SetStreamStatus(status string)
}

// ChatDisplay 聊天区接口
type ChatDisplay interface {
	WidthAware
	Scrollable
	AddMessage(msgType MessageType, content string)
	AppendStream(text string)
	StreamDone()
	Refresh()
	ScrollToBottom()
}

// SidebarDisplay 侧边栏接口
type SidebarDisplay interface {
	WidthAware
	Scrollable
	SwitchTab(tab int)
	NextTab()
	SetTodos(todos []TodoEntry)
	SetLogs(logs []LogEntry)
	SetAgents(agents []AgentEntry)
	SetPluginPanels(panels map[string]PluginPanelEntry)
	GetPluginPanels() map[string]PluginPanelEntry
	Refresh()
}

// InputDisplay 输入区接口
type InputDisplay interface {
	WidthAware
	Focused() bool
	View() string
	SetHelpText(text string)
	HelpView() string
}


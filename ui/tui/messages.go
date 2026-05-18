package tui

import (
	"time"
	"catcode/tool"
	"strings"
	"github.com/charmbracelet/lipgloss"
)

type StreamMsg string
type StreamDoneMsg struct{}
type ToolCallMsg string

type AddMessageMsg struct {
	Type    MessageType
	Content string
	Sender  string
}

type StatusMsg struct {
	ModelName string
	ToolCount int
	MsgCount  int
}

type UpdateTodosMsg struct {
	Todos []TodoEntry
}

type UpdateLogMsg struct {
	Time    string
	Content string
	Level   string
}

type UpdateAgentsMsg struct {
	Agents []AgentEntry
}

type UpdateSessionsMsg struct {
	Sessions []SessionInfo
}

type UpdateCompanionMsg struct {
	Mood       string
	Intimacy   int
	Excitement int
	Shyness    int
	Fatigue    int
}

type ScheduledTaskInfo struct {
	ID              int64
	Name            string
	Description     string
	IntervalSeconds int
	Enabled         bool
}

type UpdateTasksMsg struct {
	Tasks []ScheduledTaskInfo
}

type SessionInfoMsg struct {
	WorkspacePath  string
	PluginCount    int
	MCPServerCount int
}

type QuestionRequestMsg struct {
	Questions []QuestionInfo
	ReplyCh   chan tool.QuestionAnswer
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 辅助
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━


func truncStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "…"
}

func nowTime() string {
	return time.Now().Format("15:04:05")
}

func mutedStyle(s string) string {
	return lipgloss.NewStyle().Foreground(muted).Background(bg).Render(s)
}

func warningStyle(s string) string {
	return lipgloss.NewStyle().Foreground(warningC).Background(bg).Render(s)
}

func errStyle(s string) string {
	return lipgloss.NewStyle().Foreground(errColor).Background(bg).Render(s)
}

func accentStyle(s string) string {
	return lipgloss.NewStyle().Foreground(accent).Background(bg).Render(s)
}

func boldStyle(s string) string {
	return lipgloss.NewStyle().Foreground(primary).Background(bg).Bold(true).Render(s)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// SetOnSidebarWidthChange 设置侧边栏宽度变更回调
func (m *Model) SetOnSidebarWidthChange(cb func(int)) {
	m.onSidebarWidthChange = cb
}

// SetOnTick 设置周期任务回调
func (m *Model) SetOnTick(cb func()) {
	m.onTick = cb
}

// SidebarWidth 返回当前侧边栏宽度
func (m *Model) SidebarWidth() int {
	return m.sidebarWidth
}

// SetAgentList 设置 @mention 的智能体列表（从 DB 获取描述）
func (m *Model) SetAgentList(agents []AgentInfo) {
	m.agentList = agents
}

// HasPendingInput 是否有缓存的用户输入
func (m *Model) HasPendingInput() bool {
	return m.pendingInput != ""
}

// TakePendingInput 取出缓存的用户输入并清空
func (m *Model) TakePendingInput() string {
	input := m.pendingInput
	m.pendingInput = ""
	return input
}

func (m *Model) notifyWidthChange() {
	if m.onSidebarWidthChange != nil {
		m.onSidebarWidthChange(m.sidebarWidth)
	}
}

// wrapText 侧边栏文本换行（按字符宽度，保留缩进）

// wrapText 按宽度换行文本
func wrapText(text string, maxWidth int) string {
	if maxWidth <= 4 {
		return text
	}
	runes := []rune(text)
	var result strings.Builder
	for i := 0; i < len(runes); {
		end := i + maxWidth
		if end > len(runes) {
			end = len(runes)
		}
		result.WriteString(string(runes[i:end]))
		i = end
		if i < len(runes) {
			result.WriteString("\n")
		}
	}
	return result.String()
}

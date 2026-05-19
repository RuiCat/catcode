package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
	"time"
)

type MessageType int

const (
	MsgUser MessageType = iota
	MsgAssistant
	MsgTool
	MsgError
	MsgSystem
)

func (t MessageType) String() string {
	switch t {
	case MsgUser:
		return "👤 你"
	case MsgAssistant:
		return "🤖 AI"
	case MsgTool:
		return "🔧 工具"
	case MsgError:
		return "❌ 错误"
	case MsgSystem:
		return "📋 系统"
	}
	return ""
}

func (t MessageType) HeaderStyle() lipgloss.Style {
	switch t {
	case MsgUser:
		return userHeaderStyle
	case MsgAssistant:
		return aiHeaderStyle
	case MsgTool:
		return toolHeaderStyle
	case MsgError:
		return errHeaderStyle
	case MsgSystem:
		return sysHeaderStyle
	}
	return sysHeaderStyle
}

type ChatMessage struct {
	Type           MessageType
	Content        string
	Thinking       string // 思考/推理过程内容
	ThinkingFolded bool   // 是否折叠思考过程（默认展开，可用 /thinking 折叠）
	Sender         string
	Folded         bool // 是否折叠
}

func (m *ChatMessage) render(width int, isDark bool) string {
	renderer := NewMarkdownRenderer(width-4, isDark)
	renderedContent := renderer.Render(m.Content)

	// 渲染思考过程（如果存在）
	var thinkingBlock string
	if m.Thinking != "" {
		if m.ThinkingFolded {
			thinkingBlock = thinkingFoldedStyle.Render("🧠 思考过程 (Enter 展开)")
		} else {
			truncated := m.Thinking
			if len(truncated) > 5000 {
				truncated = truncated[:5000] + "\n... (更多内容已省略)"
			}
			header := lipgloss.NewStyle().Foreground(lipgloss.Color("#666")).Bold(true).Render("🧠 思考过程:")
			thinkingContent := header + "\n" + thinkingTextStyle.Width(width-6).Render(truncated)
			thinkingBlock = thinkingBorderStyle.Width(width - 4).Render(thinkingContent)
		}
		renderedContent = thinkingBlock + "\n" + renderedContent
	}

	switch m.Type {
	case MsgUser:
		// 用户消息：细边框左对齐
		return userBorderStyle.Render(renderedContent)
	case MsgAssistant:
		// AI 消息：纯文本，无头部
		return aiContentStyle.Render(renderedContent)
	case MsgTool:
		// 工具消息：小图标 + 缩小字体
		return toolContentStyle.Render(renderedContent)
	case MsgError:
		return errContentStyle.Render(renderedContent)
	case MsgSystem:
		return sysContentStyle.Render(renderedContent)
	}

	// 折叠处理
	lines := strings.Split(renderedContent, "\n")
	if m.Folded && len(lines) > 8 {
		preview := strings.Join(lines[:8], "\n")
		moreLine := fmt.Sprintf("\n  … 还有 %d 行 (按 Enter 展开)", len(lines)-8)
		moreStyle := lipgloss.NewStyle().Foreground(accent).Background(bg).Italic(true)
		return preview + moreStyle.Render(moreLine)
	}

	return renderedContent
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 侧边栏 Tab
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// TabKey 侧边栏标签唯一标识
type TabKey = string

// 内置 Tab 键名
const (
	TabPlan      TabKey = "plan"
	TabLog       TabKey = "log"
	TabAgents    TabKey = "agents"
	TabCompanion TabKey = "companion"
	TabTasks     TabKey = "tasks"
	TabSession   TabKey = "session"
)

// TabDef 侧边栏标签定义
type TabDef struct {
	Key      TabKey
	Title    string
	Shortcut string
	Builtin  bool
	Render   func(m *Model, sb *strings.Builder)
}

// PluginPanel 插件侧边栏面板
type PluginPanel struct {
	Key     string
	Title   string
	Content string
}

// UpdatePluginPanelsMsg TUI消息：更新插件面板
type UpdatePluginPanelsMsg struct {
	Panels       map[string]PluginPanel
	ActivateFirst bool // 是否自动切换到第一个插件面板（启动时使用）
}

// ActivateSidebarTabMsg TUI消息：激活指定侧边栏标签
type ActivateSidebarTabMsg struct {
	Key TabKey
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 日志条目
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type LogEntry struct {
	Time    string
	Content string
	Level   string // "info", "warn", "error", "debug"
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Todo 条目
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type TodoEntry struct {
	Content string
	Status  string // "done", "active", "pending", "failed"
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 子智能体状态
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type AgentEntry struct {
	Name        string
	ID          string // Agent 唯一 ID
	Status      string // "pending", "running", "completed", "error", "idle"
	Task        string
	FullTask    string // 完整任务描述
	Spinner     int
	CurrentTool string        // 当前执行工具名
	ToolCount   int           // 已执行工具数
	StartTime   time.Time     // 开始时间
	Duration    time.Duration // 完成耗时 (completed 时有值)
	ErrorMsg    string        // 错误信息 (error 时有值)
	FullOutput  string        // 格式化完整输出
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 会话信息
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type SessionInfo struct {
	ID           string
	Model        string
	MessageCount int
	IsActive     bool
}

package tui

import (
	"fmt"
	"strings"
	"time"
	"catcode/tool"
	"catcode/ui/tui/component"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type Model struct {
	width  int
	height int
	ready  bool

	// 聊天区
	viewport     viewport.Model
	messages     []*ChatMessage
	streamStatus string // 流式状态: ""=未流式, "思考中"=等待LLM, "执行工具: xxx"=工具调用, "等待子智能体: xxx"=子智能体
	streamBuf    strings.Builder
	streamTokens int // 已收到的 token 数

	// 不打断思考的插入
	pendingInput string // 流式期间缓存的用户输入
	streamActive bool   // 是否正在流式输出（防重入）

	// 思考过程
	thinkingBuf    strings.Builder // 流式思考内容累积
	thinkingActive bool            // 当前是否在接收思考内容
	showThinking   bool            // 全局开关：是否显示思考过程
	status  component.StatusDisplay
	chat    component.ChatDisplay
	side    component.SidebarDisplay
	input   component.InputDisplay

	// 选项框模式
	questionMode      bool
	questionQuestions []tool.QuestionInfo
	questionAnswers   [][]string
	questionSelected  int
	questionTab       int
	questionReply     chan tool.QuestionAnswer
	questionPending   []QuestionRequestMsg // 排队的选项框（当前 question 完成后再展示）

	// @mention 自动补全
	mentionState *MentionState
	agentList    []AgentInfo // DB 获取的智能体描述列表

	// 周期任务
	scheduledTasks []ScheduledTaskInfo

	// 会话信息
	workspacePath  string
	pluginCount    int
	mcpServerCount int

	// 输入区
	textarea textarea.Model

	// 侧边栏
	sidebarTab   SidebarTab
	sidebarVP    viewport.Model
	todos        []TodoEntry
	logs         []LogEntry
	agents       []AgentEntry
	sessions     []SessionInfo
	sidebarWidth int

	// 子会话导航
	agentSelectedIdx int            // -1 = 未选中, 0+ = 选中的 agent 索引
	subSessionView   bool           // true = 当前在子会话视图
	subSessionAgent  *AgentEntry    // 正在查看的子智能体
	subSessionVP     viewport.Model // 子会话独立视口

	// 猫猫状态
	companionMood       string
	companionIntimacy   int
	companionExcitement int
	companionShyness    int
	companionFatigue    int

	// 状态
	modelName   string
	toolCount   int
	roleCount   int
	sessionMsgs int
	isDark      bool // 主题

	// 搜索模式
	searchMode  bool
	searchQuery string

	// 回调
	onSubmit             func(string)
	onCancel             func()
	onSidebarWidthChange func(int) // 侧边栏宽度变更回调
	onTick               func()    // 周期任务回调

	// 渲染器缓存
	mdRenderer *MarkdownRenderer
	statusBar  *component.StatusBarComponent
}

func New(modelName string, toolCount, roleCount int, sidebarWidth int, onSubmit func(string)) *Model {
	ta := textarea.New()
	ta.Placeholder = "输入消息..."
	ta.ShowLineNumbers = false
	ta.CharLimit = 8000
	ta.SetHeight(3)
	ta.SetWidth(80)
	ta.Focus()

	// 设置 textarea 内部颜色统一
	ta.FocusedStyle.Base = lipgloss.NewStyle().Background(bg)
	ta.BlurredStyle.Base = lipgloss.NewStyle().Background(bg)
	ta.FocusedStyle.Text = lipgloss.NewStyle().Foreground(fg)
	ta.BlurredStyle.Text = lipgloss.NewStyle().Foreground(fg)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle().Background(bg)
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle().Background(bg)
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(muted)
	ta.BlurredStyle.Placeholder = lipgloss.NewStyle().Foreground(muted)
	ta.FocusedStyle.Prompt = lipgloss.NewStyle().Foreground(primary)
	ta.BlurredStyle.Prompt = lipgloss.NewStyle().Foreground(primary)

	chatVP := viewport.New(60, 20)
	chatVP.Style = lipgloss.NewStyle().Foreground(fg).Background(bg)

	sideVP := viewport.New(30, 20)
	sideVP.Style = lipgloss.NewStyle().Foreground(fg).Background(bg)

	m := &Model{
		viewport:         chatVP,
		textarea:         ta,
		sidebarVP:        sideVP,
		sidebarTab:       TabPlan,
		sidebarWidth:     sidebarWidth,
		messages:         make([]*ChatMessage, 0),
		todos:            make([]TodoEntry, 0),
		logs:             make([]LogEntry, 0),
		agents:           make([]AgentEntry, 0),
		sessions:         make([]SessionInfo, 0),
		modelName:        modelName,
		toolCount:        toolCount,
		roleCount:        roleCount,
		onSubmit:         onSubmit,
		width:            110,
		height:           30,
		ready:            true,
		isDark:           true,
		agentSelectedIdx: -1,
		subSessionVP:     viewport.New(60, 20),
	}

	m.mdRenderer = NewMarkdownRenderer(60, true)

	m.showThinking = true
	m.statusBar = component.NewStatusBar()
	m.status = &statusAdapter{m: m}
	m.chat = &chatAdapter{m: m}
	m.side = &sidebarAdapter{m: m}
	m.input = &inputAdapter{m: m}

	m.refreshChat()
	m.refreshSidebar()
	m.addMsg(MsgSystem, fmt.Sprintf("	🐱 欢迎使用 catcode\n	**模型**: `%s`  **工具**: `%d`  **角色**: `%d`", modelName, toolCount, roleCount))
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		tickSidebar(),
	)
}

func tickSidebar() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// TickMsg 定时器消息
type TickMsg time.Time

func (m *Model) resizeLayout() {
	chatW := max(40, m.width-m.sidebarWidth-2)
	sideW := m.sidebarWidth

	m.viewport.Width = chatW
	m.viewport.Height = max(5, m.height-8)
	m.sidebarVP.Width = sideW
	m.sidebarVP.Height = max(5, m.height-8)
	m.textarea.SetWidth(m.width - 4)
	m.mdRenderer = NewMarkdownRenderer(chatW-4, m.isDark)
	m.refreshChat()
	m.refreshSidebar()
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 组件接口访问器（供插件和外部代码使用）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (m *Model) StatusDisplay() component.StatusDisplay {
	return &statusAdapter{m: m}
}

func (m *Model) ChatDisplay() component.ChatDisplay {
	return &chatAdapter{m: m}
}

func (m *Model) SidebarDisplay() component.SidebarDisplay {
	return &sidebarAdapter{m: m}
}

func (m *Model) InputDisplay() component.InputDisplay {
	return &inputAdapter{m: m}
}

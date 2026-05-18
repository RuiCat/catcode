package tui

import (
	"fmt"
	"strings"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Update
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// 首次初始化时从参数值读取，窗口大小变化时保持当前宽度
		if m.sidebarWidth == 0 {
			m.sidebarWidth = 28
		}
		// 窄屏时自动隐藏侧边栏
		if msg.Width < 80 {
			m.sidebarWidth = 0
		}
		chatW := max(40, msg.Width-m.sidebarWidth-2)
		sideW := m.sidebarWidth

		// 确保高度至少为 5，避免 viewport 高度为 0
		sideH := max(5, msg.Height-8)
		chatH := max(5, msg.Height-8)

		if !m.ready {
			m.viewport = viewport.New(chatW, chatH)
			m.sidebarVP = viewport.New(sideW, sideH)
			m.textarea.SetWidth(msg.Width - 4)
			m.ready = true
		} else {
			m.viewport.Width = chatW
			m.viewport.Height = chatH
			m.sidebarVP.Width = sideW
			m.sidebarVP.Height = sideH
			m.textarea.SetWidth(msg.Width - 4)
		}
		m.mdRenderer = NewMarkdownRenderer(chatW-4, m.isDark)
		m.subSessionVP.Width = chatW
		m.subSessionVP.Height = chatH
		m.refreshChat()
		m.refreshSidebar()

	case tea.KeyMsg:
		// 子会话视图 — 拦截所有按键
		if m.subSessionView {
			var cmd tea.Cmd
			switch msg.String() {
			case "esc":
				m.subSessionView = false
				m.subSessionAgent = nil
				m.refreshChat()
				m.refreshSidebar()
				return m, nil
			case "ctrl+c":
				// 子会话中 Ctrl+C：先退出子会话，不直接退出程序
				m.subSessionView = false
				m.subSessionAgent = nil
				m.refreshChat()
				m.refreshSidebar()
				return m, nil
			case "f1":
				m.sidebarTab = TabPlan
				m.agentSelectedIdx = -1
				m.refreshSidebar()
				return m, nil
			case "f2":
				m.sidebarTab = TabLog
				m.agentSelectedIdx = -1
				m.refreshSidebar()
				return m, nil
			case "f3":
				m.sidebarTab = TabAgents
				m.agentSelectedIdx = -1
				m.refreshSidebar()
				return m, nil
			case "f4":
				m.sidebarTab = TabSession
				m.agentSelectedIdx = -1
				m.refreshSidebar()
				return m, nil
			case "f5":
				m.sidebarTab = TabCompanion
				m.agentSelectedIdx = -1
				m.refreshSidebar()
				return m, nil
			case "f6":
				m.sidebarTab = TabTasks
				m.agentSelectedIdx = -1
				m.refreshSidebar()
				return m, nil
			case "tab":
				tabs := []SidebarTab{TabPlan, TabLog, TabAgents, TabCompanion, TabTasks, TabSession}
				for i, t := range tabs {
					if t == m.sidebarTab {
						m.sidebarTab = tabs[(i+1)%len(tabs)]
						break
					}
				}
				m.agentSelectedIdx = -1
				m.refreshSidebar()
				return m, nil
			case "up", "down", "pgup", "pgdown", "home", "end":
				m.subSessionVP, cmd = m.subSessionVP.Update(msg)
				return m, cmd
			case "ctrl+s":
				return m, nil
			default:
				return m, nil
			}
		}

		// @mention 菜单键盘优先
		if m.mentionState != nil && m.mentionState.Active {
			if m.HandleMentionKey(msg.String()) {
				return m, nil
			}
		}

		// 选项框模式：导航键由 handleQuestionKey 处理，滚动键（pgup/pgdown等）被其拦截
		// 其他键正常往下传递，viewport 正常更新（不添加任何守卫）
		if m.questionMode {
			if m.handleQuestionKey(msg.String()) {
				return m, nil
			}
		}

		// 搜索模式
		if m.searchMode {
			switch msg.String() {
			case "enter", "esc":
				m.searchMode = false
				return m, nil
			case "backspace":
				if len(m.searchQuery) > 0 {
					m.searchQuery = m.searchQuery[:len(m.searchQuery)-1]
				}
				return m, nil
			default:
				if len(msg.String()) == 1 {
					m.searchQuery += msg.String()
				}
				return m, nil
			}
		}

		// Agents Tab 选中导航
		if m.sidebarTab == TabAgents && len(m.agents) > 0 {
			switch msg.String() {
			case "up":
				if m.agentSelectedIdx > 0 {
					m.agentSelectedIdx--
				} else {
					m.agentSelectedIdx = len(m.agents) - 1
				}
				m.refreshSidebar()
				return m, nil
			case "down":
				if m.agentSelectedIdx < len(m.agents)-1 {
					m.agentSelectedIdx++
				} else {
					m.agentSelectedIdx = 0
				}
				m.refreshSidebar()
				return m, nil
			case "enter":
				if m.agentSelectedIdx >= 0 && m.agentSelectedIdx < len(m.agents) {
					m.enterSubSession(m.agentSelectedIdx)
				}
				return m, nil
			}
		}

		switch msg.String() {
		case "ctrl+c":
			if m.streamStatus != "" && m.onCancel != nil {
				m.onCancel()
				m.streamStatus = ""
				m.addMsg(MsgSystem, "已取消")
				m.addLog("用户取消", "warn")
				return m, nil
			}
			return m, tea.Quit

		case "ctrl+s":
			input := strings.TrimSpace(m.textarea.Value())
			if input == "" {
				return m, nil
			}
			if input == "/thinking" {
				m.showThinking = !m.showThinking
				for _, msg := range m.messages {
					if msg.Thinking != "" {
						msg.ThinkingFolded = !m.showThinking
					}
				}
				m.addLog("思考过程: "+map[bool]string{true: "显示", false: "隐藏"}[m.showThinking], "info")
				m.textarea.Reset()
				m.refreshChat()
				return m, nil
			}
			// 流式期间：缓存输入，不打断当前响应
			if m.streamStatus != "" {
				m.pendingInput = input
				m.textarea.Reset()
				m.addMsg(MsgSystem, "⏳ 消息已缓存 (当前响应完成后发送) ")
				m.addLog("缓存插入: "+truncStr(input, 60), "info")
				return m, nil
			}
			m.addMsg(MsgUser, input)
			m.addLog("用户: "+truncStr(input, 60), "info")
			m.textarea.Reset()
			m.streamStatus = "思考中"
			m.streamBuf.Reset()
			m.streamTokens = 0
			m.streamActive = true
			// 预创建本轮 AI 消息占位，避免 updateLastAI 覆盖上一轮内容
			m.messages = append(m.messages, &ChatMessage{Type: MsgAssistant, Content: ""})
			if m.onSubmit != nil {
				go m.onSubmit(input)
			}
			return m, nil

		case "esc":
			if m.streamStatus != "" {
				return m, nil
			}
			return m, tea.Quit

		case "f1":
			m.sidebarTab = TabPlan
			m.agentSelectedIdx = -1
			m.refreshSidebar()
			return m, nil
		case "f2":
			m.sidebarTab = TabLog
			m.agentSelectedIdx = -1
			m.refreshSidebar()
			return m, nil
		case "f3":
			m.sidebarTab = TabAgents
			m.agentSelectedIdx = -1
			m.refreshSidebar()
			return m, nil
		case "f4":
			m.sidebarTab = TabSession
			m.agentSelectedIdx = -1
			m.refreshSidebar()
			return m, nil
		case "f5":
			m.sidebarTab = TabCompanion
			m.agentSelectedIdx = -1
			m.refreshSidebar()
			return m, nil
		case "f6":
			m.sidebarTab = TabTasks
			m.agentSelectedIdx = -1
			m.refreshSidebar()
			return m, nil
		case "tab":
			tabs := []SidebarTab{TabPlan, TabLog, TabAgents, TabCompanion, TabTasks, TabSession}
			for i, t := range tabs {
				if t == m.sidebarTab {
					m.sidebarTab = tabs[(i+1)%len(tabs)]
					break
				}
			}
			m.agentSelectedIdx = -1
			m.refreshSidebar()
			return m, nil

		case "ctrl+_":
			// Ctrl+/ 或 Ctrl+?（在某些终端中映射为 Ctrl+_）
			m.addMsg(MsgSystem, m.renderHelpContent())
			return m, nil

		case "ctrl+f":
			// 搜索模式
			m.searchMode = true
			m.searchQuery = ""
			return m, nil

		case "ctrl+left":
			// 增大侧边栏宽度
			maxWidth := m.width / 2
			if m.sidebarWidth < maxWidth {
				m.sidebarWidth += 5
				m.resizeLayout()
				m.notifyWidthChange()
			}
			return m, nil

		case "ctrl+right":
			// 减小侧边栏宽度
			if m.sidebarWidth > 10 {
				m.sidebarWidth -= 5
				m.resizeLayout()
				m.notifyWidthChange()
			}
			return m, nil

		case "alt+t":
			m.showThinking = !m.showThinking
			for _, msg := range m.messages {
				if msg.Thinking != "" {
					msg.ThinkingFolded = !m.showThinking
				}
			}
			m.refreshChat()

		case "enter":
			// fall through to textarea

		// 滚动键 → 传递给 viewport
		case "up", "down", "pgup", "pgdown", "home", "end":
			// fall through to viewport
		}

	// 流式
	case StreamMsg:
		text := string(msg)
		m.streamTokens++

		// ━━━ 思考过程处理 ━━━
		if strings.HasPrefix(text, "\n🧠>") {
			thinking := strings.TrimPrefix(text, "\n🧠>")
			m.thinkingBuf.WriteString(thinking)
			m.thinkingActive = true
			m.streamStatus = "深度思考中..."
			m.refreshChat()
			break
		}

		if text == "\n🧠✓" {
			m.thinkingActive = false
			break
		}

		// 如果之前有思考过程，在第一个非思考文本到达时将其附加到当前 AI 消息
		if m.thinkingActive && !strings.HasPrefix(text, "\n") {
			m.thinkingActive = false
			if m.thinkingBuf.Len() > 0 && len(m.messages) > 0 {
				lastMsg := m.messages[len(m.messages)-1]
				if lastMsg.Type == MsgAssistant {
					lastMsg.Thinking = m.thinkingBuf.String()
					lastMsg.ThinkingFolded = false
				}
			}
			m.thinkingBuf.Reset()
		}

		// 工具执行开始标记 → 切换到工具执行状态
		if strings.HasPrefix(text, "\n⚙️") {
			toolName := strings.TrimPrefix(text, "\n⚙️")
			if toolName != "" {
				m.streamStatus = "执行工具: " + toolName
			} else {
				m.streamStatus = "执行工具"
			}
			m.refreshChat()
			break
		}

		// 继续思考标记 → 重置缓冲 + 新占位避免覆盖上一轮
		if text == "\n💭" {
			m.streamStatus = "思考中"
			if m.streamBuf.Len() > 0 {
				m.thinkingBuf.Reset()
				m.thinkingActive = false
				m.streamBuf.Reset()
				// 新轮次：预创建独立占位，保留上一轮 AI 内容
				m.messages = append(m.messages, &ChatMessage{Type: MsgAssistant, Content: ""})
			}
			m.refreshChat()
			break
		}

		// 子智能体启动标记 → 切换到等待子智能体状态
		if strings.HasPrefix(text, "\n🤖") {
			agentInfo := strings.TrimPrefix(text, "\n🤖 ")
			m.streamStatus = "等待子智能体: " + agentInfo
			m.addLog(agentInfo, "info")
			m.refreshChat()
			break
		}

		// 工具/子智能体完成标记 → 更新状态
		if strings.HasPrefix(text, "\n✅") {
			m.streamStatus = "思考中"
			m.refreshChat()
			break
		}

		// 自我纠正标记
		if strings.HasPrefix(text, "\n🔄") {
			m.streamStatus = "自我纠正中"
			m.refreshChat()
			break
		}

		// 错误/警告标记 → 记录日志但不显示在对话框
		if strings.HasPrefix(text, "\n❌") || strings.HasPrefix(text, "\n⚠️") || strings.HasPrefix(text, "\n🛡️") {
			m.addLog(strings.TrimPrefix(strings.TrimPrefix(text, "\n"), " "), "warn")
			m.refreshChat()
			break
		}

		// 普通文本 → 流式输出
		m.streamBuf.WriteString(text)
		m.updateLastAI()
		m.viewport.GotoBottom()

	case StreamDoneMsg:
		// 如果还有未处理的思考内容，附加到最后一条 AI 消息
		if m.thinkingBuf.Len() > 0 && len(m.messages) > 0 {
			lastMsg := m.messages[len(m.messages)-1]
			if lastMsg.Type == MsgAssistant {
				lastMsg.Thinking = m.thinkingBuf.String()
				lastMsg.ThinkingFolded = false
			}
		}
		m.thinkingBuf.Reset()
		m.thinkingActive = false

		m.streamStatus = ""
		m.streamActive = false
		m.addLog("AI 响应完成 ("+fmt.Sprintf("%d tokens", m.streamTokens)+")", "info")
		m.refreshChat()
		// 流式期间缓存的用户输入 → 自动发送
		if m.pendingInput != "" {
			pending := m.pendingInput
			m.pendingInput = ""
			m.addMsg(MsgUser, pending)
			m.addLog("发送缓存: "+truncStr(pending, 60), "info")
			m.textarea.Reset()
			m.streamStatus = "思考中"
			m.streamBuf.Reset()
			m.streamTokens = 0
			m.streamActive = true
			// 预创建新轮次的 AI 消息占位，避免 updateLastAI 覆盖上一轮内容
			m.messages = append(m.messages, &ChatMessage{Type: MsgAssistant, Content: ""})
			if m.onSubmit != nil {
				go m.onSubmit(pending)
			}
		}

	case ToolCallMsg:
		m.addMsg(MsgTool, string(msg))
		m.addLog(string(msg), "info")
		m.viewport.GotoBottom()

	case AddMessageMsg:
		m.addMsg(msg.Type, msg.Content)

	// 状态更新
	case StatusMsg:
		if msg.ModelName != "" {
			m.modelName = msg.ModelName
		}
		if msg.ToolCount > 0 {
			m.toolCount = msg.ToolCount
		}
		if msg.MsgCount > 0 {
			m.sessionMsgs = msg.MsgCount
		}

	// 侧边栏更新
	case UpdateTodosMsg:
		m.todos = msg.Todos
		m.refreshSidebar()
		return m, nil

	case UpdateLogMsg:
		m.logs = append(m.logs, LogEntry{Time: msg.Time, Content: msg.Content, Level: msg.Level})
		if len(m.logs) > 200 {
			m.logs = m.logs[100:]
		}
		if m.sidebarTab == TabLog {
			m.refreshSidebar()
		}
		return m, nil

	case UpdateAgentsMsg:
		m.agents = msg.Agents
		if m.agentSelectedIdx >= len(m.agents) {
			m.agentSelectedIdx = -1
		}
		// 子会话视图中 agent 状态更新时刷新内容
		if m.subSessionView && m.subSessionAgent != nil {
			for _, a := range m.agents {
				if a.ID == m.subSessionAgent.ID {
					m.refreshSubSessionContent(a)
					break
				}
			}
		}
		if m.sidebarTab == TabAgents {
			m.refreshSidebar()
		}
		return m, nil

	case UpdateCompanionMsg:
		m.companionMood = msg.Mood
		m.companionIntimacy = msg.Intimacy
		m.companionExcitement = msg.Excitement
		m.companionShyness = msg.Shyness
		m.companionFatigue = msg.Fatigue
		if m.sidebarTab == TabCompanion {
			m.refreshSidebar()
		}
		return m, nil

	case QuestionRequestMsg:
		m.EnterQuestionMode(msg.Questions, msg.ReplyCh)
		return m, nil

	case UpdateTasksMsg:
		m.scheduledTasks = msg.Tasks
		if m.sidebarTab == TabTasks {
			m.refreshSidebar()
		}
		return m, nil

	case SessionInfoMsg:
		m.workspacePath = msg.WorkspacePath
		m.pluginCount = msg.PluginCount
		m.mcpServerCount = msg.MCPServerCount
		if m.sidebarTab == TabSession {
			m.refreshSidebar()
		}
		return m, nil

	case UpdateSessionsMsg:
		m.sessions = msg.Sessions
		if m.sidebarTab == TabSession {
			m.refreshSidebar()
		}
		return m, nil

	case TickMsg:
		if m.onTick != nil {
			m.onTick()
		}
		// 定时刷新侧边栏（智能体动画）
		if m.sidebarTab == TabAgents {
			// 更新 spinner 动画
			for i := range m.agents {
				if m.agents[i].Status == "running" || m.agents[i].Status == "pending" {
					m.agents[i].Spinner = (m.agents[i].Spinner + 1) % len(SpinnerFrames)
				}
			}
			m.refreshSidebar()
		}
		return m, tickSidebar()
	}

	// 更新 textarea
	_, isKey := msg.(tea.KeyMsg)
	if !m.subSessionView && (m.streamStatus == "" || isKey) {
		m.textarea, cmd = m.textarea.Update(msg)
		cmds = append(cmds, cmd)
		// @mention 检测
		m.mentionState = CheckMention(m.textarea.Value(), m.agentList)
	}

	if m.subSessionView {
		m.subSessionVP, cmd = m.subSessionVP.Update(msg)
		cmds = append(cmds, cmd)
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
		cmds = append(cmds, cmd)
	}
	m.sidebarVP, cmd = m.sidebarVP.Update(msg)
	cmds = append(cmds, cmd)

	// @mention 激活时自动刷新
	if m.mentionState != nil && m.mentionState.Active {
		cmds = append(cmds, tickSidebar())
	}

	return m, tea.Batch(cmds...)
}

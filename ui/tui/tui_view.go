package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// View
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (m *Model) View() string {
	if !m.ready {
		return "初始化中..."
	}

	if m.subSessionView && m.subSessionAgent == nil {
		m.subSessionView = false
	}

	m.status.SetWidth(m.width)
	m.input.SetWidth(m.width)
	m.chat.SetWidth(max(1, m.width - m.sidebarWidth - 2))
	m.side.SetWidth(m.sidebarWidth)
	status := m.status.View()

	// 聊天区域
	var chatView string
	if m.subSessionView {
		// 子会话：使用 subSessionVP，尺寸与主 viewport 同步
		m.subSessionVP.Width = m.viewport.Width
		m.subSessionVP.Height = m.viewport.Height
		chatView = m.subSessionVP.View()
	} else {
		chatView = m.viewport.View()
	}

	// 侧边栏
	var sideView string
	if m.sidebarWidth > 0 {
		sideView = m.renderSidebarTabs()
		sideView += "\n"
		sideView += m.sidebarVP.View()
		// 高度对齐：用带背景色的空行填充
		chatLines := strings.Count(chatView, "\n") + 1
		sideLines := strings.Count(sideView, "\n") + 1
		bgBlank := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", m.sidebarWidth))
		if sideLines < chatLines {
			for i := sideLines; i < chatLines; i++ {
				sideView += "\n" + bgBlank
			}
		} else if chatLines < sideLines {
			chatBlank := lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", max(0, m.width-m.sidebarWidth-2)))
			for i := chatLines; i < sideLines; i++ {
				chatView += "\n" + chatBlank
			}
		}
	}

	// 水平拼接
	var content string
	if m.sidebarWidth > 0 {
		separator := separatorStyle.Render(" │ ")
		content = lipgloss.JoinHorizontal(lipgloss.Top, chatView, separator, sideView)
	} else {
		content = chatView
	}

	inputArea := m.input.View()
	if m.subSessionView {
		m.input.SetHelpText("Esc:返回  ↑/↓:滚动  F1-F6:面板  Tab:切换")
	} else {
		m.input.SetHelpText("Ctrl+S:发送  Ctrl+C:取消  F1-F5:面板  Tab:切换  Esc:退出")
	}
	help := m.input.HelpView()

	// @mention 菜单
	if m.mentionState != nil && m.mentionState.Active {
		mentionView := RenderMention(m.mentionState, 30)
		return lipgloss.JoinVertical(lipgloss.Left,
			status, content, inputArea, mentionView,
		)
	}

	// 选项框模式：替换输入区为选项界面
	if m.questionMode {
		questionView := m.renderQuestion()
		// 计算选项框占用的行数，压缩 viewport 高度防止溢出终端
		questionLines := strings.Count(questionView, "\n") + 2 // +2 for status line + margin
		// 保存原始高度
		origChatH := m.viewport.Height
		origSideH := m.sidebarVP.Height
		// 压缩 viewport 为选项框腾出空间（最小 5 行）
		newH := max(5, m.viewport.Height-questionLines)
		m.viewport.Height = newH
		m.sidebarVP.Height = newH
		// 重建 content（使用新高度重新取 viewport View）
		chatView := m.viewport.View()
		var sideView string
		if m.sidebarWidth > 0 {
			sideView = m.renderSidebarTabs()
			sideView += "\n"
			sideView += m.sidebarVP.View()
		}
		var adjContent string
		if m.sidebarWidth > 0 {
			separator := separatorStyle.Render(" │ ")
			adjContent = lipgloss.JoinHorizontal(lipgloss.Top, chatView, separator, sideView)
		} else {
			adjContent = chatView
		}
		// 恢复原始高度
		m.viewport.Height = origChatH
		m.sidebarVP.Height = origSideH

		return lipgloss.JoinVertical(lipgloss.Left,
			status,
			adjContent,
			questionView,
		)
	}

	if m.searchMode {
		searchBar := m.renderSearchBar()
		return lipgloss.JoinVertical(lipgloss.Left,
			status,
			content,
			searchBar,
			help,
		)
	}

	return lipgloss.JoinVertical(lipgloss.Left,
		status,
		content,
		inputArea,
		help,
	)
}

func (m *Model) addMsg(t MessageType, content string) {
	msg := &ChatMessage{
		Type:    t,
		Content: content,
		Folded:  false,
	}
	// 长消息自动折叠（工具输出和 AI 长回复）
	if t == MsgTool || t == MsgAssistant {
		lines := strings.Count(content, "\n") + 1
		if lines > 12 {
			msg.Folded = true
		}
	}
	m.messages = append(m.messages, msg)
	m.refreshChat()
}

func (m *Model) updateLastAI() {
	content := m.streamBuf.String()
	// 从后向前查找最后一条 MsgAssistant，避免工具消息干扰
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].Type == MsgAssistant {
			m.messages[i].Content = content
			m.refreshChat()
			return
		}
	}
	// 没有找到，创建新的
	m.messages = append(m.messages, &ChatMessage{Type: MsgAssistant, Content: content})
	m.refreshChat()
}

func (m *Model) refreshChat() {
	var sb strings.Builder
	for _, msg := range m.messages {
		sb.WriteString(msg.render(m.viewport.Width, m.isDark))
		sb.WriteString("\n")
	}
	if m.streamStatus != "" {
		spinner := SpinnerFrames[m.streamTokens%len(SpinnerFrames)]
		streamLine := fmt.Sprintf("%s %s (%d tokens)", spinner, m.streamStatus, m.streamTokens)
		sb.WriteString("\n")
		sb.WriteString(lipgloss.NewStyle().Foreground(muted).Background(bg).Render(streamLine))
		sb.WriteString("\n")
	}
	m.viewport.SetContent(sb.String())
}

func (m *Model) addLog(content string, level string) {
	m.logs = append(m.logs, LogEntry{Time: nowTime(), Content: content, Level: level})
	if len(m.logs) > 200 {
		m.logs = m.logs[100:]
	}
	if m.sidebarTab == TabLog {
		m.refreshSidebar()
	}
}

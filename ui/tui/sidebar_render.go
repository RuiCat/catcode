package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

func (m *Model) refreshSidebar() {
	var sb strings.Builder

	def, ok := m.sidebarTabs[m.sidebarTab]
	if !ok {
		if panel, pok := m.pluginPanels[m.sidebarTab]; pok {
			m.renderPluginTab(&sb, panel)
		} else {
			sb.WriteString(mutedStyle("(未知标签)"))
		}
	} else {
		def.Render(m, &sb)
	}

	m.sidebarVP.SetContent(sb.String())
	m.sidebarVP.GotoTop()
}

// enterSubSession 进入子会话视图
func (m *Model) enterSubSession(idx int) {
	m.refreshSubSessionContent(m.agents[idx])
	m.subSessionView = true
	m.subSessionVP.GotoTop()
}

// refreshSubSessionContent 刷新子会话视口内容（供 agent 状态更新时动态刷新）
func (m *Model) refreshSubSessionContent(agent AgentEntry) {
	cp := agent
	m.subSessionAgent = &cp

	var sb strings.Builder
	sb.WriteString(boldStyle(fmt.Sprintf("📋 子智能体: %s\n", agent.Name)))
	sb.WriteString(fmt.Sprintf("任务: %s\n", agent.FullTask))
	statusLine := fmt.Sprintf("状态: %s", agent.Status)
	if agent.Duration > 0 {
		statusLine += fmt.Sprintf(" · %d tools · %.1fs", agent.ToolCount, agent.Duration.Seconds())
	}
	sb.WriteString(statusLine + "\n")
	if agent.ErrorMsg != "" {
		sb.WriteString(errStyle("错误: "+agent.ErrorMsg) + "\n")
	}
	sb.WriteString(strings.Repeat("─", 60) + "\n\n")

	if agent.FullOutput != "" {
		rendered := m.mdRenderer.Render(agent.FullOutput)
		sb.WriteString(rendered)
	} else if agent.Status == "running" {
		sb.WriteString(mutedStyle("(正在执行中，请稍后刷新查看输出...)\n"))
	} else {
		sb.WriteString(mutedStyle("(暂无输出)\n"))
	}

	m.subSessionVP.SetContent(sb.String())
}

func (m *Model) renderPlanTab(sb *strings.Builder) {
	if len(m.todos) == 0 {
		sb.WriteString(mutedStyle("暂无规划任务\n\n使用 `todo` 工具创建"))
		return
	}

	// 统计进度
	total := len(m.todos)
	done := 0
	for _, t := range m.todos {
		if t.Status == "completed" || t.Status == "done" || t.Status == "cancelled" {
			done++
		}
	}
	progress := float64(done) / float64(total)

	// 进度条
	sb.WriteString(fmt.Sprintf("进度: %s\n\n", ProgressBar(int(progress), m.sidebarWidth-10)))

	for _, t := range m.todos {
		icon := sidebarItemPending
		switch t.Status {
		case "completed", "done":
			icon = sidebarItemDone
		case "in_progress", "active":
			icon = sidebarItemActive
		case "failed":
			icon = sidebarItemFailed
		case "cancelled":
			icon = sidebarItemFailed
		}
		// 截断长文本
		content := truncStr(t.Content, m.sidebarWidth-6)
		sb.WriteString(fmt.Sprintf("%s %s\n", icon, content))
	}
}

func (m *Model) renderLogTab(sb *strings.Builder) {
	if len(m.logs) == 0 {
		sb.WriteString(mutedStyle("暂无日志"))
		return
	}

	// 日志级别过滤指示
	sb.WriteString(mutedStyle("最近日志 (最多50条)"))
	sb.WriteString("\n\n")

	start := 0
	if len(m.logs) > 50 {
		start = len(m.logs) - 50
	}
	for _, l := range m.logs[start:] {
		// 根据级别着色
		levelColor := muted
		switch l.Level {
		case "warn":
			levelColor = warningC
		case "error":
			levelColor = errColor
		case "info":
			levelColor = muted
		}
		levelStyle := lipgloss.NewStyle().Foreground(levelColor).Background(bg)
		sb.WriteString(levelStyle.Render(
			fmt.Sprintf("%s %s", mutedStyle(l.Time), l.Content)))
		sb.WriteString("\n")
	}
}

func (m *Model) renderAgentsTab(sb *strings.Builder) {
	if len(m.agents) == 0 {
		sb.WriteString(mutedStyle("暂无异步子智能体"))
		return
	}

	for i, a := range m.agents {
		// 选中指示器
		if i == m.agentSelectedIdx {
			sb.WriteString(accentStyle("▸ "))
		} else {
			sb.WriteString("  ")
		}

		var statusIcon string
		var statusColor func(string) string
		var infoLine string

		switch a.Status {
		case "pending":
			statusIcon = "~"
			statusColor = mutedStyle
			infoLine = "等待中..."
		case "running":
			frame := SpinnerFrames[a.Spinner%len(SpinnerFrames)]
			statusIcon = frame
			statusColor = warningStyle
			if a.CurrentTool != "" {
				infoLine = "↳ " + a.CurrentTool
			}
		case "completed":
			statusIcon = "└"
			statusColor = mutedStyle
			infoLine = fmt.Sprintf("%d tools · %.1fs",
				a.ToolCount, a.Duration.Seconds())
		case "error":
			statusIcon = "❌"
			statusColor = errStyle
			if a.ErrorMsg != "" {
				infoLine = a.ErrorMsg
			}
		default:
			// "idle" 或其他未知状态
			statusIcon = "💤"
			statusColor = mutedStyle
		}

		sb.WriteString(fmt.Sprintf("%s %s", statusIcon, boldStyle(a.Name)))
		if a.Task != "" {
			sb.WriteString(fmt.Sprintf("\n  %s", statusColor(a.Task)))
		}
		if infoLine != "" {
			sb.WriteString(fmt.Sprintf("\n    %s", mutedStyle(infoLine)))
		}
		if i == m.agentSelectedIdx {
			sb.WriteString(fmt.Sprintf("\n  %s", mutedStyle("⏎ 查看详情")))
		}
		sb.WriteString("\n")
	}
}

func (m *Model) renderSessionTab(sb *strings.Builder) {
	sb.WriteString(boldStyle("📋 工作区") + "\n")
	sb.WriteString(fmt.Sprintf("路径: %s\n", m.workspacePath))
	sb.WriteString(fmt.Sprintf("模型: %s\n", m.modelName))
	sb.WriteString(fmt.Sprintf("消息: %d 条 | 工具: %d | 角色: %d\n\n", m.sessionMsgs, m.toolCount, m.roleCount))

	sb.WriteString(boldStyle("🤖 智能体") + "\n")
	for _, a := range m.agentList {
		desc := a.Description
		if desc == "" {
			desc = "(无描述)"
		}
		sb.WriteString(fmt.Sprintf("  • %s\n", a.Name))
		sb.WriteString(wrapText("    "+desc, m.sidebarWidth-35) + "\n")
	}
	sb.WriteString(fmt.Sprintf("\n共 %d 个智能体\n\n", len(m.agentList)))

	sb.WriteString(boldStyle("🔌 扩展") + "\n")
	sb.WriteString(fmt.Sprintf("插件: %d 个\n", m.pluginCount))
	sb.WriteString(fmt.Sprintf("MCP:  %d 个\n\n", m.mcpServerCount))

	if len(m.sessions) == 0 {
		sb.WriteString(mutedStyle("按 Ctrl+S 保存会话"))
		return
	}

	sb.WriteString(boldStyle("💾 历史会话") + "\n")

	sb.WriteString(mutedStyle("历史会话"))
	sb.WriteString("\n\n")
	for _, s := range m.sessions {
		icon := "💾"
		if s.IsActive {
			icon = "▶"
		}
		sb.WriteString(fmt.Sprintf("%s %s (%d条)\n",
			icon, truncStr(s.Model, 15), s.MessageCount))
	}
}

// renderTasksTab 渲染周期任务面板
func (m *Model) renderTasksTab(sb *strings.Builder) {
	sb.WriteString("  ⏰ 周期任务\n")
	sb.WriteString("  " + strings.Repeat("─", m.sidebarWidth-4) + "\n\n")
	if len(m.scheduledTasks) == 0 {
		sb.WriteString("  📭 暂无周期任务\n")
		sb.WriteString("  使用 schedule_create \n")
		sb.WriteString("  工具创建定时任务")
		return
	}
	for _, t := range m.scheduledTasks {
		status := "✅"
		if !t.Enabled {
			status = "⏸"
		}
		sb.WriteString(fmt.Sprintf("  %s #%d %s\n", status, t.ID, truncStr(t.Name, m.sidebarWidth-10)))
		if t.Description != "" {
			sb.WriteString(fmt.Sprintf("     %s\n", t.Description))
		}
		sb.WriteString(fmt.Sprintf("     ⏱ %ds\n\n", t.IntervalSeconds))
	}
}

// renderCompanionTab 渲染猫猫状态面板
func (m *Model) renderCompanionTab(sb *strings.Builder) {
	sb.WriteString("  🐱 猫猫状态\n")
	sb.WriteString("  " + strings.Repeat("─", m.sidebarWidth-4) + "\n\n")

	// 心情
	moodEmoji := map[string]string{
		"happy": "😸", "neutral": "😺", "shy": "😳", "tsundere": "😤", "sleepy": "😴",
	}
	emoji := moodEmoji[m.companionMood]
	if emoji == "" {
		emoji = "😺"
	}
	if m.companionMood != "" {
		sb.WriteString(fmt.Sprintf("  %s 心情: %s\n\n", emoji, m.companionMood))
	} else {
		sb.WriteString(fmt.Sprintf("  %s 心情: 未知\n\n", emoji))
	}

	// 状态条
	drawBar := func(label string, val, max int) string {
		barW := m.sidebarWidth - 14
		if barW < 5 {
			barW = 5
		}
		filled := val * barW / max
		if filled < 0 {
			filled = 0
		}
		if filled > barW {
			filled = barW
		}
		return fmt.Sprintf("  %-6s %s%s %d/%d",
			label,
			strings.Repeat("█", filled),
			strings.Repeat("░", barW-filled),
			val, max)
	}

	sb.WriteString(drawBar("亲密度", m.companionIntimacy, 100) + "\n")
	sb.WriteString(drawBar("兴奋度", m.companionExcitement, 100) + "\n")
	sb.WriteString(drawBar("害羞度", m.companionShyness, 100) + "\n")
	sb.WriteString(drawBar("疲劳度", m.companionFatigue, 100) + "\n")

	if m.companionMood == "" {
		sb.WriteString("\n\n  💤 猫猫还没被叫醒喵~\n")
		sb.WriteString("  使用 companion_talk 工具\n")
		sb.WriteString("  和猫猫互动吧！")
	}
}

func (m *Model) renderPluginTab(sb *strings.Builder, panel PluginPanel) {
	sb.WriteString(boldStyle(fmt.Sprintf("%s\n", panel.Title)))
	sb.WriteString(strings.Repeat("─", m.sidebarWidth-2) + "\n\n")
	if panel.Content == "" {
		sb.WriteString(mutedStyle("(插件面板暂无内容)\n"))
	} else {
		rendered := m.mdRenderer.Render(panel.Content)
		sb.WriteString(rendered)
	}
}

func (m *Model) renderPluginTabByKey(sb *strings.Builder) {
	panel, ok := m.pluginPanels[m.sidebarTab]
	if !ok {
		sb.WriteString(mutedStyle("(插件面板未注册)"))
		return
	}
	sb.WriteString(boldStyle(fmt.Sprintf("%s\n", panel.Title)))
	sb.WriteString(strings.Repeat("─", m.sidebarWidth-2) + "\n\n")
	if panel.Content == "" {
		sb.WriteString(mutedStyle("(等待插件更新...)\n"))
	} else {
		rendered := m.mdRenderer.Render(panel.Content)
		sb.WriteString(rendered)
	}
}

// registerBuiltinTabs 注册所有内置侧边栏标签
func (m *Model) registerBuiltinTabs() {
	m.sidebarTabs = make(map[string]*TabDef)
	m.tabOrder = make([]string, 0, 6)

	tabs := []struct {
		key      TabKey
		title    string
		shortcut string
		render   func(m *Model, sb *strings.Builder)
	}{
		{TabPlan, "📋 规划", "F1", (*Model).renderPlanTab},
		{TabLog, "📜 日志", "F2", (*Model).renderLogTab},
		{TabAgents, "🤖 智能体", "F3", (*Model).renderAgentsTab},
		{TabCompanion, "🐱 猫猫", "F5", (*Model).renderCompanionTab},
		{TabTasks, "⏰ 任务", "F6", (*Model).renderTasksTab},
		{TabSession, "💾 会话", "F4", (*Model).renderSessionTab},
	}

	for _, t := range tabs {
		m.sidebarTabs[t.key] = &TabDef{
			Key:      t.key,
			Title:    t.title,
			Shortcut: t.shortcut,
			Builtin:  true,
			Render:   t.render,
		}
		m.tabOrder = append(m.tabOrder, t.key)
	}
}

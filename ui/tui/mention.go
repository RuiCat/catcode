package tui

import "strings"

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// @mention 子智能体 autocomplete（从 DB 配置获取描述）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// AgentInfo 智能体简要信息（用于 autocomplete）
type AgentInfo struct {
	Name        string
	Description string
}

// MentionState @mention 自动补全状态
type MentionState struct {
	Active   bool        // 是否正在显示菜单
	Query    string      // 用户输入的 @ 后文本
	Agents   []AgentInfo // 匹配的 agent 列表
	Selected int         // 当前选中索引
}

// CheckMention 检测 textarea 内容是否触发了 @mention
func CheckMention(text string, agents []AgentInfo) *MentionState {
	// 查找最后一个 @ 符号（行首或空格后）
	idx := strings.LastIndex(text, "@")
	if idx < 0 {
		return nil
	}
	if idx > 0 && text[idx-1] != ' ' && text[idx-1] != '\n' {
		return nil
	}
	query := strings.ToLower(text[idx+1:])
	if strings.Contains(query, " ") {
		return nil
	}

	var matched []AgentInfo
	for _, a := range agents {
		if strings.HasPrefix(strings.ToLower(a.Name), query) {
			matched = append(matched, a)
		}
	}
	if len(matched) == 0 {
		return nil
	}

	return &MentionState{
		Active:   true,
		Query:    query,
		Agents:   matched,
		Selected: 0,
	}
}

// RenderMention 渲染 @mention 自动补全弹出菜单
func RenderMention(ms *MentionState, maxWidth int) string {
	if ms == nil || !ms.Active || len(ms.Agents) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n  ┌─ @子智能体 ──────────────────┐\n")
	for i, a := range ms.Agents {
		cursor := "  "
		if i == ms.Selected {
			cursor = "▶ "
		}
		desc := a.Description
		sb.WriteString("  │ ")
		sb.WriteString(cursor)
		sb.WriteString(a.Name)
		if desc != "" {
			pad := maxWidth - len(a.Name) - len(cursor) - 10
			if pad < 2 {
				pad = 2
			}
			sb.WriteString(strings.Repeat(" ", pad))
			sb.WriteString(desc)
		}
		sb.WriteString(" │\n")
	}
	sb.WriteString("  └────────────────────────────────┘")
	return sb.String()
}

// HandleMentionKey 处理 @mention 菜单的键盘事件
func (m *Model) HandleMentionKey(key string) bool {
	if !m.mentionState.Active {
		return false
	}
	switch key {
	case "up", "k":
		if m.mentionState.Selected > 0 {
			m.mentionState.Selected--
		}
		return true
	case "down", "j":
		if m.mentionState.Selected < len(m.mentionState.Agents)-1 {
			m.mentionState.Selected++
		}
		return true
	case "enter", "tab":
		m.replaceMention()
		return true
	case "esc":
		m.mentionState.Active = false
		return true
	}
	return false
}

func (m *Model) replaceMention() {
	if !m.mentionState.Active || len(m.mentionState.Agents) == 0 {
		return
	}
	agent := m.mentionState.Agents[m.mentionState.Selected].Name
	text := m.textarea.Value()
	idx := strings.LastIndex(text, "@")
	if idx < 0 {
		return
	}
	newText := text[:idx] + "@" + agent + " "
	m.textarea.SetValue(newText)
	m.textarea.CursorEnd()
	m.mentionState.Active = false
}

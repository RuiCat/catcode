package tui

import (
	"fmt"
	"strings"

	"catcode/tool"

	"github.com/charmbracelet/lipgloss"
)

// questionPanelStyle 选项框面板样式（限制最大宽度防止撑破布局）
var questionPanelStyle = lipgloss.NewStyle().
	MaxWidth(80).
	MarginTop(1)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 选项框模式
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// EnterQuestionMode 进入选项框模式
func (m *Model) EnterQuestionMode(questions []tool.QuestionInfo, replyCh chan tool.QuestionAnswer) {
	// 如果已在 question 模式中，排队等待
	if m.questionMode {
		m.questionPending = append(m.questionPending, QuestionRequestMsg{
			Questions: questions,
			ReplyCh:   replyCh,
		})
		return
	}
	m.questionMode = true
	m.questionQuestions = questions
	m.questionAnswers = make([][]string, len(questions))
	m.questionSelected = 0
	m.questionTab = 0
	m.questionReply = replyCh
	m.refreshSidebar()
	// 侧边栏已在 refreshSidebar 中通过 GotoTop 重置，viewport 保持当前滚动位置不变
}

// QuestionInfo 问题信息（从 tool 包导入）
type QuestionInfo = tool.QuestionInfo

// QuestionOption 选项
type QuestionOption = tool.QuestionOption

// QuestionAnswer 用户回答
type QuestionAnswer = tool.QuestionAnswer

// renderQuestion 渲染选项框界面
func (m *Model) renderQuestion() string {
	if !m.questionMode || len(m.questionQuestions) == 0 {
		return ""
	}

	q := m.questionQuestions[m.questionTab]
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  ❓ %s\n", q.Question))
	if q.Multiple {
		sb.WriteString("  [空格]选择  [Enter]确认  [Tab]下一题\n\n")
	} else {
		sb.WriteString("  [↑↓]选择  [Enter]确认\n\n")
	}

	selected := m.questionAnswers[m.questionTab]
	if selected == nil {
		selected = []string{}
	}

	for i, opt := range q.Options {
		marker := "  "
		cursor := " "
		if i == m.questionSelected {
			cursor = "▶"
		}
		checked := "○"
		for _, s := range selected {
			if s == opt.Label {
				checked = "●"
				break
			}
		}
		if q.Multiple {
			sb.WriteString(fmt.Sprintf("%s %s %s  %s\n", cursor, marker, checked, opt.Label))
		} else {
			sb.WriteString(fmt.Sprintf("%s %s    %s\n", cursor, marker, opt.Label))
		}
		if opt.Description != "" {
			sb.WriteString(fmt.Sprintf("         %s\n", mutedStyle(opt.Description)))
		}
	}

	// 进度
	sb.WriteString(fmt.Sprintf("\n  问题 %d/%d", m.questionTab+1, len(m.questionQuestions)))

	return questionPanelStyle.Render(sb.String())
}

// handleQuestionKey 处理选项框键盘事件，返回 true 表示已处理
func (m *Model) handleQuestionKey(key string) bool {
	if !m.questionMode || len(m.questionQuestions) == 0 {
		return false
	}

	q := m.questionQuestions[m.questionTab]

	switch key {
	case "up", "k":
		if m.questionSelected > 0 {
			m.questionSelected--
		}
		return true

	case "down", "j":
		if m.questionSelected < len(q.Options)-1 {
			m.questionSelected++
		}
		return true

	case " ":
		if q.Multiple {
			label := q.Options[m.questionSelected].Label
			answers := m.questionAnswers[m.questionTab]
			found := false
			for i, a := range answers {
				if a == label {
					answers = append(answers[:i], answers[i+1:]...)
					found = true
					break
				}
			}
			if !found {
				answers = append(answers, label)
			}
			m.questionAnswers[m.questionTab] = answers
		}
		return true

	case "enter":
		if !q.Multiple {
			// 单选：直接选中当前
			m.questionAnswers[m.questionTab] = []string{q.Options[m.questionSelected].Label}
		}
		if m.questionTab < len(m.questionQuestions)-1 {
			m.questionTab++
			m.questionSelected = 0
		} else {
			// 最后一题，提交
			m.submitQuestionAnswers()
		}
		return true

	case "tab":
		if m.questionTab < len(m.questionQuestions)-1 {
			m.questionTab++
			m.questionSelected = 0
		}
		return true

	case "esc":
		// 取消：返回空答案
		m.questionAnswers = make([][]string, len(m.questionQuestions))
		m.submitQuestionAnswers()
		return true

	case "pgup", "pgdown", "home", "end":
		// 在 question 模式下忽略滚动键，防止 viewport 意外滚动导致内容消失
		return true
	}

	return false
}

// submitQuestionAnswers 提交答案并退出选项模式
func (m *Model) submitQuestionAnswers() {
	if m.questionReply != nil {
		m.questionReply <- tool.QuestionAnswer{Answers: m.questionAnswers}
	}
	m.questionMode = false
	m.questionQuestions = nil
	m.questionAnswers = nil
	m.questionReply = nil
	m.refreshSidebar()
	// 侧边栏由 refreshSidebar 中的 GotoTop 重置

	// 处理排队的下一个 question
	if len(m.questionPending) > 0 {
		next := m.questionPending[0]
		m.questionPending = m.questionPending[1:]
		m.EnterQuestionMode(next.Questions, next.ReplyCh)
	}
}

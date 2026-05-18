package component

// Deprecated: QuestionComponent 使用的 component.QuestionInfo 类型与主 TUI 代码
// 实际使用的 tool.QuestionInfo 类型不兼容（Header vs Question, Options []string vs []QuestionOption）。
// 当前主 TUI 在 ui/tui/question.go 中直接处理问题，不使用此组件。
// 此组件保留作为参考，待后续统一类型系统后重新启用。

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// QuestionComponent 选项框组件，实现 DialogComponent 和 QuestionDisplay
type QuestionComponent struct {
	BaseComponent

	active    bool
	questions []QuestionInfo
	answers   [][]string
	selected  int // 当前选项索引
	tab       int // 当前问题索引
	resultCh  chan interface{}
}

// NewQuestionComponent 创建新的选项框组件
func NewQuestionComponent() *QuestionComponent {
	return &QuestionComponent{
		BaseComponent: NewBaseComponent("question"),
		resultCh:      make(chan interface{}, 1),
	}
}

// Activate 激活选项框，设置问题列表
func (q *QuestionComponent) Activate(questions []QuestionInfo) {
	q.questions = questions
	q.answers = make([][]string, len(questions))
	q.selected = 0
	q.tab = 0
	q.active = true
	q.SetVisible(true)
}

// IsActive 返回是否激活
func (q *QuestionComponent) IsActive() bool {
	return q.active
}

// SetReply 设置回复（可在外部预设答案）
func (q *QuestionComponent) SetReply(reply interface{}) {
	// no-op for now, answers are set via Update
}

// Result 返回结果 channel（DialogComponent 接口）
func (q *QuestionComponent) Result() <-chan interface{} {
	return q.resultCh
}

// Update 处理消息（键盘事件）
func (q *QuestionComponent) Update(msg tea.Msg) (Component, tea.Cmd) {
	if !q.active || len(q.questions) == 0 {
		return q, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return q, q.handleKey(msg.String())
	}
	return q, nil
}

// handleKey 处理键盘输入，返回 tea.Cmd
func (q *QuestionComponent) handleKey(key string) tea.Cmd {
	if len(q.questions) == 0 {
		return nil
	}

	question := q.questions[q.tab]

	switch key {
	case "up", "k":
		if q.selected > 0 {
			q.selected--
		}
	case "down", "j":
		if q.selected < len(question.Options)-1 {
			q.selected++
		}
	case " ":
		// 切换当前选项（多选标记）
		if len(question.Options) > 0 {
			label := question.Options[q.selected]
			answers := q.answers[q.tab]
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
			q.answers[q.tab] = answers
		}
	case "enter":
		// 如果有空格选中的答案则保留，否则单选当前高亮项
		if len(q.answers[q.tab]) == 0 {
			if len(question.Options) > 0 {
				q.answers[q.tab] = []string{question.Options[q.selected]}
			}
		}
		if q.tab < len(q.questions)-1 {
			q.tab++
			q.selected = 0
		} else {
			return q.submit()
		}
	case "tab":
		if q.tab < len(q.questions)-1 {
			q.tab++
			q.selected = 0
		}
	case "esc":
		// 取消：返回空答案
		q.answers = make([][]string, len(q.questions))
		return q.submit()
	}
	return nil
}

// submit 提交答案并退出
func (q *QuestionComponent) submit() tea.Cmd {
	q.active = false
	q.SetVisible(false)

	// 发送结果到 channel（非阻塞）
	select {
	case q.resultCh <- q.answers:
	default:
	}
	return nil
}

// View 渲染选项框
func (q *QuestionComponent) View() string {
	if !q.active || len(q.questions) == 0 {
		return ""
	}

	question := q.questions[q.tab]
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("  ❓ %s\n", question.Header))
	sb.WriteString("  [↑↓]选择  [空格]多选  [Enter]确认  [Tab]下一题  [Esc]取消\n\n")

	for i, opt := range question.Options {
		cursor := " "
		marker := "  "
		if i == q.selected {
			cursor = "▶"
		}

		checked := "○"
		for _, s := range q.answers[q.tab] {
			if s == opt {
				checked = "●"
				break
			}
		}

		sb.WriteString(fmt.Sprintf("%s %s %s  %s\n", cursor, marker, checked, opt))
	}

	sb.WriteString(fmt.Sprintf("\n  问题 %d/%d", q.tab+1, len(q.questions)))

	return sb.String()
}

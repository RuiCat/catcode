package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 代码块渲染 + 折叠
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// renderCodeBlock 渲染代码块（带语法高亮和行号）
func (r *MarkdownRenderer) renderCodeBlock(lang string, lines []string) string {
	if len(lines) == 0 {
		return ""
	}

	var sb strings.Builder

	// 代码块头部 — 显示语言
	headerText := lang
	if headerText == "" {
		headerText = "code"
	}
	headerStyle := lipgloss.NewStyle().
		Foreground(r.codeStyle.HeaderFg).
		Background(r.codeStyle.HeaderBg).
		Padding(0, 2).
		Width(r.width)
	sb.WriteString(headerStyle.Render(fmt.Sprintf(" %s ", headerText)))
	sb.WriteString("\n")

	// 代码块主体 — 设置宽度使背景色填满整行
	bodyStyle := lipgloss.NewStyle().
		Foreground(r.codeStyle.BodyFg).
		Background(r.codeStyle.BodyBg).
		Padding(0, 1).
		Width(r.width)

	// 行号宽度
	lineNumWidth := 3
	if len(lines) >= 100 {
		lineNumWidth = 4
	}

	// 设置当前代码块背景色，使高亮词保持背景一致
	r.codeBg = r.codeStyle.BodyBg

	for i, line := range lines {
		// 行号
		lineNum := ""
		if r.showLineNum {
			lineNum = lipgloss.NewStyle().
				Foreground(r.codeStyle.LineNumFg).
				Background(r.codeStyle.BodyBg).
				Width(lineNumWidth).
				Align(lipgloss.Right).
				Render(fmt.Sprintf("%d", i+1))
		}

		// 语法高亮
		highlighted := r.highlightLine(line, lang)

		sb.WriteString(bodyStyle.Render(lineNum + " " + highlighted))
		sb.WriteString("\n")
	}

	return sb.String()
}

// highlightLine 对单行代码进行语法高亮
func (r *MarkdownRenderer) highlightLine(line, lang string) string {
	if line == "" {
		return ""
	}

	// 根据语言选择高亮规则
	switch strings.ToLower(lang) {
	case "go", "golang":
		return r.highlightGo(line)
	case "python", "py":
		return r.highlightPython(line)
	case "javascript", "js", "typescript", "ts", "tsx", "jsx":
		return r.highlightJS(line)
	case "rust", "rs":
		return r.highlightRust(line)
	case "json", "yaml", "yml", "toml", "xml", "html", "css":
		return r.highlightData(line)
	case "bash", "sh", "shell", "zsh":
		return r.highlightBash(line)
	case "diff":
		return r.highlightDiff(line)
	default:
		return r.highlightGeneric(line)
	}
}

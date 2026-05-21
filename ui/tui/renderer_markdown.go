package tui

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Markdown 渲染
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// renderInline 渲染行内 Markdown 元素
func (r *MarkdownRenderer) renderInline(line string) string {
	if line == "" {
		return ""
	}

	// 标题
	if strings.HasPrefix(line, "# ") {
		return r.renderHeading(line[2:], 1)
	}
	if strings.HasPrefix(line, "## ") {
		return r.renderHeading(line[3:], 2)
	}
	if strings.HasPrefix(line, "### ") {
		return r.renderHeading(line[4:], 3)
	}
	if strings.HasPrefix(line, "#### ") {
		return r.renderHeading(line[5:], 4)
	}

	// 无序列表
	if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
		content := line[2:]
		return fmt.Sprintf("  • %s", r.renderInlineCode(content))
	}

	// 有序列表
	if matched, _ := regexp.MatchString(`^\d+\.\s`, line); matched {
		content := line[strings.Index(line, " ")+1:]
		return fmt.Sprintf("  %s", r.renderInlineCode(content))
	}

	// 引用
	if strings.HasPrefix(line, "> ") {
		content := line[2:]
		return r.renderQuote(content)
	}

	// 分隔线
	if matched, _ := regexp.MatchString(`^[-*_]{3,}$`, strings.TrimSpace(line)); matched {
		return r.renderHR()
	}

	// 普通文本 — 渲染行内代码和粗体/斜体，并自动换行
	rendered := r.renderInlineCode(line)
	if r.width > 0 {
		rendered = lipgloss.NewStyle().Width(r.width).Render(rendered)
	}
	return rendered
}

// renderHeading 渲染标题
func (r *MarkdownRenderer) renderHeading(text string, level int) string {
	style := lipgloss.NewStyle().Bold(true).Background(r.bgColor)
	switch level {
	case 1:
		style = style.
			Foreground(lipgloss.Color("#FF6B35")).
			Underline(true).
			Padding(0, 0)
		if r.width > 0 {
			style = style.Width(r.width)
		}
		return style.Render(text)
	case 2:
		style = style.
			Foreground(lipgloss.Color("#FF6B35"))
		if r.width > 0 {
			style = style.Width(r.width)
		}
		return style.Render(text)
	case 3:
		style = style.
			Foreground(lipgloss.Color("#4A90D9"))
		if r.width > 0 {
			style = style.Width(r.width)
		}
		return style.Render(text)
	default:
		style = style.
			Foreground(lipgloss.Color("#4A90D9"))
		if r.width > 0 {
			style = style.Width(r.width)
		}
		return style.Render(text)
	}
}

// renderInlineCode 渲染行内代码、粗体、斜体
func (r *MarkdownRenderer) renderInlineCode(text string) string {
	// 行内代码背景色 — 根据主题使用合适的颜色
	inlineCodeBg := lipgloss.Color("#1a1b26") // 暗色主题默认
	if !r.isDark {
		inlineCodeBg = lipgloss.Color("#E8E8E8") // 亮色主题使用浅灰
	}

	// 行内代码 `code`
	codeRe := regexp.MustCompile("`([^`]+)`")
	text = codeRe.ReplaceAllStringFunc(text, func(match string) string {
		code := match[1 : len(match)-1]
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF7B72")).
			Background(inlineCodeBg).
			Render(code)
	})

	// 粗体 **text**
	boldRe := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = boldRe.ReplaceAllStringFunc(text, func(match string) string {
		content := match[2 : len(match)-2]
		return lipgloss.NewStyle().Bold(true).Render(content)
	})

	// 斜体 *text*
	italicRe := regexp.MustCompile(`\*([^*]+)\*`)
	text = italicRe.ReplaceAllStringFunc(text, func(match string) string {
		content := match[1 : len(match)-1]
		return lipgloss.NewStyle().Italic(true).Render(content)
	})

	return text
}

// renderQuote 渲染引用块
func (r *MarkdownRenderer) renderQuote(text string) string {
	// 使用 ▎ 字符模拟引用线
	quotePrefix := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#30363D")).
		Background(r.bgColor).
		Render("▎")
	content := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#8B949E")).
		Background(r.bgColor)
	if r.width > 0 {
		content = content.Width(r.width - 2)
	}
	return quotePrefix + " " + content.Render(text)
}

// renderHR 渲染分隔线
func (r *MarkdownRenderer) renderHR() string {
	width := r.width
	if width <= 0 || width > 80 {
		width = 60
	}
	return strings.Repeat("─", width)
}

// Package tui — 消息渲染器
// 提供 Markdown 渲染、代码语法高亮、消息折叠等功能
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Markdown 渲染器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MarkdownRenderer Markdown → 终端渲染
type MarkdownRenderer struct {
	width       int
	codeStyle   CodeBlockStyle
	syntaxTheme SyntaxTheme
	showLineNum bool
	isDark      bool
	codeBg      lipgloss.Color // 当前代码块背景色，用于高亮词保持背景一致
	bgColor     lipgloss.Color // 全局背景色，统一所有块级元素背景
}

// NewMarkdownRenderer 创建 Markdown 渲染器
func NewMarkdownRenderer(width int, isDark bool) *MarkdownRenderer {
	r := &MarkdownRenderer{
		width:       width,
		showLineNum: true,
		isDark:      isDark,
	}
	if isDark {
		r.codeStyle = codeBlockDark
		r.syntaxTheme = DarkSyntax
		r.bgColor = lipgloss.Color("#1a1b26")
	} else {
		r.codeStyle = codeBlockLight
		r.syntaxTheme = LightSyntax
		r.bgColor = lipgloss.Color("#FFFFFF")
	}
	return r
}

// Render 渲染 Markdown 文本为终端格式
func (r *MarkdownRenderer) Render(text string) string {
	if text == "" {
		return ""
	}

	lines := strings.Split(text, "\n")
	var result strings.Builder
	inCodeBlock := false
	var codeLang string
	var codeLines []string

	flushCodeBlock := func() {
		if len(codeLines) > 0 {
			result.WriteString(r.renderCodeBlock(codeLang, codeLines))
			codeLines = nil
			codeLang = ""
		}
	}

	for _, line := range lines {
		// 检测代码块开始/结束
		if strings.HasPrefix(line, "```") {
			if inCodeBlock {
				// 结束代码块
				flushCodeBlock()
				inCodeBlock = false
			} else {
				// 开始代码块
				flushCodeBlock()
				inCodeBlock = true
				codeLang = strings.TrimSpace(strings.TrimPrefix(line, "```"))
			}
			continue
		}

		if inCodeBlock {
			codeLines = append(codeLines, line)
			continue
		}

		// 渲染行内元素
		rendered := r.renderInline(line)
		result.WriteString(rendered)
		result.WriteString("\n")
	}

	// 处理未闭合的代码块
	if inCodeBlock && len(codeLines) > 0 {
		result.WriteString(r.renderCodeBlock(codeLang, codeLines))
	}

	return result.String()
}

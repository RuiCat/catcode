package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

var (
	primary   = lipgloss.Color("#FF6B35")
	secondary = lipgloss.Color("#4A90D9")
	success   = lipgloss.Color("#50C878")
	warningC  = lipgloss.Color("#FFD700")
	errColor  = lipgloss.Color("#FF4444")
	muted     = lipgloss.Color("#888888")
	bg        = lipgloss.Color("#1a1b26")
	fg        = lipgloss.Color("#c0caf5")
	panelBg   = lipgloss.Color("#24283b")
	borderFg  = lipgloss.Color("#414868")
	accent    = lipgloss.Color("#58A6FF")

	// 标题样式
	titleStyle = lipgloss.NewStyle().
			Foreground(primary).
			Background(bg).
			Bold(true).
			Padding(0, 1)

	// 状态栏
	statusStyle = lipgloss.NewStyle().
			Background(bg).
			Foreground(muted).
			Padding(0, 1)

	// 面板
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderFg).
			Background(bg).
			Padding(0, 1)

	panelActiveStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primary).
				Background(bg).
				Padding(0, 1)

	// 聊天消息样式
	userBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.Border{Left: "│"}, false, false, false, true).
			BorderForeground(secondary).
			PaddingLeft(1).
			Background(bg)

	aiContentStyle = lipgloss.NewStyle().
			Foreground(fg).
			Background(bg).
			PaddingLeft(2)

	toolContentStyle = lipgloss.NewStyle().
				Foreground(success).
				Background(bg).
				PaddingLeft(2).
				Italic(true)

	errContentStyle = lipgloss.NewStyle().
			Foreground(errColor).
			Background(bg).
			PaddingLeft(2)

	sysContentStyle = lipgloss.NewStyle().
			Foreground(muted).
			Background(bg).
			PaddingLeft(2).
			Italic(true)

	// 消息头部
	userHeaderStyle = lipgloss.NewStyle().
			Foreground(secondary).
			Bold(true).
			Background(bg).
			Padding(0, 1)

	aiHeaderStyle = lipgloss.NewStyle().
			Foreground(primary).
			Bold(true).
			Background(bg).
			Padding(0, 1)

	toolHeaderStyle = lipgloss.NewStyle().
			Foreground(success).
			Bold(true).
			Background(bg).
			Padding(0, 1)

	errHeaderStyle = lipgloss.NewStyle().
			Foreground(errColor).
			Bold(true).
			Background(bg).
			Padding(0, 1)

	sysHeaderStyle = lipgloss.NewStyle().
			Foreground(muted).
			Italic(true).
			Background(bg).
			Padding(0, 1)

	// 侧边栏
	sidebarTitleStyle = lipgloss.NewStyle().
				Foreground(primary).
				Bold(true).
				Padding(0, 1).
				Background(bg)

	sidebarItemDone    = lipgloss.NewStyle().Foreground(success).Render("✅")
	sidebarItemActive  = lipgloss.NewStyle().Foreground(warningC).Render("🔄")
	sidebarItemPending = lipgloss.NewStyle().Foreground(muted).Render("⬜")
	sidebarItemFailed  = lipgloss.NewStyle().Foreground(errColor).Render("❌")

	sidebarLog = lipgloss.NewStyle().Foreground(muted).Background(bg)

	// Tab 样式（激活时使用反色背景突出显示）
	tabActiveStyle = lipgloss.NewStyle().
			Foreground(bg).
			Background(primary).
			Padding(0, 2)

	tabInactiveStyle = lipgloss.NewStyle().
				Foreground(muted).
				Background(bg).
				Padding(0, 2)

	// 输入框
	inputStyle = lipgloss.NewStyle().
			Background(bg).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(borderFg).
			Padding(0, 1)

	inputFocusedStyle = lipgloss.NewStyle().
				Background(bg).
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primary).
				Padding(0, 1)

	// 帮助栏
	helpStyle = lipgloss.NewStyle().Foreground(muted).Background(bg).Padding(0, 1)

	// 进度条
	progressBarStyle = lipgloss.NewStyle().Foreground(success).Background(bg)
	progressBgStyle  = lipgloss.NewStyle().Foreground(borderFg).Background(bg)

	// 分隔符
	separatorStyle = lipgloss.NewStyle().Foreground(borderFg).Background(bg)

	// 思考过程样式
	thinkingFoldedStyle = lipgloss.NewStyle().
				Foreground(muted).
				Background(lipgloss.Color("#1a1a2e")).
				Padding(0, 1).
				Italic(true)

	thinkingTextStyle = lipgloss.NewStyle().
				Foreground(muted).
				Italic(true)

	thinkingBorderStyle = lipgloss.NewStyle().
				BorderLeft(true).
				BorderStyle(lipgloss.NormalBorder()).
				BorderForeground(lipgloss.Color("#444")).
				PaddingLeft(1).
				MarginTop(0).
				MarginBottom(0)
)

// SpinnerFrames 动画帧
var SpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// ProgressBar 渲染进度条
func ProgressBar(percent int, width int) string {
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	if width < 10 {
		width = 10
	}
	barW := width - 8
	filled := barW * percent / 100
	empty := barW - filled
	return fmt.Sprintf("[%s%s] %3d%%",
		strings.Repeat("█", filled),
		strings.Repeat("░", empty),
		percent)
}

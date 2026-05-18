package component

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

type StatusBarComponent struct {
	BaseComponent
	ModelName    string
	ToolCount    int
	RoleCount    int
	SessionMsgs  int
	StreamStatus string
	width        int
}

func NewStatusBar() *StatusBarComponent {
	return &StatusBarComponent{BaseComponent: NewBaseComponent("status")}
}

func (s *StatusBarComponent) Update(msg tea.Msg) (Component, tea.Cmd) { return s, nil }
func (s *StatusBarComponent) SetBounds(r Rect) { s.BaseComponent.SetBounds(r); s.width = r.Width }
func (s *StatusBarComponent) View() string {
	style := lipgloss.NewStyle().Width(s.width).
		Background(lipgloss.Color("#222")).Foreground(lipgloss.Color("#aaa"))
	left := fmt.Sprintf("🐱 catcode | %s | %d工具", s.ModelName, s.ToolCount)
	right := fmt.Sprintf("%d条消息", s.SessionMsgs)
	if s.StreamStatus != "" { right = s.StreamStatus + " | " + right }
	pad := s.width - len(left) - len(right) - 2
	if pad < 1 { pad = 1 }
	return style.Render(left + strings.Repeat(" ", pad) + right)
}

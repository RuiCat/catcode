package tui

import (
	"fmt"
	"github.com/charmbracelet/lipgloss"
	"strings"
)

func (m *Model) renderStatus() string {
	left := fmt.Sprintf("🐱 catcode | %s | %d工具 %d角色",
		m.modelName, m.toolCount, m.roleCount)
	left += fmt.Sprintf(" | %d条消息", m.sessionMsgs)
	if m.subSessionView && m.subSessionAgent != nil {
		left += fmt.Sprintf(" | 📋 %s", m.subSessionAgent.Name)
	}
	return statusStyle.Width(m.width).Render(left)
}

// renderSidebarTabs 渲染侧边栏顶部的 Tab 切换栏
func (m *Model) renderSidebarTabs() string {
	var sb strings.Builder
	first := true
	for _, key := range m.tabOrder {
		def, ok := m.sidebarTabs[key]
		if !ok {
			continue
		}
		if !first {
			sb.WriteString(" ")
		}
		first = false
		if key == m.sidebarTab {
			sb.WriteString(tabActiveStyle.Render(def.Title))
		} else {
			sb.WriteString(tabInactiveStyle.Render(def.Title))
		}
	}
	return sb.String()
}

func (m *Model) renderSearchBar() string {
	searchStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Background(bg).
		Padding(0, 1).
		Width(m.width - 2)

	query := m.searchQuery
	if query == "" {
		query = "输入搜索关键词..."
	}
	return searchStyle.Render(fmt.Sprintf("🔍 搜索: %s", query))
}

func (m *Model) renderHelpContent() string {
	return `## 快捷键帮助

### 基本操作
| 快捷键 | 功能 |
|--------|------|
| Ctrl+S | 发送消息 |
| Ctrl+C | 取消流式响应 / 退出 |
| Ctrl+F | 搜索聊天历史 |
| Ctrl+_ | 显示此帮助 |
| Esc | 退出程序 |

### 面板切换
| 快捷键 | 面板 |
|--------|------|
| F1 | 📋 规划面板 |
| F2 | 📜 日志面板 |
| F3 | 🤖 智能体面板 |
| F4 | 💾 会话面板 |
| F5 | 🐱 猫猫面板 |
| F6 | ⏰ 任务面板 |
| Tab | 循环切换面板 |

### 侧边栏宽度
| 快捷键 | 功能 |
|--------|------|
| Ctrl+Left | 增大侧边栏宽度 |
| Ctrl+Right | 减小侧边栏宽度 |

### 导航
| 快捷键 | 功能 |
|--------|------|
| ↑/↓ | 上下滚动 |
| PgUp/PgDn | 翻页 |
| Home/End | 跳转首尾 |

### 思考过程与命令
| 快捷键 | 功能 |
|--------|------|
| Alt+T | 切换思考过程显示 |
| /thinking | 命令行切换思考过程 |
| @agent | 调用子智能体 |

### 其他
| 操作 | 说明 |
|------|------|
| 鼠标滚轮 | 滚动面板 |`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 内部方法
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

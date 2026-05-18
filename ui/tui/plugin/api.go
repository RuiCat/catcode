package plugin

// NOTE: UIAPI 接口已定义但尚无实现，也未集成到主 TUI 代码中。
// 这是 v0.9.2 ARCHITECTURE.md 规划的插件 TUI API，
// 插件可通过此接口添加侧边栏标签、弹出对话框、追加聊天内容等。
// 计划在未来版本中实现。

import (
	"time"

	"catcode/tool"
)

// UIAPI 插件界面接口
// 插件通过此接口与 TUI 交互，无需直接操作 TUI 内部状态
type UIAPI interface {
	// RegisterSidebarTab 注册自定义侧边栏 Tab
	RegisterSidebarTab(title string, render func(width int) string) error

	// UnregisterSidebarTab 注销侧边栏 Tab
	UnregisterSidebarTab(title string) error

	// ShowQuestion 显示选项对话框，返回用户回答的 channel
	ShowQuestion(questions []tool.QuestionInfo) <-chan tool.QuestionAnswer

	// ShowConfirm 显示确认对话框
	ShowConfirm(message string) <-chan bool

	// AppendToChat 向聊天区追加内容
	AppendToChat(content string)

	// ShowNotification 显示通知
	ShowNotification(message string, level string, duration time.Duration)

	// SetStatus 设置状态栏键值
	SetStatus(key, value string)
}

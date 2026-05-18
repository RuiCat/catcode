package tui

import (
	"strings"
	"testing"
)

func TestMessageType_Constants(t *testing.T) {
	types := map[MessageType]string{
		MsgUser:      "👤 你",
		MsgAssistant: "🤖 AI",
		MsgTool:      "🔧 工具",
		MsgError:     "❌ 错误",
		MsgSystem:    "📋 系统",
	}
	for mt, want := range types {
		got := mt.String()
		if got != want {
			t.Errorf("MessageType(%d).String() = %q, want %q", mt, got, want)
		}
	}
}

func TestMessageType_Unknown(t *testing.T) {
	var mt MessageType = 99
	if mt.String() != "" {
		t.Errorf("unknown MessageType should return empty string, got %q", mt.String())
	}
}

func TestChatMessage_Fields(t *testing.T) {
	cm := &ChatMessage{
		Type:    MsgUser,
		Content: "hello",
	}
	if cm.Type != MsgUser {
		t.Error("Type should be MsgUser")
	}
	if cm.Content != "hello" {
		t.Error("Content mismatch")
	}
}

func TestChatMessage_ThinkingField(t *testing.T) {
	cm := &ChatMessage{
		Type:           MsgAssistant,
		Content:        "response",
		Thinking:       "thinking...",
		ThinkingFolded: false,
	}
	if cm.Thinking != "thinking..." {
		t.Error("Thinking field mismatch")
	}
	if cm.ThinkingFolded {
		t.Error("ThinkingFolded should default false")
	}
}

func TestStreamMsg_Type(t *testing.T) {
	var sm StreamMsg = "hello"
	if sm != "hello" {
		t.Error("StreamMsg mismatch")
	}
}

func TestStreamDoneMsg_Type(t *testing.T) {
	_ = StreamDoneMsg{}
}

func TestToolCallMsg_Type(t *testing.T) {
	var tc ToolCallMsg = "tool_call"
	if tc != "tool_call" {
		t.Error("ToolCallMsg mismatch")
	}
}

func TestAddMessageMsg_Construct(t *testing.T) {
	msg := AddMessageMsg{
		Type:    MsgAssistant,
		Content: "response",
		Sender:  "general",
	}
	if msg.Type != MsgAssistant || msg.Content != "response" || msg.Sender != "general" {
		t.Error("AddMessageMsg fields mismatch")
	}
}

func TestStatusMsg_Construct(t *testing.T) {
	msg := StatusMsg{ModelName: "deepseek", ToolCount: 5, MsgCount: 10}
	if msg.ModelName != "deepseek" {
		t.Error("ModelName mismatch")
	}
}

func TestUpdateTodosMsg_Construct(t *testing.T) {
	msg := UpdateTodosMsg{Todos: []TodoEntry{{Content: "task1", Status: "pending"}}}
	if len(msg.Todos) != 1 || msg.Todos[0].Content != "task1" {
		t.Error("UpdateTodosMsg mismatch")
	}
}

func TestUpdateLogMsg_Construct(t *testing.T) {
	msg := UpdateLogMsg{Time: "12:00", Content: "log entry", Level: "info"}
	if msg.Level != "info" {
		t.Error("UpdateLogMsg mismatch")
	}
}

func TestQuestionRequestMsg_Construct(t *testing.T) {
	replyCh := make(chan QuestionAnswer, 1)
	msg := QuestionRequestMsg{
		Questions: []QuestionInfo{{Header: "Choose", Options: []QuestionOption{{Label: "A"}}}},
		ReplyCh:   replyCh,
	}
	if len(msg.Questions) != 1 {
		t.Error("QuestionRequestMsg mismatch")
	}
}

func TestScheduleTaskInfo_Fields(t *testing.T) {
	task := ScheduledTaskInfo{ID: 1, Name: "backup", Enabled: true, IntervalSeconds: 60}
	if task.ID != 1 || task.Name != "backup" || !task.Enabled {
		t.Error("ScheduledTaskInfo fields mismatch")
	}
}

func TestSessionInfoMsg_Fields(t *testing.T) {
	msg := SessionInfoMsg{WorkspacePath: "/tmp", PluginCount: 3}
	if msg.WorkspacePath != "/tmp" {
		t.Error("SessionInfoMsg mismatch")
	}
}

func TestModel_HasPendingInput_Empty(t *testing.T) {
	m := &Model{}
	if m.HasPendingInput() {
		t.Error("HasPendingInput should return false for new Model")
	}
}

func TestModel_TakePendingInput_Empty(t *testing.T) {
	m := &Model{}
	result := m.TakePendingInput()
	if result != "" || m.HasPendingInput() {
		t.Error("TakePendingInput on empty should return empty")
	}
}

func TestModel_SidebarWidth_Default(t *testing.T) {
	m := &Model{}
	if m.SidebarWidth() != 0 {
		t.Errorf("SidebarWidth = %d, want 0", m.SidebarWidth())
	}
}

func TestSidebarTab_String(t *testing.T) {
	tests := map[SidebarTab]string{
		TabPlan:      "📋 规划",
		TabLog:       "📜 日志",
		TabAgents:    "🤖 智能体",
		TabCompanion: "🐱 猫猫",
		TabTasks:     "⏰ 任务",
		TabSession:   "💾 会话",
	}
	for tab, want := range tests {
		if got := tab.String(); got != want {
			t.Errorf("SidebarTab(%d).String() = %q, want %q", tab, got, want)
		}
	}
}

func TestSidebarTab_Shortcut(t *testing.T) {
	tests := map[SidebarTab]string{
		TabPlan:      "F1",
		TabLog:       "F2",
		TabAgents:    "F3",
		TabCompanion: "F5",
		TabTasks:     "F6",
		TabSession:   "F4",
	}
	for tab, want := range tests {
		if got := tab.Shortcut(); got != want {
			t.Errorf("SidebarTab(%d).Shortcut() = %q, want %q", tab, got, want)
		}
	}
}

func TestNowTime_Format(t *testing.T) {
	result := nowTime()
	if len(result) != 8 {
		t.Errorf("nowTime() = %q, len=%d, want len=8 (HH:MM:SS)", result, len(result))
	}
	parts := strings.Split(result, ":")
	if len(parts) != 3 {
		t.Errorf("nowTime() = %q, want HH:MM:SS format", result)
	}
}

func TestMutedStyle_ReturnsNonEmpty(t *testing.T) {
	result := mutedStyle("test")
	if result == "" {
		t.Error("mutedStyle should return non-empty string")
	}
}

func TestWarningStyle_ReturnsNonEmpty(t *testing.T) {
	result := warningStyle("test")
	if result == "" {
		t.Error("warningStyle should return non-empty string")
	}
}

func TestErrStyle_ReturnsNonEmpty(t *testing.T) {
	result := errStyle("test")
	if result == "" {
		t.Error("errStyle should return non-empty string")
	}
}

func TestAccentStyle_ReturnsNonEmpty(t *testing.T) {
	result := accentStyle("test")
	if result == "" {
		t.Error("accentStyle should return non-empty string")
	}
}

func TestBoldStyle_ReturnsNonEmpty(t *testing.T) {
	result := boldStyle("test")
	if result == "" {
		t.Error("boldStyle should return non-empty string")
	}
}

package tui

import (
	"testing"
)

func TestCheckMention_Empty(t *testing.T) {
	result := CheckMention("", []AgentInfo{})
	if result != nil {
		t.Errorf("CheckMention(\"\") should return nil, got %v", result)
	}
}

func TestCheckMention_NoAt(t *testing.T) {
	result := CheckMention("hello world", []AgentInfo{})
	if result != nil {
		t.Errorf("CheckMention(%q) should return nil, got %v", "hello world", result)
	}
}

func TestCheckMention_PartialAt(t *testing.T) {
	agents := []AgentInfo{
		{Name: "explore", Description: "搜索代码库"},
		{Name: "plan", Description: "设计方案"},
	}
	result := CheckMention("@ex", agents)
	if result == nil {
		t.Error("CheckMention(@ex) should return MentionState")
		return
	}
	if !result.Active {
		t.Error("MentionState.Active should be true")
	}
	if len(result.Agents) != 1 || result.Agents[0].Name != "explore" {
		t.Errorf("expected [explore], got %v", result.Agents)
	}
}

func TestCheckMention_ExactMatch(t *testing.T) {
	agents := []AgentInfo{
		{Name: "explore", Description: "搜索代码库"},
	}
	result := CheckMention("@explore", agents)
	if result == nil {
		t.Error("CheckMention(@explore) should return MentionState")
		return
	}
	if !result.Active {
		t.Error("MentionState.Active should be true")
	}
}

func TestCheckMention_NoMatch(t *testing.T) {
	agents := []AgentInfo{
		{Name: "explore", Description: "搜索代码库"},
	}
	result := CheckMention("@xyz", agents)
	if result != nil {
		t.Errorf("CheckMention(@xyz) should return nil (no match), got %v", result)
	}
}

func TestCheckMention_EmptyAgentList(t *testing.T) {
	result := CheckMention("@explore", []AgentInfo{})
	if result != nil {
		t.Errorf("CheckMention with empty agent list should return nil, got %v", result)
	}
}

func TestCheckMention_MultipleMatches(t *testing.T) {
	agents := []AgentInfo{
		{Name: "explore", Description: "搜索"},
		{Name: "extractor", Description: "提取"},
	}
	result := CheckMention("@ex", agents)
	if result == nil {
		t.Error("CheckMention(@ex) should return MentionState for multiple matches")
		return
	}
	if len(result.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(result.Agents))
	}
}

func TestCheckMention_WordBoundary(t *testing.T) {
	agents := []AgentInfo{{Name: "explore"}}
	result := CheckMention("email@explore test", agents)
	if result != nil {
		t.Error("CheckMention should not match @ in middle of email")
	}
}

func TestCheckMention_AfterNewline(t *testing.T) {
	agents := []AgentInfo{{Name: "explore"}}
	result := CheckMention("hello\n@ex", agents)
	if result == nil {
		t.Error("CheckMention should match @ after newline")
	}
}

func TestCheckMention_SpaceAfterAt(t *testing.T) {
	agents := []AgentInfo{{Name: "explore"}}
	result := CheckMention("@ ", agents)
	if result != nil {
		t.Error("CheckMention should return nil when query contains space")
	}
}

func TestRenderMention_Nil(t *testing.T) {
	result := RenderMention(nil, 40)
	if result != "" {
		t.Errorf("RenderMention(nil) = %q, want empty", result)
	}
}

func TestRenderMention_Inactive(t *testing.T) {
	ms := &MentionState{Active: false, Agents: []AgentInfo{{Name: "explore"}}}
	result := RenderMention(ms, 40)
	if result != "" {
		t.Errorf("RenderMention(inactive) = %q, want empty", result)
	}
}

func TestRenderMention_Active(t *testing.T) {
	ms := &MentionState{
		Active:   true,
		Query:    "ex",
		Agents:   []AgentInfo{{Name: "explore", Description: "搜索代码"}},
		Selected: 0,
	}
	result := RenderMention(ms, 40)
	if result == "" {
		t.Error("RenderMention(active) should return non-empty string")
	}
}

func TestRenderMention_Selected(t *testing.T) {
	ms := &MentionState{
		Active:   true,
		Query:    "ex",
		Agents:   []AgentInfo{{Name: "explore", Description: "搜索代码"}},
		Selected: 0,
	}
	result := RenderMention(ms, 40)
	if result == "" {
		t.Error("RenderMention should include the menu")
	}
	// selected item should have ▶ marker
	if !contains(result, "▶") {
		t.Error("RenderMention should show ▶ cursor on selected item")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

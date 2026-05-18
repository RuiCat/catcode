package tui

import "testing"

func TestParseAtCommand_NotAtCommand(t *testing.T) {
	tests := []string{
		"",
		"hello world",
		"not an @ command",
		" email@example.com",
	}
	for _, input := range tests {
		result := ParseAtCommand(input)
		if result.IsAtCmd {
			t.Errorf("ParseAtCommand(%q) should not be an @ command", input)
		}
	}
}

func TestParseAtCommand_InvalidAgent(t *testing.T) {
	result := ParseAtCommand("@unknown do something")
	if result.IsAtCmd {
		t.Errorf("ParseAtCommand(@unknown) should not match invalid agent")
	}
	if result.Task != "@unknown do something" {
		t.Errorf("expected task to be original input, got %q", result.Task)
	}
}

func TestParseAtCommand_ValidAgents(t *testing.T) {
	tests := []struct {
		input     string
		agentType string
		task      string
	}{
		{"@explore search the codebase", "explore", "search the codebase"},
		{"@plan design a solution", "plan", "design a solution"},
		{"@general help me", "general", "help me"},
		{"@reviewer review this code", "reviewer", "review this code"},
		{"@verifier verify the output", "verifier", "verify the output"},
		{"@lean4 prove the theorem", "lean4", "prove the theorem"},
	}
	for _, tc := range tests {
		result := ParseAtCommand(tc.input)
		if !result.IsAtCmd {
			t.Errorf("ParseAtCommand(%q) should be an @ command", tc.input)
		}
		if result.AgentType != tc.agentType {
			t.Errorf("ParseAtCommand(%q) agentType = %q, want %q", tc.input, result.AgentType, tc.agentType)
		}
		if result.Task != tc.task {
			t.Errorf("ParseAtCommand(%q) task = %q, want %q", tc.input, result.Task, tc.task)
		}
	}
}

func TestParseAtCommand_CaseInsensitive(t *testing.T) {
	tests := []string{
		"@Explore search",
		"@PLAN design",
		"@GENERAL help",
	}
	for _, input := range tests {
		result := ParseAtCommand(input)
		if !result.IsAtCmd {
			t.Errorf("ParseAtCommand(%q) should be case-insensitive", input)
		}
	}
}

func TestParseAtCommand_NoTask(t *testing.T) {
	result := ParseAtCommand("@explore")
	if !result.IsAtCmd {
		t.Error("ParseAtCommand(@explore) should be an @ command")
	}
	if result.AgentType != "explore" {
		t.Errorf("agentType = %q, want explore", result.AgentType)
	}
	if result.Task != "" {
		t.Errorf("task = %q, want empty", result.Task)
	}
}

func TestParseAtCommand_ExtraSpaces(t *testing.T) {
	result := ParseAtCommand("  @explore   find stuff  ")
	if !result.IsAtCmd {
		t.Error("ParseAtCommand should handle extra spaces")
	}
	if result.AgentType != "explore" {
		t.Errorf("agentType = %q, want explore", result.AgentType)
	}
	if result.Task != "find stuff" {
		t.Errorf("task = %q, want 'find stuff'", result.Task)
	}
}

func TestParseAtCommand_AtInMiddleOfText(t *testing.T) {
	result := ParseAtCommand("text @explore search")
	if result.IsAtCmd {
		t.Error("ParseAtCommand should not match @ in middle of text")
	}
}

func TestAllAgentTypes(t *testing.T) {
	types := AllAgentTypes()
	if len(types) != 6 {
		t.Errorf("AllAgentTypes() len = %d, want 6", len(types))
	}
	expected := map[string]bool{
		"explore": true, "plan": true, "general": true,
		"reviewer": true, "verifier": true, "lean4": true,
	}
	for _, typ := range types {
		if !expected[typ] {
			t.Errorf("unexpected agent type: %s", typ)
		}
	}
}

func TestValidAgentTypes_Sync(t *testing.T) {
	for _, typ := range AllAgentTypes() {
		if !ValidAgentTypes[typ] {
			t.Errorf("AllAgentTypes() returns %q but ValidAgentTypes doesn't contain it", typ)
		}
	}
	if len(ValidAgentTypes) != len(AllAgentTypes()) {
		t.Errorf("ValidAgentTypes and AllAgentTypes() have different lengths: %d vs %d",
			len(ValidAgentTypes), len(AllAgentTypes()))
	}
}

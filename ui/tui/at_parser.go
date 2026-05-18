package tui

import "strings"

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// @ 命令解析器 — 支持 @agent 直接调用子智能体
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// AtCommand @ 命令解析结果
type AtCommand struct {
	AgentType string // 子智能体类型 (explore/plan/general/reviewer/verifier)
	Task      string // 任务描述
	IsAtCmd   bool   // 是否为 @ 命令
}

// ValidAgentTypes 有效的子智能体类型
var ValidAgentTypes = map[string]bool{
	"explore":  true,
	"plan":     true,
	"general":  true,
	"reviewer": true,
	"verifier": true,
	"lean4":    true,
}

// ParseAtCommand 解析 @ 命令
// 格式: @agent 任务描述
// 返回解析结果，IsAtCmd 指示是否匹配
func ParseAtCommand(input string) AtCommand {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "@") {
		return AtCommand{Task: input}
	}

	// 分割 "@agent 任务"
	parts := strings.SplitN(input[1:], " ", 2)
	agentType := strings.ToLower(strings.TrimSpace(parts[0]))
	if !ValidAgentTypes[agentType] {
		return AtCommand{Task: input} // 不是有效 agent，作为普通文本
	}

	task := ""
	if len(parts) > 1 {
		task = strings.TrimSpace(parts[1])
	}

	return AtCommand{
		AgentType: agentType,
		Task:      task,
		IsAtCmd:   true,
	}
}

// AllAgentTypes 返回所有有效的 agent 类型（用于帮助信息）
func AllAgentTypes() []string {
	return []string{"explore", "plan", "general", "reviewer", "verifier", "lean4"}
}

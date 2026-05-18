package subagent

import (
	"catcode/tool"
)

// Config 子智能体配置
type Config struct {
	Type         string
	Model        string
	SystemPrompt string
	Temperature  float64
	MaxTokens    int
	Tools        []string
	ProviderName string
	Permissions  []tool.PermissionRule
}

package hook

import (
	"reflect"

	"catcode/ai/session"
	"github.com/traefik/yaegi/interp"
)

// ContextBuildInput Hook上下文构建的输入
type ContextBuildInput struct {
	Task           string
	ContextSummary string
	AgentType      string
	Extra          map[string]any
}

// ContextBuildResult Hook上下文构建的输出
type ContextBuildResult struct {
	SystemPrompt        string   // 覆盖系统提示词
	MemoryIndex         string   // 覆盖记忆索引
	ExtraSystemMessages []string // 附加系统消息
}

// registerSymbols 向 yaegi 解释器注册 catcode 内部类型符号
func registerSymbols(i *interp.Interpreter) {
	i.Use(interp.Exports{
		"catcode/agent/subagent/hook": {
			"ContextBuildInput":  reflect.ValueOf((*ContextBuildInput)(nil)),
			"ContextBuildResult": reflect.ValueOf((*ContextBuildResult)(nil)),
		},
		"catcode/ai/session/session": {
			"Session": reflect.ValueOf((*session.Session)(nil)),
		},
	})
}

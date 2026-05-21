package orchestrator

import (
	"encoding/json"
	"strings"

	"catcode/ai/llm"
	"catcode/ai/session"
	"catcode/data/storage"
)

// injectMemoryIndex 将记忆索引注入为独立的系统消息
// 索引作为 [记忆索引] 层插入在 SystemPrompt 之后、对话上下文之前
func (a *Architect) injectMemoryIndex() {
	if a.memoryService == nil {
		return
	}
	contextHint := a.extractContextHint()
	index := a.memoryService.BuildIndex(contextHint)
	if index == "" {
		a.mainSession.MemoryIndex = ""
		return
	}
	a.mainSession.MemoryIndex = index
}

// extractContextHint 从最近消息中提取上下文提示（用于智能记忆选择）
func (a *Architect) extractContextHint() string {
	// 从后往前找最近的 user 消息和 assistant 消息，拼接为上下文
	var parts []string
	count := 0
	for i := len(a.mainSession.Messages) - 1; i >= 0 && count < 3; i-- {
		msg := a.mainSession.Messages[i]
		if !msg.Enable {
			continue
		}
		if msg.Role == "user" || msg.Role == "assistant" {
			content := msg.Content
			if len(content) > 200 {
				content = content[:200]
			}
			parts = append([]string{content}, parts...)
			count++
		}
	}
	return strings.Join(parts, " ")
}

// InjectMemoryIndex 公开的记忆索引注入方法（供会话恢复后调用）
func (a *Architect) InjectMemoryIndex() {
	a.injectMemoryIndex()
}

// LoadHistory 从存储层消息行恢复对话历史到主会话
// 保留已注册的工具，只替换消息列表
func (a *Architect) LoadHistory(messages []*storage.MessageRow) {
	a.mainSession.Messages = make([]*session.Message, 0, len(messages))
	for _, m := range messages {
		var toolCalls []llm.ToolCall
		json.Unmarshal([]byte(m.ToolCallsJSON), &toolCalls)

		enabled := true
		if !m.Enabled {
			enabled = false
		}

		msg := &session.Message{
			Role:             m.Role,
			Content:          m.Content,
			Name:             m.Name,
			ToolCallID:       m.ToolCallID,
			ToolCalls:        toolCalls,
			ReasoningContent: m.ReasoningContent,
			Enable:           enabled,
		}
		msg.Update()
		a.mainSession.Messages = append(a.mainSession.Messages, msg)
	}
}


// errStack 从错误中提取堆栈跟踪（如果是 CatError），否则返回 ""
func errStack(err error) string {
	type stackTracer interface {
		StackTrace() string
	}
	if st, ok := err.(stackTracer); ok {
		return st.StackTrace()
	}
	return ""
}


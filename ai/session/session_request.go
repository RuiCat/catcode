package session

import (
	"encoding/json"
	"io"

	"catcode/ai/llm"
	cerr "catcode/core/errors"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 工具管理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// AddTool 注册工具到会话
func (s *Session) AddTool(t *tool.Tool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.ToolsMap[t.Function.Name]; ok {
		return cerr.Newf("session: 重复工具 %s", t.Function.Name)
	}

	if err := t.Update(); err != nil {
		return err
	}
	t.Enable = true

	s.ToolsMap[t.Function.Name] = len(s.Tools)
	s.Tools = append(s.Tools, t)
	return nil
}

// GetTool 按名称获取工具
func (s *Session) GetTool(name string) (*tool.Tool, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	idx, ok := s.ToolsMap[name]
	if !ok {
		return nil, false
	}
	return s.Tools[idx], true
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 请求构建（零拷贝 buffer 拼接）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// BuildRequest 构建完整的 ChatRequest
// 使用预编码缓存优化消息构建
func (s *Session) BuildRequest() (*llm.ChatRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 构建消息列表 — 分层上下文结构
	// 系统提示词 → 记忆索引 → 上下文索引 → 对话上下文
	messages := make([]llm.Message, 0, len(s.Messages)+3)

	// 第1层：系统提示词
	if s.SystemPrompt != "" {
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: s.SystemPrompt,
		})
	}

	// 第2层：记忆索引（全局记忆 + 智慧体记忆）
	if s.MemoryIndex != "" {
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: s.MemoryIndex,
		})
	}

	// 第2.5层：指令文件内容
	if s.InstructionsContent != "" {
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: s.InstructionsContent,
		})
	}

	// 第3层：上下文索引（压缩摘要）
	if s.Summary != "" {
		messages = append(messages, llm.Message{
			Role:    "system",
			Content: "[上下文索引]\n" + s.Summary,
		})
	}

	// 第4层：对话上下文（最近消息 + 工具调用历史）
	for _, msg := range s.Messages {
		if !msg.Enable {
			continue
		}
		content := msg.Content
		// 对 tool 消息在 LLM 上下文层截断，DB 层保留完整数据
		if msg.Role == "tool" && s.MaxToolResultLen > 0 && len(content) > s.MaxToolResultLen {
			content = content[:s.MaxToolResultLen] + "\n... (截断)"
		}
		m := llm.Message{
			Role:             msg.Role,
			Content:          content,
			ReasoningContent: msg.ReasoningContent,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCalls:        msg.ToolCalls,
		}
		messages = append(messages, m)
	}

	// 4. 构建工具列表
	toolDefs := make([]llm.ToolDef, 0, len(s.Tools))
	for _, t := range s.Tools {
		if !t.Enable {
			continue
		}
		toolDefs = append(toolDefs, llm.ToolDef{
			Type: "function",
			Function: llm.FuncDef{
				Name:        t.Function.Name,
				Description: t.Function.Description,
				Parameters:  t.Function.Parameters,
			},
		})
	}

	req := &llm.ChatRequest{
		Model:    s.Model,
		Messages: messages,
		Tools:    toolDefs,
		Stream:   true,
	}
	if s.Temperature > 0 {
		req.Temperature = s.Temperature
	}
	if s.MaxTokens > 0 {
		req.MaxTokens = s.MaxTokens
	}
	return req, nil
}

// BuildCleanRequest 构建独立请求（跳过未完成的 tool_calls 和孤立的 tool 消息）
// 用于子智能体双向通信等需要独立上下文但不依赖完整会话状态的场景
func (s *Session) BuildCleanRequest(question string, maxTokens int) *llm.ChatRequest {
	s.mu.RLock()
	defer s.mu.RUnlock()

	messages := []llm.Message{
		{Role: "system", Content: s.SystemPrompt},
	}
	// 注入记忆索引
	if s.MemoryIndex != "" {
		messages = append(messages, llm.Message{Role: "system", Content: s.MemoryIndex})
	}

	// 提取最近的非 tool_calls 正常消息作为上下文
	var contextMsgs []llm.Message
	for i := len(s.Messages) - 1; i >= 0 && len(contextMsgs) < 6; i-- {
		msg := s.Messages[i]
		if !msg.Enable {
			continue
		}
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			continue
		}
		if msg.Role == "tool" {
			continue
		}
		contextMsgs = append([]llm.Message{{
			Role:    msg.Role,
			Content: msg.Content,
		}}, contextMsgs...)
	}
	messages = append(messages, contextMsgs...)
	messages = append(messages, llm.Message{Role: "user", Content: question})

	req := &llm.ChatRequest{
		Model:    s.Model,
		Messages: messages,
		Stream:   false,
	}
	if maxTokens > 0 {
		req.MaxTokens = maxTokens
	} else if s.MaxTokens > 0 {
		req.MaxTokens = s.MaxTokens
	}
	if s.Temperature > 0 {
		req.Temperature = s.Temperature
	}
	return req
}

// BuildRequestReader 构建零拷贝请求读取器（预留功能，当前未使用）
func (s *Session) BuildRequestReader() io.Reader {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 重置 buffer
	s.msgBuf.Reset()
	s.toolBuf.Reset()

	_, modelName := llm.ParseModelName(s.Model)
	s.msgBuf.AddBytes([]byte(`{"model":"`))
	s.msgBuf.AddString(modelName)
	s.msgBuf.AddBytes([]byte(`","messages":[`))

	// System 消息
	if s.SystemPrompt != "" {
		s.msgBuf.AddBytes([]byte(`{"role":"system","content":`))
		sysData, _ := json.Marshal(s.SystemPrompt)
		s.msgBuf.AddBytes(sysData)
		s.msgBuf.AddBytes([]byte(`},`))
	}

	// 记忆索引
	if s.MemoryIndex != "" {
		s.msgBuf.AddBytes([]byte(`{"role":"system","content":`))
		memData, _ := json.Marshal(s.MemoryIndex)
		s.msgBuf.AddBytes(memData)
		s.msgBuf.AddBytes([]byte(`},`))
	}

	// 指令文件内容
	if s.InstructionsContent != "" {
		s.msgBuf.AddBytes([]byte(`{"role":"system","content":`))
		instData, _ := json.Marshal(s.InstructionsContent)
		s.msgBuf.AddBytes(instData)
		s.msgBuf.AddBytes([]byte(`},`))
	}

	// 上下文索引（压缩摘要）
	if s.Summary != "" {
		s.msgBuf.AddBytes([]byte(`{"role":"system","content":`))
		sumData, _ := json.Marshal("[上下文索引]\n" + s.Summary)
		s.msgBuf.AddBytes(sumData)
		s.msgBuf.AddBytes([]byte(`},`))
	}

	// 历史消息（使用预编码缓存，零拷贝）
	visible := 0
	for _, msg := range s.Messages {
		if msg.Enable {
			visible++
		}
	}
	added := 0
	for _, msg := range s.Messages {
		if !msg.Enable {
			continue
		}
		s.msgBuf.AddPtr(&msg.cachedJSON)
		added++
		if added < visible {
			s.msgBuf.AddBytes([]byte(`,`))
		}
	}

	s.msgBuf.AddBytes([]byte(`],`))

	// 工具
	if len(s.Tools) > 0 {
		// 预计算启用的工具数量（避免 disabled 工具导致逗号错误）
		enabledCount := 0
		for _, t := range s.Tools {
			if t.Enable {
				enabledCount++
			}
		}
		if enabledCount > 0 {
			s.msgBuf.AddBytes([]byte(`"tools":[`))
			added := 0
			for _, t := range s.Tools {
				if !t.Enable {
					continue
				}
				cached := t.CachedJSON()
				s.msgBuf.AddBytes(cached)
				added++
				if added < enabledCount {
					s.msgBuf.AddBytes([]byte(`,`))
				}
			}
			s.msgBuf.AddBytes([]byte(`],`))
		}
	}

	s.msgBuf.AddBytes([]byte(`"stream":true}`))
	return s.msgBuf.Get()
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 统计信息
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// TokenCount 估算当前会话的 token 数量
func (s *Session) TokenCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	total := llm.EstimateTokens(s.SystemPrompt)
	for _, msg := range s.Messages {
		total += llm.EstimateTokens(msg.Content)
	}
	return total
}

// MessageCount 返回消息数量
func (s *Session) MessageCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Messages)
}

// ToolCount 返回已注册工具数量
func (s *Session) ToolCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Tools)
}

// ToolNames 返回已注册工具名称列表
func (s *Session) ToolNames() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := make([]string, len(s.Tools))
	for i, t := range s.Tools {
		names[i] = t.Function.Name
	}
	return names
}

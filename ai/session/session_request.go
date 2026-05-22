package session

import (
	"bytes"
	"encoding/json"
	"fmt"
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

const summaryPrefix = "[上下文索引]\n"

// encodeSystemMessages 将 system 消息序列化为逗号分隔的 JSON 片段（不含外层括号）
// 纯计算函数，不修改 Session 状态，可在 RLock 下安全调用
func (s *Session) encodeSystemMessages() []byte {
	var buf bytes.Buffer
	first := true

	write := func(data []byte) {
		if !first {
			buf.WriteByte(',')
		}
		buf.Write(data)
		first = false
	}

	if s.SystemPrompt != "" {
		data, _ := json.Marshal(llm.Message{Role: "system", Content: s.SystemPrompt})
		write(data)
	}
	if s.MemoryIndex != "" {
		data, _ := json.Marshal(llm.Message{Role: "system", Content: s.MemoryIndex})
		write(data)
	}
	if s.InstructionsContent != "" {
		data, _ := json.Marshal(llm.Message{Role: "system", Content: s.InstructionsContent})
		write(data)
	}
	if s.Summary != "" {
		data, _ := json.Marshal(llm.Message{Role: "system", Content: summaryPrefix + s.Summary})
		write(data)
	}

	return buf.Bytes()
}

// buildSystemMessagesJSON 构建 system 消息 JSON（带缓存）
// 在 RLock 下调用安全：读取缓存时获取 cacheMu 锁
func (s *Session) buildSystemMessagesJSON() []byte {
	currentKey := fmt.Sprintf("SP=%d|MI=%d|IC=%d|SM=%d", len(s.SystemPrompt), len(s.MemoryIndex), len(s.InstructionsContent), len(s.Summary))

	s.cacheMu.Lock()
	if s.cachedSystemJSON != nil && s.systemStateKey == currentKey {
		result := s.cachedSystemJSON
		s.cacheMu.Unlock()
		return result // 缓存命中
	}
	s.cacheMu.Unlock()

	// 缓存未命中，构建并更新
	json := s.encodeSystemMessages()

	s.cacheMu.Lock()
	s.cachedSystemJSON = json
	s.systemStateKey = currentKey
	s.cacheMu.Unlock()

	return json
}

// BuildRequest 构建完整的 ChatRequest
// 使用 buffer 零拷贝拼接消息 JSON + system 消息缓存，避免双重序列化
func (s *Session) BuildRequest() (*llm.ChatRequest, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 获取 system 消息 JSON（带缓存）
	sysJSON := s.buildSystemMessagesJSON()

	// 构建完整消息数组
	s.msgBuf.Reset()
	s.msgBuf.AddBytes([]byte(`[`))

	hasPrev := len(sysJSON) > 0
	if hasPrev {
		s.msgBuf.AddBytes(sysJSON)
	}

	// 对话上下文 — 零拷贝引用 Message.cachedJSON
	for _, msg := range s.Messages {
		if !msg.Enable {
			continue
		}
		// tool 消息截断时需重新编码
		if msg.Role == "tool" && s.MaxToolResultLen > 0 && len(msg.Content) > s.MaxToolResultLen {
			if hasPrev {
				s.msgBuf.AddBytes([]byte(`,`))
			}
			truncated := llm.Message{
				Role:       msg.Role,
				Content:    msg.Content[:s.MaxToolResultLen] + "\n... (截断)",
				Name:       msg.Name,
				ToolCallID: msg.ToolCallID,
			}
			data, _ := json.Marshal(truncated)
			s.msgBuf.AddBytes(data)
			hasPrev = true
		} else {
			if msg.cachedJSON == nil {
				msg.Update()
			}
			if hasPrev {
				s.msgBuf.AddBytes([]byte(`,`))
			}
			s.msgBuf.AddPtr(&msg.cachedJSON)
			hasPrev = true
		}
	}

	s.msgBuf.AddBytes([]byte(`]`))
	messagesJSON := s.msgBuf.Bytes()

	// 构建工具列表
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
		Model:                s.Model,
		Messages:             nil,
		PrebuiltMessagesJSON: messagesJSON,
		Tools:                toolDefs,
		Stream:               true,
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
		sumData, _ := json.Marshal(summaryPrefix + s.Summary)
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

// RecalculateTokenCount 重新计算 token 计数（用于一致性校验）
func (s *Session) RecalculateTokenCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	total := llm.EstimateTokens(s.SystemPrompt)
	for _, msg := range s.Messages {
		if msg.Enable {
			total += llm.EstimateTokens(msg.Content)
		}
	}
	return total
}

// TokenCount 估算当前会话的 token 数量（O(1) 增量维护）
func (s *Session) TokenCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runningTokenCount
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

package session

import (
	"encoding/json"
	"fmt"
	"time"

	"catcode/ai/llm"
	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 消息管理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// AddMessage 添加消息到对话历史
func (s *Session) AddMessage(role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg := &Message{Role: role, Content: content, Enable: true}
	if err := msg.Update(); err != nil {
		return cerr.Wrap(err, "session: 消息预编码失败")
	}
	s.Messages = append(s.Messages, msg)
	s.runningTokenCount += llm.EstimateTokens(content)
	s.UpdatedAt = time.Now()
	return nil
}

// AddToolResult 添加工具结果消息
func (s *Session) AddToolResult(toolCallID, name, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 防御：拒绝空 tool_call_id，及早暴露调用方 bug
	if toolCallID == "" {
		return cerr.Newf("session.AddToolResult: tool_call_id 不能为空 (tool: %s)", name)
	}

	// 检查是否已存在相同 toolCallID 的结果 → 幂等，替换而非重复添加
	for _, msg := range s.Messages {
		if msg.Role == "tool" && msg.ToolCallID == toolCallID {
			// 替换现有结果（保留最新内容）
			oldTokens := llm.EstimateTokens(msg.Content)
			msg.Content = content
			msg.Name = name
			if err := msg.Update(); err != nil {
				return err
			}
			s.runningTokenCount += llm.EstimateTokens(content) - oldTokens
			s.UpdatedAt = time.Now()
			return nil
		}
	}

	msg := &Message{
		Role:       "tool",
		Content:    content,
		Name:       name,
		ToolCallID: toolCallID,
		Enable:     true,
	}
	if err := msg.Update(); err != nil {
		return err
	}
	s.Messages = append(s.Messages, msg)
	s.runningTokenCount += llm.EstimateTokens(content)
	s.UpdatedAt = time.Now()
	return nil
}

// AddAssistantWithTools 添加包含工具调用的 assistant 消息
func (s *Session) AddAssistantWithTools(content string, toolCalls []llm.ToolCall) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg := &Message{
		Role:      "assistant",
		Content:   content,
		Enable:    true,
		ToolCalls: toolCalls,
	}

	if len(toolCalls) > 0 {
		// 构建包含 tool_calls 的 JSON
		type assistantMsg struct {
			Role      string         `json:"role"`
			Content   string         `json:"content"`
			ToolCalls []llm.ToolCall `json:"tool_calls"`
		}
		am := assistantMsg{Role: "assistant", Content: content, ToolCalls: toolCalls}
		data, err := json.Marshal(am)
		if err != nil {
			return err
		}
		msg.cachedJSON = data
	} else {
		if err := msg.Update(); err != nil {
			return err
		}
	}

	s.Messages = append(s.Messages, msg)
	s.runningTokenCount += llm.EstimateTokens(content)
	if len(toolCalls) > 0 {
		tcJSON, _ := json.Marshal(toolCalls)
		s.runningTokenCount += llm.EstimateTokens(string(tcJSON))
	}
	s.UpdatedAt = time.Now()
	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 消息验证与清理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// CleanOrphanedToolCalls 清理不完整的 tool_calls（避免 API 400 错误）
// 遍历所有 assistant-with-tool_calls 消息，确保每个 tool_call.id 之后都有对应的
// tool 角色消息（按 tool_call_id 精确匹配）。任何未闭合的都会导致该消息及之后的内容被移除。
func (s *Session) CleanOrphanedToolCalls() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.Messages) == 0 {
		return
	}

	// 从后往前检查所有包含 tool_calls 的 assistant 消息
	for i := len(s.Messages) - 1; i >= 0; i-- {
		msg := s.Messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 {
			continue
		}

		// 验证该 assistant 的所有 tool_call_id 是否都有对应的 tool 结果
		for _, tc := range msg.ToolCalls {
			matched := false
			for j := i + 1; j < len(s.Messages); j++ {
				if s.Messages[j].Role == "tool" && s.Messages[j].ToolCallID == tc.ID && s.Messages[j].Enable {
					matched = true
					break
				}
			}
			if !matched {
				// 发现未闭合的 tool_call → 禁用该 assistant 及紧随其后的所有 tool 消息
				// （非破坏性：用 Enable=false 标记而非截断，避免牵连下游有效消息）
				if s.Messages[i].Enable {
					s.runningTokenCount -= llm.EstimateTokens(s.Messages[i].Content)
				}
				s.Messages[i].Enable = false
				// 禁用该 assistant 对应的所有 tool 结果
				for j := i + 1; j < len(s.Messages); j++ {
					if s.Messages[j].Role == "assistant" {
						break // 遇到下一个 assistant，停止
					}
					for _, tc := range msg.ToolCalls {
						if s.Messages[j].Role == "tool" && s.Messages[j].ToolCallID == tc.ID {
							if s.Messages[j].Enable {
								s.runningTokenCount -= llm.EstimateTokens(s.Messages[j].Content)
							}
							s.Messages[j].Enable = false
						}
					}
				}
				s.UpdatedAt = time.Now()
				// 不 truncate，继续检查其他 assistant
			}
		}
	}

	// 第二轮：去重 tool 结果（保留每个 tool_call_id 的最新出现）
	seen := make(map[string]int) // tool_call_id → index of latest occurrence
	for i := len(s.Messages) - 1; i >= 0; i-- {
		msg := s.Messages[i]
		if msg.Role == "tool" && msg.ToolCallID != "" {
			if prevIdx, exists := seen[msg.ToolCallID]; exists {
				// Duplicate found - disable the earlier occurrence
				if s.Messages[prevIdx].Enable {
					s.runningTokenCount -= llm.EstimateTokens(s.Messages[prevIdx].Content)
				}
				s.Messages[prevIdx].Enable = false
			}
			seen[msg.ToolCallID] = i
		}
	}
}

// ValidateMessages 校验消息完整性，返回第一个发现的问题描述
// 检查项：tool 消息 tool_call_id 非空、assistant tool_calls 有对应 tool 结果
func (s *Session) ValidateMessages() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for i, msg := range s.Messages {
		// 检查 tool 消息必须有 tool_call_id
		if msg.Role == "tool" && msg.ToolCallID == "" && msg.Enable {
			return fmt.Sprintf("消息[%d] tool 角色缺少 tool_call_id (name=%s)", i, msg.Name)
		}
	}

	// 从后向前检查 assistant tool_calls 完整性
	for i := len(s.Messages) - 1; i >= 0; i-- {
		msg := s.Messages[i]
		if msg.Role != "assistant" || len(msg.ToolCalls) == 0 || !msg.Enable {
			continue
		}
		for _, tc := range msg.ToolCalls {
			matched := false
			for j := i + 1; j < len(s.Messages); j++ {
				if s.Messages[j].Role == "tool" && s.Messages[j].ToolCallID == tc.ID && s.Messages[j].Enable {
					matched = true
					break
				}
			}
			if !matched {
				return fmt.Sprintf("assistant 消息[%d] 的 tool_call %s 缺少对应 tool 结果", i, tc.ID)
			}
		}
	}
	return ""
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 消息状态管理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// LastMessage 返回最后一条消息
func (s *Session) LastMessage() *Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Messages) == 0 {
		return nil
	}
	return s.Messages[len(s.Messages)-1]
}

// ClearReasoning 清除所有消息的 reasoning_content（线程安全）
func (s *Session) ClearReasoning() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, msg := range s.Messages {
		msg.ReasoningContent = ""
	}
}

// Clear 清空所有消息
func (s *Session) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = s.Messages[:0]
	s.runningTokenCount = llm.EstimateTokens(s.SystemPrompt)
	s.MemoryIndex = ""
	s.Summary = ""
	s.UpdatedAt = time.Now()

	// 清除 system 消息缓存
	s.cacheMu.Lock()
	s.cachedSystemJSON = nil
	s.systemStateKey = ""
	s.cacheMu.Unlock()
}

// TrimMessages 保留最近 N 条消息，删除更早的
func (s *Session) TrimMessages(keep int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Messages) > keep {
		for i := 0; i < len(s.Messages)-keep; i++ {
			if s.Messages[i].Enable {
				s.runningTokenCount -= llm.EstimateTokens(s.Messages[i].Content)
			}
		}
		s.Messages = s.Messages[len(s.Messages)-keep:]
		s.UpdatedAt = time.Now()
	}
}


// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Token 计数维护方法
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DisableMessage 禁用消息并更新 token 计数（供 compact 等外部包使用）
func (s *Session) DisableMessage(index int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= 0 && index < len(s.Messages) && s.Messages[index].Enable {
		s.runningTokenCount -= llm.EstimateTokens(s.Messages[index].Content)
		s.Messages[index].Enable = false
	}
}

// DisableMessages 批量禁用消息并更新 token 计数（减少锁竞争）
func (s *Session) DisableMessages(indices []int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, idx := range indices {
		if idx >= 0 && idx < len(s.Messages) && s.Messages[idx].Enable {
			s.runningTokenCount -= llm.EstimateTokens(s.Messages[idx].Content)
			s.Messages[idx].Enable = false
		}
	}
}

// AppendMessage 安全地追加消息并更新 token 计数（供 compact 等外部包使用）
func (s *Session) AppendMessage(msg *Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if msg.cachedJSON == nil {
		if err := msg.Update(); err != nil {
			return cerr.Wrap(err, "session: AppendMessage 预编码失败")
		}
	}
	s.Messages = append(s.Messages, msg)
	s.runningTokenCount += llm.EstimateTokens(msg.Content)
	return nil
}

// RestoreMessages 从 DB 恢复消息列表（替换当前消息并重建 token 计数）
func (s *Session) RestoreMessages(msgs []*Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Messages = msgs
	s.runningTokenCount = llm.EstimateTokens(s.SystemPrompt)
	for _, msg := range msgs {
		if msg.Enable {
			s.runningTokenCount += llm.EstimateTokens(msg.Content)
		}
	}
}

// SetMessageContent 替换消息内容并更新 token 计数（供 compact 等外部包使用）
func (s *Session) SetMessageContent(index int, newContent string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.Messages) {
		return
	}
	msg := s.Messages[index]
	if !msg.Enable {
		msg.Content = newContent
		if err := msg.Update(); err != nil {
			return
		}
		return // 禁用消息不参与 token 计数，但仍然更新缓存
	}
	oldTokens := llm.EstimateTokens(msg.Content)
	msg.Content = newContent
	if err := msg.Update(); err != nil {
		msg.Content = "" // 恢复旧值较复杂，至少清空避免使用旧缓存
		return
	}
	s.runningTokenCount += llm.EstimateTokens(newContent) - oldTokens
}

package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"catcode/ai/llm"
	"catcode/data/storage"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 上下文压缩
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// NeedsCompression 检查是否需要压缩
func (s *Session) NeedsCompression() bool {
	return s.TokenCount() > s.CompressThreshold
}

// SetSummary 设置压缩摘要
func (s *Session) SetSummary(summary string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Summary = summary
	s.UpdatedAt = time.Now()
	s.cacheMu.Lock()
	s.cachedSystemJSON = nil
	s.systemStateKey = ""
	s.cacheMu.Unlock()
}

// SetLastReasoning 设置最后一条消息的 reasoning_content（线程安全）
func (s *Session) SetLastReasoning(content string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Messages) > 0 {
		s.Messages[len(s.Messages)-1].ReasoningContent = content
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Session ↔ DB 转换
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ToConversationRow 将 Session 转换为 DB ConversationRow
func (s *Session) ToConversationRow() *storage.ConversationRow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	metaJSON, _ := json.Marshal(s.Metadata)
	return &storage.ConversationRow{
		ID:                s.ID,
		Model:             s.Model,
		SystemPrompt:      s.SystemPrompt,
		Summary:           s.Summary,
		CompressThreshold: s.CompressThreshold,
		MetadataJSON:      string(metaJSON),
		MessageCount:      len(s.Messages),
		TokenCount:        s.TokenCount(),
		CreatedAt:         s.CreatedAt,
		UpdatedAt:         s.UpdatedAt,
	}
}

// ToMessageRows 将 Session 的消息转换为 DB MessageRow 列表
func (s *Session) ToMessageRows() []*storage.MessageRow {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var rows []*storage.MessageRow
	for i, msg := range s.Messages {
		// 只保存启用的消息（禁用的消息是压缩过的，不应持久化）
		if !msg.Enable {
			continue
		}
		tcJSON, _ := json.Marshal(msg.ToolCalls)
		enabledInt := 1
		rows = append(rows, &storage.MessageRow{
			ConversationID:   s.ID,
			Seq:              i,
			Role:             msg.Role,
			Content:          msg.Content,
			Name:             msg.Name,
			ToolCallID:       msg.ToolCallID,
			ToolCallsJSON:    string(tcJSON),
			ReasoningContent: msg.ReasoningContent,
			Enabled:          enabledInt == 1,
		})
	}
	return rows
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 文件块管理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// UpsertFileBlock 插入或替换文件块，返回是否替换了已有块
func (s *Session) UpsertFileBlock(toolCallID, path, content string, offset, endLine, totalLines, totalBytes int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if fb, ok := s.FileBlocks[path]; ok && fb.MsgIndex >= 0 && fb.MsgIndex < len(s.Messages) {
		prevSummary := extractKeyInfo(fb.Content)
		newContent := content
		if prevSummary != "" {
			newContent = fmt.Sprintf("[上一块 行%d-%d 摘要: %s]\n[当前块 行%d-%d/%d]\n%s",
				fb.Offset, fb.EndLine, prevSummary, offset, endLine, totalLines, content)
		} else {
			newContent = fmt.Sprintf("[当前块 行%d-%d/%d]\n%s", offset, endLine, totalLines, content)
		}
		oldTokens := llm.EstimateTokens(s.Messages[fb.MsgIndex].Content)
		s.Messages[fb.MsgIndex].Content = newContent
		s.Messages[fb.MsgIndex].Update()
		s.runningTokenCount += llm.EstimateTokens(newContent) - oldTokens
		fb.Offset = offset
		fb.EndLine = endLine
		fb.Content = content
		fb.PrevSummary = prevSummary
		return true
	}
	newContent := fmt.Sprintf("[文件: %s, 行%d-%d/%d, %d字节]\n%s", path, offset, endLine, totalLines, totalBytes, content)
	msg := &Message{Role: "tool", Content: newContent, Name: "read", ToolCallID: toolCallID, Enable: true}
	msg.Update()
	s.Messages = append(s.Messages, msg)
	s.runningTokenCount += llm.EstimateTokens(newContent)
	s.FileBlocks[path] = &FileBlock{Path: path, Offset: offset, EndLine: endLine, Content: content, TotalLines: totalLines, TotalBytes: totalBytes, MsgIndex: len(s.Messages) - 1}
	return false
}

func extractKeyInfo(content string) string {
	var parts []string
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "func ") {
			if idx := strings.Index(trimmed, "{"); idx > 0 {
				parts = append(parts, trimmed[:idx])
			}
		} else if strings.HasPrefix(trimmed, "type ") && strings.Contains(trimmed, "struct") {
			parts = append(parts, trimmed)
		} else if strings.HasPrefix(trimmed, "import ") {
			parts = append(parts, trimmed)
		}
	}
	if len(parts) > 5 {
		parts = parts[:5]
	}
	return strings.Join(parts, "; ")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 字段 getter/setter（SessionInterface 接口需要）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (s *Session) GetID() string                   { return s.ID }
func (s *Session) GetModel() string                { return s.Model }
func (s *Session) GetSystemPrompt() string         { return s.SystemPrompt }
func (s *Session) SetSystemPrompt(p string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runningTokenCount += llm.EstimateTokens(p) - llm.EstimateTokens(s.SystemPrompt)
	s.SystemPrompt = p
	s.cacheMu.Lock()
	s.cachedSystemJSON = nil
	s.systemStateKey = ""
	s.cacheMu.Unlock()
}
func (s *Session) GetTemperature() float64         { return s.Temperature }
// SetTemperature 设置 Temperature（线程安全）
func (s *Session) SetTemperature(t float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Temperature = t
}
func (s *Session) GetMaxTokens() int               { return s.MaxTokens }
// SetMaxTokens 设置 MaxTokens（线程安全）
func (s *Session) SetMaxTokens(m int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MaxTokens = m
}
func (s *Session) GetMemoryIndex() string          { return s.MemoryIndex }
// SetMemoryIndex 设置记忆索引（线程安全，并失效系统消息缓存）
func (s *Session) SetMemoryIndex(idx string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.MemoryIndex = idx
	s.cacheMu.Lock()
	s.cachedSystemJSON = nil
	s.systemStateKey = ""
	s.cacheMu.Unlock()
}
func (s *Session) GetSummary() string              { return s.Summary }
func (s *Session) GetMaxToolResultLen() int        { return s.MaxToolResultLen }
func (s *Session) SetMaxToolResultLen(l int)       { s.MaxToolResultLen = l }
func (s *Session) GetCompressThreshold() int       { return s.CompressThreshold }
func (s *Session) SetCompressThreshold(t int)      { s.CompressThreshold = t }
func (s *Session) GetInstructionsContent() string  { return s.InstructionsContent }
// SetInstructionsContent 设置指令文件内容（线程安全，并失效系统消息缓存）
func (s *Session) SetInstructionsContent(c string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InstructionsContent = c
	s.cacheMu.Lock()
	s.cachedSystemJSON = nil
	s.systemStateKey = ""
	s.cacheMu.Unlock()
}
func (s *Session) LockMessages()                   { s.mu.Lock() }
func (s *Session) UnlockMessages()                 { s.mu.Unlock() }

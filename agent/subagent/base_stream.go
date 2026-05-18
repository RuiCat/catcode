package subagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"catcode/ai/llm"
	"catcode/ai/session"
	"catcode/core/event"
	"catcode/data/storage"
)

// loadMessages 从存储层消息行恢复子智能体对话历史
func (sa *BaseAgent) loadMessages(messages []*storage.MessageRow) {
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
		sa.session.Messages = append(sa.session.Messages, msg)
	}
	sa.session.CleanOrphanedToolCalls()
	if issue := sa.session.ValidateMessages(); issue != "" {
		sa.session.Clear()
		if sa.bus != nil {
			sa.bus.PublishAsync(event.EventAgentStatusChanged, map[string]any{
				"agent_id": sa.id,
				"status":   "reset",
				"reason":   issue,
			})
		}
	}
}

// publishAgentStatus 发布子智能体状态变更事件（供 TUI 订阅）
func (sa *BaseAgent) publishAgentStatus() {
	if sa.bus == nil {
		return
	}
	sa.mu.RLock()
	data := map[string]any{
		"type":         sa.agentType,
		"id":           sa.id,
		"status":       sa.status,
		"task":         sa.task,
		"current_tool": sa.currentTool,
		"tool_count":   sa.toolCount,
		"start_time":   sa.startTime,
		"duration":     sa.duration,
		"error_msg":    sa.errorMsg,
	}
	sa.mu.RUnlock()
	sa.bus.PublishAsync(event.EventAgentStatusChanged, data)
}

// streamResult processStream 的处理结果
type streamResult struct {
	content   string
	toolCalls []*llm.ToolCall
	reasoning string
}

// processStream 处理子智能体的 LLM 流式响应
func (sa *BaseAgent) processStream(ctx context.Context, streamCh <-chan *llm.StreamEvent, responseCh chan<- string, task string) (streamResult, error) {
	var content string
	var reasoningBuilder strings.Builder
	var fullOutputSB strings.Builder
	var toolCalls []*llm.ToolCall
	for {
		select {
		case <-ctx.Done():
			sa.mu.Lock()
			sa.currentTool = ""
			sa.status = "error"
			sa.errorMsg = fmt.Sprintf("上下文已取消: %v", ctx.Err())
			sa.duration = time.Since(sa.startTime)
			sa.mu.Unlock()
			sa.publishAgentStatus()
			if sa.bus != nil {
				sa.bus.PublishAsync(event.EventTaskFailed, map[string]any{
					"agent": sa.agentType,
					"id":    sa.id,
					"task":  task,
					"error": sa.errorMsg,
				})
			}
			return streamResult{}, ctx.Err()
		case evt, ok := <-streamCh:
			if !ok {
				sa.mu.Lock()
				sa.currentTool = ""
				sa.status = "error"
				sa.errorMsg = "流通道异常关闭（未收到终端事件）"
				sa.duration = time.Since(sa.startTime)
				sa.mu.Unlock()
				sa.publishAgentStatus()
				if sa.bus != nil {
					sa.bus.PublishAsync(event.EventTaskFailed, map[string]any{
						"agent": sa.agentType,
						"id":    sa.id,
						"task":  task,
						"error": sa.errorMsg,
					})
				}
				return streamResult{}, fmt.Errorf("stream closed unexpectedly")
			}
			switch evt.Type {
			case llm.StreamReasoning:
				reasoningBuilder.WriteString(evt.ReasoningContent)
				responseCh <- "\n🧠>" + evt.ReasoningContent
			case llm.StreamToolCall:
				if evt.Tool != nil {
					sa.mu.Lock()
					sa.currentTool = evt.Tool.Function.Name
					sa.toolCount++
					sa.mu.Unlock()
					toolCalls = append(toolCalls, evt.Tool)
					args := evt.Tool.Function.Arguments
					if len(args) > 2000 {
						args = args[:2000] + "\n... (截断)"
					}
					safeArgs := strings.ReplaceAll(args, "```", "\\`\\`\\`")
					fullOutputSB.WriteString(fmt.Sprintf("\n\n🔧 **工具调用**: `%s`\n```json\n%s\n```\n",
						evt.Tool.Function.Name, safeArgs))
					if sa.bus != nil {
						sa.bus.PublishAsync(event.EventAgentToolStart, map[string]any{
							"agent": sa.agentType,
							"id":    sa.id,
							"tool":  evt.Tool.Function.Name,
						})
					}
				}
			case llm.StreamTextDelta:
				content += evt.Content
				fullOutputSB.WriteString(evt.Content)
				responseCh <- evt.Content
			case llm.StreamDone:
				if len(toolCalls) > 0 && content == "" {
					content = " "
				}
				reasoning := reasoningBuilder.String()
				if reasoning != "" {
					responseCh <- "\n🧠✓"
					fullOutputSB.WriteString(fmt.Sprintf("\n\n💭 **推理**: %s\n", reasoning))
				}
				if len(toolCalls) > 0 {
					sa.session.AddAssistantWithTools(content, llm.ConvertToolCalls(toolCalls))
					if reasoning != "" {
						sa.session.SetLastReasoning(reasoning)
					}
				} else if content != "" {
					sa.session.AddMessage("assistant", content)
					if reasoning != "" {
						sa.session.SetLastReasoning(reasoning)
					}
				}

				if len(toolCalls) > 0 {
					return streamResult{content: content, toolCalls: toolCalls, reasoning: reasoning}, nil
				}
				return streamResult{content: content}, nil
			case llm.StreamError:
				sa.mu.Lock()
				sa.currentTool = ""
				sa.status = "error"
				if evt.Error != nil {
					sa.errorMsg = evt.Error.Error()
				} else {
					sa.errorMsg = "未知流错误"
				}
				sa.duration = time.Since(sa.startTime)
				sa.mu.Unlock()
				sa.publishAgentStatus()
				if sa.bus != nil {
					sa.bus.PublishAsync(event.EventTaskFailed, map[string]any{
						"agent": sa.agentType,
						"id":    sa.id,
						"task":  task,
						"error": sa.errorMsg,
					})
				}
				responseCh <- fmt.Sprintf("\n[错误: %v]", evt.Error)
				if evt.Error != nil {
					return streamResult{}, evt.Error
				}
				return streamResult{}, fmt.Errorf("未知流错误")
			}
		}
	}
}

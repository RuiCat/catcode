package orchestrator

import (
	"context"
	"fmt"
	"strings"

	"catcode/ai/llm"

	cerr "catcode/core/errors"
)

// StreamResult processStream 的处理结果
type StreamResult struct {
	Content   string          // 累积的文本内容
	ToolCalls []*llm.ToolCall // LLM 请求的工具调用
	Reasoning string          // thinking 推理内容
}

// processStream 处理 LLM 流式响应
func (a *Architect) processStream(ctx context.Context, streamCh <-chan *llm.StreamEvent, responseCh chan<- string) (StreamResult, error) {
	var contentBuilder strings.Builder
	var reasoningBuilder strings.Builder
	var toolCalls []*llm.ToolCall

	for {
		select {
		case <-ctx.Done():
			return StreamResult{}, ctx.Err()
		case evt, ok := <-streamCh:
			if !ok {
				return StreamResult{}, cerr.New("stream closed unexpectedly")
			}

			switch evt.Type {
			case llm.StreamTextDelta:
				contentBuilder.WriteString(evt.Content)
				responseCh <- evt.Content

			case llm.StreamReasoning:
				reasoningBuilder.WriteString(evt.ReasoningContent)
				// 流式展示思考过程给用户
				responseCh <- "\n🧠>" + evt.ReasoningContent

			case llm.StreamToolCall:
				if evt.Tool != nil {
					toolCalls = append(toolCalls, evt.Tool)
				}

			case llm.StreamDone:
				content := contentBuilder.String()

				// 确保有 tool_calls 时 content 字段不为空（API 要求）
				if len(toolCalls) > 0 && content == "" {
					content = " "
				}

				// 1. 始终将 assistant 消息加入会话
				reasoning := reasoningBuilder.String()
				if len(toolCalls) > 0 {
					a.mainSession.AddAssistantWithTools(content, llm.ConvertToolCalls(toolCalls))
					// 注入 reasoning_content
					if reasoning != "" {
						a.mainSession.SetLastReasoning(reasoning)
					}
					names := make([]string, len(toolCalls))
					for i, tc := range toolCalls {
						names[i] = tc.Function.Name
					}
					a.planStatus = "执行工具: " + strings.Join(names, ", ")
					responseCh <- fmt.Sprintf("\n🔧 调用工具: %s", strings.Join(names, ", "))
				} else if content != "" {
					a.mainSession.AddMessage("assistant", content)
					if reasoning != "" {
						a.mainSession.SetLastReasoning(reasoning)
					}
				}

				// 标记思考过程结束
				if reasoning != "" {
					responseCh <- "\n🧠✓"
				}

				return StreamResult{Content: content, ToolCalls: toolCalls, Reasoning: reasoning}, nil

			case llm.StreamError:
				if evt.Error != nil {
					return StreamResult{}, evt.Error
				}
				return StreamResult{}, cerr.New("stream error")
			}
		}
	}
}

// clearThinkingState 清除会话中所有消息的 reasoning_content
func (a *Architect) clearThinkingState() {
	a.mainSession.ClearReasoning()
}

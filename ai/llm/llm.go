// Package llm 实现 LLM 提供商类型定义和 OpenAI 兼容客户端
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"catcode/core/buffer"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 请求/响应类型
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ChatRequest 统一对话请求
type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Tools       []ToolDef `json:"tools,omitempty"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
	// Thinking 模式
	Thinking *ThinkingConfig `json:"thinking,omitempty"`
}

// ThinkingConfig thinking 模式配置
type ThinkingConfig struct {
	Type         string `json:"type"` // "enabled"
	BudgetTokens int    `json:"budget_tokens"`
}

// Message 消息
type Message struct {
	Role             string     `json:"role"`
	Content          string     `json:"content"`
	ReasoningContent string     `json:"reasoning_content,omitempty"` // thinking 模式推理内容
	Name             string     `json:"name,omitempty"`
	ToolCallID       string     `json:"tool_call_id,omitempty"`
	ToolCalls        []ToolCall `json:"tool_calls,omitempty"`
}

// ToolDef 工具定义（发送给 LLM）
type ToolDef struct {
	Type     string  `json:"type"`
	Function FuncDef `json:"function"`
}

// FuncDef 函数定义
type FuncDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall 工具调用
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolCallFunc `json:"function"`
}

// ToolCallFunc 工具调用函数部分
type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON 字符串
}

// ChatResponse 非流式对话响应
type ChatResponse struct {
	ID      string   `json:"id"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice 响应选项
type Choice struct {
	Index   int     `json:"index"`
	Message Message `json:"message"`
	Reason  string  `json:"finish_reason"`
}

// Usage token 使用统计
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 流式事件
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// StreamEventType 流式事件类型
type StreamEventType int

const (
	StreamTextDelta StreamEventType = iota
	StreamReasoning                 // thinking 模式推理内容
	StreamToolCall
	StreamDone
	StreamError
)

// StreamEvent 流式事件
type StreamEvent struct {
	Type             StreamEventType
	Content          string    // TextDelta 时的文本增量
	ReasoningContent string    // ReasonDelta 时的推理增量
	Tool             *ToolCall // ToolCall 时的工具调用
	Usage            *Usage    // Done 时的 token 统计
	Error            error     // Error 时的错误
}

// String 返回 StreamEventType 的可读表示
func (t StreamEventType) String() string {
	switch t {
	case StreamTextDelta:
		return "TextDelta"
	case StreamReasoning:
		return "Reasoning"
	case StreamToolCall:
		return "ToolCall"
	case StreamDone:
		return "Done"
	case StreamError:
		return "Error"
	default:
		return "Unknown"
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// OpenAI 兼容 Provider
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// OpenAIClient OpenAI 兼容 API 客户端
type OpenAIClient struct {
	name    string
	baseURL string
	apiKey  string
	client  *http.Client
}

// NewOpenAI 创建 OpenAI 兼容客户端
func NewOpenAI(name, baseURL, apiKey string) Provider {
	return &OpenAIClient{
		name:    name,
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		client: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// Name 返回提供商名称
func (c *OpenAIClient) Name() string { return c.name }

// Close 关闭客户端（no-op for HTTP client）
func (c *OpenAIClient) Close() error { return nil }

// Chat 发送流式对话请求（含重试）
func (c *OpenAIClient) Chat(ctx context.Context, req *ChatRequest) (<-chan *StreamEvent, error) {
	req.Stream = true

	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		body, err := c.buildRequestBody(req)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			c.baseURL+"/chat/completions", body)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.client.Do(httpReq)
		if err != nil {
			lastErr = cerr.Wrap(err, "llm: HTTP 请求失败")
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = cerr.Newf("llm: API 错误 %d (将重试)", resp.StatusCode)
			continue
		}

		if resp.StatusCode != 200 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, cerr.Newf("llm: API 错误 %d: %s", resp.StatusCode, string(errBody))
		}

		return parseSSE(ctx, resp.Body), nil
	}

	return nil, cerr.Wrapf(lastErr, "llm: 重试 %d 次后仍失败", maxRetries)
}

// ChatSync 发送非流式对话请求（含重试）
func (c *OpenAIClient) ChatSync(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	req.Stream = false

	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(attempt*attempt) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		body, err := c.buildRequestBody(req)
		if err != nil {
			return nil, err
		}

		httpReq, err := http.NewRequestWithContext(ctx, "POST",
			c.baseURL+"/chat/completions", body)
		if err != nil {
			return nil, err
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)

		resp, err := c.client.Do(httpReq)
		if err != nil {
			lastErr = cerr.Wrap(err, "llm: HTTP 请求失败")
			continue
		}

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			resp.Body.Close()
			lastErr = cerr.Newf("llm: API 错误 %d (将重试)", resp.StatusCode)
			continue
		}

		if resp.StatusCode != 200 {
			errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
			return nil, cerr.Newf("llm: API 错误 %d: %s", resp.StatusCode, string(errBody))
		}

		var chatResp ChatResponse
		if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
			resp.Body.Close()
			return nil, cerr.Wrap(err, "llm: 响应解析失败")
		}
		resp.Body.Close()
		return &chatResp, nil
	}

	return nil, cerr.Wrapf(lastErr, "llm: 重试 %d 次后仍失败", maxRetries)
}

// buildRequestBody 构建请求体（使用零拷贝 Buffer）
func (c *OpenAIClient) buildRequestBody(req *ChatRequest) (io.Reader, error) {
	// 使用 Buffer 零拷贝拼接 JSON
	buf := buffer.New()
	_, modelName := ParseModelName(req.Model)
	buf.AddBytes([]byte(`{"model":"`))
	buf.AddString(modelName)
	buf.AddBytes([]byte(`","messages":`))

	msgBytes, err := json.Marshal(req.Messages)
	if err != nil {
		return nil, err
	}
	buf.AddBytes(msgBytes)

	if len(req.Tools) > 0 {
		buf.AddBytes([]byte(`,"tools":`))
		toolBytes, err := json.Marshal(req.Tools)
		if err != nil {
			return nil, err
		}
		buf.AddBytes(toolBytes)
	}

	buf.AddBytes([]byte(`,"stream":`))
	if req.Stream {
		buf.AddBytes([]byte(`true`))
	} else {
		buf.AddBytes([]byte(`false`))
	}

	if req.Temperature > 0 {
		buf.AddBytes([]byte(fmt.Sprintf(`,"temperature":%f`, req.Temperature)))
	}
	if req.MaxTokens > 0 {
		buf.AddBytes([]byte(fmt.Sprintf(`,"max_tokens":%d`, req.MaxTokens)))
	}
	if req.Thinking != nil && req.Thinking.Type == "enabled" {
		buf.AddBytes([]byte(fmt.Sprintf(
			`,"thinking":{"type":"enabled","budget_tokens":%d}`, req.Thinking.BudgetTokens)))
	}

	buf.AddBytes([]byte(`}`))
	return buf.Get(), nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SSE 流式解析器 (纯标准库实现)
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const streamIdleTimeout = 180 * time.Second // 180秒无数据视为API无响应

// parseSSE 解析 SSE 流式响应，返回事件 channel
// 内置空闲超时检测：如果 streamIdleTimeout 内未收到任何 SSE 事件，视为 API 无响应并超时
// ctx 用于调用方取消（调用方主动取消 goroutine）
func parseSSE(ctx context.Context, r io.ReadCloser) <-chan *StreamEvent {
	ch := make(chan *StreamEvent, 32)
	go func() {
		defer close(ch)
		defer r.Close()

		// 行读取 goroutine —— 从阻塞的 scanner 中解耦
		type lineResult struct {
			line string
			err  error
		}
		lineCh := make(chan lineResult, 1)

		scanner := bufio.NewScanner(r)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

		// 读取 goroutine：持续扫描 SSE 行
		go func() {
			for scanner.Scan() {
				lineCh <- lineResult{line: scanner.Text()}
			}
			lineCh <- lineResult{err: scanner.Err()}
		}()

		// 空闲超时计时器：每次收到有效数据重置
		idleTimer := time.NewTimer(streamIdleTimeout)
		defer idleTimer.Stop()

		accumulatedTools := make(map[int]*ToolCall)
		var hasData bool

		for {
			select {
			case <-ctx.Done():
				return

			case <-idleTimer.C:
				// 空闲超时：长时间未收到任何 SSE 数据
				ch <- &StreamEvent{
					Type:  StreamError,
					Error: fmt.Errorf("API 无响应超时: %v 内未收到数据", streamIdleTimeout),
				}
				return

			case lr := <-lineCh:
				// 收到新数据行，重置空闲计时器
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(streamIdleTimeout)
				hasData = true

				if lr.err != nil {
					// scanner 错误（包括正常EOF）
					if hasData {
						// 先发送累积的 tool_calls
						for _, tc := range accumulatedTools {
							ch <- &StreamEvent{
								Type: StreamToolCall,
								Tool: tc,
							}
						}
						ch <- &StreamEvent{Type: StreamDone}
					} else if lr.err.Error() != "EOF" {
						ch <- &StreamEvent{Type: StreamError, Error: lr.err}
					}
					return
				}

				line := lr.line
				if !strings.HasPrefix(line, "data: ") {
					continue
				}
				data := strings.TrimPrefix(line, "data: ")

				if data == "[DONE]" {
					// 先发送累积的 tool_calls
					for _, tc := range accumulatedTools {
						ch <- &StreamEvent{
							Type: StreamToolCall,
							Tool: tc,
						}
					}
					ch <- &StreamEvent{Type: StreamDone}
					return
				}

				var chunk struct {
					Choices []struct {
						Delta struct {
							Content          string `json:"content"`
							ReasoningContent string `json:"reasoning_content"`
							ToolCalls        []struct {
								Index    int    `json:"index"`
								ID       string `json:"id"`
								Type     string `json:"type"`
								Function struct {
									Name      string `json:"name"`
									Arguments string `json:"arguments"`
								} `json:"function"`
							} `json:"tool_calls"`
						} `json:"delta"`
						FinishReason string `json:"finish_reason"`
					} `json:"choices"`
					Usage *Usage `json:"usage"`
				}

				if err := json.Unmarshal([]byte(data), &chunk); err != nil {
					continue
				}

				for _, choice := range chunk.Choices {
					// 文本增量
					if choice.Delta.Content != "" {
						ch <- &StreamEvent{
							Type:    StreamTextDelta,
							Content: choice.Delta.Content,
						}
					}
					// thinking 模式推理内容
					if choice.Delta.ReasoningContent != "" {
						ch <- &StreamEvent{
							Type:             StreamReasoning,
							ReasoningContent: choice.Delta.ReasoningContent,
						}
					}
					// 工具调用增量
					for _, tc := range choice.Delta.ToolCalls {
						existing, ok := accumulatedTools[tc.Index]
						if !ok {
							if tc.ID != "" {
								existing = &ToolCall{
									ID:   tc.ID,
									Type: tc.Type,
									Function: ToolCallFunc{
										Name: tc.Function.Name,
									},
								}
								accumulatedTools[tc.Index] = existing
							} else {
								continue
							}
						} else {
							if existing.ID == "" && tc.ID != "" {
								existing.ID = tc.ID
							}
							if existing.Type == "" && tc.Type != "" {
								existing.Type = tc.Type
							}
							if existing.Function.Name == "" && tc.Function.Name != "" {
								existing.Function.Name = tc.Function.Name
							}
						}
						existing.Function.Arguments += tc.Function.Arguments
					}
				}
			}
		}
	}()
	return ch
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 工具函数
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// EstimateTokens 估算文本的 token 数
// 启发式方法：约每4个字符1个token（适用于中英文混合）
func EstimateTokens(text string) int {
	return len([]rune(text)) / 4
}

// CollectStreamContent 收集流式事件中的所有文本内容
func CollectStreamContent(ch <-chan *StreamEvent) (string, []*ToolCall, error) {
	var content bytes.Buffer
	var toolCalls []*ToolCall

	for evt := range ch {
		switch evt.Type {
		case StreamTextDelta:
			content.WriteString(evt.Content)
		case StreamToolCall:
			toolCalls = append(toolCalls, evt.Tool)
		case StreamError:
			return content.String(), toolCalls, evt.Error
		case StreamDone:
			return content.String(), toolCalls, nil
		}
	}
	return content.String(), toolCalls, nil
}

// ConvertToolCalls 将 []*ToolCall 转换为 []ToolCall（值拷贝），
// 避免跨 goroutine 共享指针导致的并发问题。
func ConvertToolCalls(calls []*ToolCall) []ToolCall {
	result := make([]ToolCall, len(calls))
	for i, tc := range calls {
		result[i] = *tc
	}
	return result
}

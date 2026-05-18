// Package llm 实现 LLM 提供商抽象层
package llm

import (
	"context"
	"io"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Provider 接口
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Provider 模型提供商接口
// 支持 OpenAI 兼容 API、Anthropic 等不同后端
type Provider interface {
	// Name 返回提供商名称
	Name() string
	// Chat 发送流式对话请求，返回 SSE 事件流
	Chat(ctx context.Context, req *ChatRequest) (<-chan *StreamEvent, error)
	// ChatSync 发送非流式对话请求，直接返回完整响应
	ChatSync(ctx context.Context, req *ChatRequest) (*ChatResponse, error)
	// Close 关闭连接，释放资源
	io.Closer
}

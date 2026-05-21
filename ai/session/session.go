// Package session 实现 LLM 会话管理
// 借鉴 catai 的 Messages 预编码缓存 + Buffer 零拷贝拼接
package session

import (
	"bytes"
	"encoding/json"
	"sync"
	"time"

	"catcode/ai/llm"
	"catcode/core/buffer"
	"catcode/data/storage"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 会话
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Session LLM 对话会话
// 子接口按功能拆分（接口隔离原则），组合接口向后兼容

// MessageAccessor 消息读写接口
type MessageAccessor interface {
	AddMessage(role, content string) error
	AddToolResult(toolCallID, name, content string) error
	AddAssistantWithTools(content string, toolCalls []llm.ToolCall) error
	CleanOrphanedToolCalls()
	ValidateMessages() string
	Clear()
	ClearReasoning()
}

// ToolAccessor 工具注册接口
type ToolAccessor interface {
	AddTool(t *tool.Tool) error
	GetTool(name string) (*tool.Tool, bool)
	ToolCount() int
}

// RequestBuilder 请求构建接口
type RequestBuilder interface {
	BuildRequest() (*llm.ChatRequest, error)
	BuildCleanRequest(question string, maxTokens int) *llm.ChatRequest
}

// SessionSerializer 持久化序列化接口
type SessionSerializer interface {
	ToConversationRow() *storage.ConversationRow
	ToMessageRows() []*storage.MessageRow
}

// SessionConfig 配置访问器接口
type SessionConfig interface {
	GetID() string
	GetModel() string
	GetSystemPrompt() string
	SetSystemPrompt(p string)
	GetTemperature() float64
	SetTemperature(t float64)
	GetMaxTokens() int
	SetMaxTokens(m int)
	GetMemoryIndex() string
	SetMemoryIndex(idx string)
	GetSummary() string
	SetSummary(summary string)
	SetLastReasoning(content string)
	GetMaxToolResultLen() int
	SetMaxToolResultLen(l int)
	GetCompressThreshold() int
	SetCompressThreshold(t int)
	GetInstructionsContent() string
	SetInstructionsContent(c string)
	LockMessages()
	UnlockMessages()
}

// SessionStats 统计接口
type SessionStats interface {
	TokenCount() int
	MessageCount() int
}

// SessionInterface 组合了会话的所有操作接口（共 33+ 方法）。
// 按职责可分为以下子接口，消费方应按需使用精确接口：
//   - MessageAccessor — 消息读写 (7 methods)
//   - ToolAccessor — 工具注册 (3 methods)
//   - RequestBuilder — 请求构建 (2 methods)
//   - SessionSerializer — 持久化序列化 (2 methods)
//   - SessionConfig — 配置访问器 (17 methods, 建议进一步拆分)
//   - SessionStats — 统计 (2 methods)
// TODO: 将 SessionConfig 的 getter/setter 对简化为可导出字段或配置结构体，
//       减少接口方法数量，降低 mock 复杂度。

// SessionInterface LLM 会话管理接口（向后兼容的组合接口）
type SessionInterface interface {
	MessageAccessor
	ToolAccessor
	RequestBuilder
	SessionSerializer
	SessionConfig
	SessionStats
}

// Session LLM 对话会话实现
type Session struct {
	ID           string         // 会话唯一标识
	Model        string         // 使用的模型
	SystemPrompt string         // 系统提示词
	Temperature  float64        // 默认请求 temperature
	MaxTokens    int            // 默认请求 max_tokens
	Messages     []*Message     // 对话历史
	Tools        []*tool.Tool   // 已注册工具
	ToolsMap     map[string]int // 工具名→索引
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Metadata     map[string]string // 元数据

	// 上下文压缩
	CompressThreshold   int // token 压缩阈值
	Summary             string
	MemoryIndex         string                // 记忆索引（global + workspace，注入为独立系统消息）
	InstructionsContent string                // 指令文件内容（注入到 BuildRequest 的 system context 中）
	MaxToolResultLen    int                   // 工具结果传给 LLM 时的截断长度（DB 层仍存完整数据）
	FileBlocks          map[string]*FileBlock // 文件路径 → 文件块（替换式读取）

	// 预编码 buffer（零拷贝关键）
	msgBuf  buffer.Buffer // 消息拼接 buffer
	toolBuf buffer.Buffer // 工具拼接 buffer

	mu sync.RWMutex
}

// New 创建新会话
func New(id, model, systemPrompt string) *Session {
	return &Session{
		ID:                id,
		Model:             model,
		SystemPrompt:      systemPrompt,
		Messages:          make([]*Message, 0),
		Tools:             make([]*tool.Tool, 0),
		ToolsMap:          make(map[string]int),
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
		Metadata:          make(map[string]string),
		CompressThreshold: 900000, // 1M 上下文下 90万触发压缩
		MaxToolResultLen:  4000,   // 工具结果传给 LLM 的默认截断长度
		FileBlocks:        make(map[string]*FileBlock),
		msgBuf:            buffer.New(),
		toolBuf:           buffer.New(),
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 消息结构
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Message 消息（含预编码缓存）
type Message struct {
	Role       string `json:"role"`
	Content    string `json:"content"`
	Name       string `json:"name,omitempty"`
	ToolCallID string `json:"tool_call_id"` // 必须字段，空值不应被省略（便于暴露调用方bug）

	// thinking 模式：推理内容必须回传给 API
	ReasoningContent string `json:"reasoning_content,omitempty"`

	// 预编码 JSON 缓存（零拷贝关键）
	cachedJSON []byte `json:"-"`
	Enable     bool   `json:"-"` // 是否在上下文中启用
	// assistant 消息可能包含 tool_calls
	ToolCalls []llm.ToolCall `json:"tool_calls,omitempty"`
}

// Update 预编码消息为 JSON 缓存
func (m *Message) Update() error {
	buf := bytes.NewBuffer(m.cachedJSON[:0])
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return err
	}
	m.cachedJSON = buf.Bytes()
	// 去除尾部换行（json.Encoder 会添加）
	if len(m.cachedJSON) > 0 && m.cachedJSON[len(m.cachedJSON)-1] == '\n' {
		m.cachedJSON = m.cachedJSON[:len(m.cachedJSON)-1]
	}
	return nil
}

// CachedJSON 返回预编码的 JSON 缓存
func (m *Message) CachedJSON() []byte {
	return m.cachedJSON
}

// FileBlock 文件块（用于替换式读取）
type FileBlock struct {
	Path        string
	Offset      int
	EndLine     int
	Content     string
	TotalLines  int
	TotalBytes  int
	PrevSummary string
	MsgIndex    int
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Session ↔ DB 转换
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// FromConversationRow 从 DB ConversationRow + MessageRows 重建 Session
func FromConversationRow(conv *storage.ConversationRow, msgs []*storage.MessageRow) *Session {
	var metadata map[string]string
	json.Unmarshal([]byte(conv.MetadataJSON), &metadata)
	if metadata == nil {
		metadata = make(map[string]string)
	}

	sess := New(conv.ID, conv.Model, conv.SystemPrompt)
	sess.Summary = conv.Summary
	sess.CompressThreshold = conv.CompressThreshold
	sess.Metadata = metadata
	sess.CreatedAt = conv.CreatedAt
	sess.UpdatedAt = conv.UpdatedAt

	for _, m := range msgs {
		var toolCalls []llm.ToolCall
		json.Unmarshal([]byte(m.ToolCallsJSON), &toolCalls)

		msg := &Message{
			Role:             m.Role,
			Content:          m.Content,
			Name:             m.Name,
			ToolCallID:       m.ToolCallID,
			ToolCalls:        toolCalls,
			ReasoningContent: m.ReasoningContent,
			Enable:           m.Enabled,
		}
		msg.Update()
		sess.Messages = append(sess.Messages, msg)
	}
	sess.CleanOrphanedToolCalls()

	return sess
}

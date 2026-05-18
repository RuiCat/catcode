package storage

import (
	"encoding/json"
	"time"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ConversationRow — conversations 表行
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ConversationRow conversations 表的行结构
type ConversationRow struct {
	ID                string
	Model             string
	SystemPrompt      string
	Summary           string
	CompressThreshold int
	MetadataJSON      string
	MessageCount      int
	TokenCount        int
	CreatedAt         time.Time
	UpdatedAt         time.Time
}

// ConversationInfo 会话简要信息
type ConversationInfo struct {
	ID           string
	Model        string
	MessageCount int
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Messages
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// MessageRow messages 表的行结构
type MessageRow struct {
	ID               int64
	ConversationID   string
	Seq              int
	Role             string
	Content          string
	Name             string
	ToolCallID       string
	ToolCallsJSON    string
	ReasoningContent string
	Enabled          bool
	CreatedAt        time.Time
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Conversations CRUD
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// CreateConversation 创建新会话
func (w *workspaceDBImpl) CreateConversation(conv *ConversationRow) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.Exec(`
		INSERT INTO conversations (
			id, model, system_prompt, summary, compress_threshold,
			metadata_json, message_count, token_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		conv.ID, conv.Model, conv.SystemPrompt, conv.Summary,
		conv.CompressThreshold, conv.MetadataJSON,
		conv.MessageCount, conv.TokenCount,
	)
	return err
}

// SaveConversation 保存或更新会话（包含消息）
func (w *workspaceDBImpl) SaveConversation(conv *ConversationRow, messages []*MessageRow) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.Begin()
	if err != nil {
		return cerr.Wrap(err, "storage: 开始事务失败")
	}
	defer tx.Rollback()

	// 更新 conversation
	_, err = tx.Exec(`
		INSERT INTO conversations (
			id, model, system_prompt, summary, compress_threshold,
			metadata_json, message_count, token_count, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(id) DO UPDATE SET
			model = excluded.model,
			system_prompt = excluded.system_prompt,
			summary = excluded.summary,
			compress_threshold = excluded.compress_threshold,
			metadata_json = excluded.metadata_json,
			message_count = excluded.message_count,
			token_count = excluded.token_count,
			updated_at = CURRENT_TIMESTAMP
	`,
		conv.ID, conv.Model, conv.SystemPrompt, conv.Summary,
		conv.CompressThreshold, conv.MetadataJSON,
		conv.MessageCount, conv.TokenCount,
	)
	if err != nil {
		return cerr.Wrap(err, "storage: 保存会话失败")
	}

	// 删除旧消息并重新写入（清旧存新策略）
	if _, err := tx.Exec("DELETE FROM messages WHERE conversation_id = ?", conv.ID); err != nil {
		return cerr.Wrap(err, "storage: 清理旧消息失败")
	}

	// 批量插入消息
	for _, msg := range messages {
		if _, err := tx.Exec(`
			INSERT INTO messages (conversation_id, seq, role, content, name, tool_call_id, tool_calls_json, reasoning_content, enabled)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, conv.ID, msg.Seq, msg.Role, msg.Content, msg.Name, msg.ToolCallID, msg.ToolCallsJSON, msg.ReasoningContent, msg.Enabled); err != nil {
			return cerr.Wrap(err, "storage: 保存消息失败")
		}
	}

	return tx.Commit()
}

// LoadConversation 加载会话及其消息
func (w *workspaceDBImpl) LoadConversation(id string) (*ConversationRow, []*MessageRow, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// 加载 conversation
	var conv ConversationRow
	err := w.db.QueryRow(`
		SELECT id, model, system_prompt, summary, compress_threshold,
			metadata_json, message_count, token_count, created_at, updated_at
		FROM conversations WHERE id = ?
	`, id).Scan(
		&conv.ID, &conv.Model, &conv.SystemPrompt, &conv.Summary,
		&conv.CompressThreshold, &conv.MetadataJSON,
		&conv.MessageCount, &conv.TokenCount,
		&conv.CreatedAt, &conv.UpdatedAt,
	)
	if err != nil {
		return nil, nil, cerr.Wrapf(err, "storage: 会话 %s 不存在", id)
	}

	// 加载消息
	rows, err := w.db.Query(`
		SELECT id, conversation_id, seq, role, content, name, tool_call_id, tool_calls_json, reasoning_content, enabled, created_at
		FROM messages WHERE conversation_id = ? ORDER BY seq ASC
	`, id)
	if err != nil {
		return &conv, nil, cerr.Wrap(err, "storage: 查询消息失败")
	}
	defer rows.Close()

	var messages []*MessageRow
	for rows.Next() {
		var msg MessageRow
		if err := rows.Scan(
			&msg.ID, &msg.ConversationID, &msg.Seq, &msg.Role, &msg.Content,
			&msg.Name, &msg.ToolCallID, &msg.ToolCallsJSON, &msg.ReasoningContent, &msg.Enabled, &msg.CreatedAt,
		); err != nil {
			continue
		}
		messages = append(messages, &msg)
	}

	return &conv, messages, nil
}

// ListConversations 列出所有会话（简要信息）
func (w *workspaceDBImpl) ListConversations() ([]ConversationInfo, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	rows, err := w.db.Query(`
		SELECT id, model, message_count, created_at, updated_at
		FROM conversations
		ORDER BY updated_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ConversationInfo
	for rows.Next() {
		var info ConversationInfo
		if err := rows.Scan(&info.ID, &info.Model, &info.MessageCount,
			&info.CreatedAt, &info.UpdatedAt); err != nil {
			continue
		}
		result = append(result, info)
	}
	return result, nil
}

// DeleteConversation 删除会话（级联删除消息）
func (w *workspaceDBImpl) DeleteConversation(id string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	// 先级联删除关联消息
	if _, err := w.db.Exec("DELETE FROM messages WHERE conversation_id = ?", id); err != nil {
		return err
	}
	_, err := w.db.Exec("DELETE FROM conversations WHERE id = ?", id)
	return err
}

// SearchConversations 搜索会话（按消息内容关键词）
func (w *workspaceDBImpl) SearchConversations(keyword string) ([]ConversationInfo, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	rows, err := w.db.Query(`
		SELECT DISTINCT c.id, c.model, c.message_count, c.created_at, c.updated_at
		FROM conversations c
		INNER JOIN messages m ON m.conversation_id = c.id
		WHERE m.content LIKE ?
		ORDER BY c.updated_at DESC
		LIMIT 20
	`, "%"+keyword+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ConversationInfo
	for rows.Next() {
		var info ConversationInfo
		if err := rows.Scan(&info.ID, &info.Model, &info.MessageCount,
			&info.CreatedAt, &info.UpdatedAt); err != nil {
			continue
		}
		result = append(result, info)
	}
	return result, nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 辅助函数
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ToJSON 将任意值序列化为 JSON 字符串
func ToJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

// FromJSON 从 JSON 字符串反序列化
func FromJSON(raw string, target any) error {
	return json.Unmarshal([]byte(raw), target)
}

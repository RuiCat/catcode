package storage

import (
	"fmt"
	"time"
)

// ErrorLogEntry 错误日志条目
type ErrorLogEntry struct {
	ID             int64
	Category       string // 错误类别
	Severity       string // error / warning / info
	Message        string // 错误消息
	StackTrace     string // 堆栈跟踪
	Source         string // 来源 (architect/subagent/llm/startup)
	ConversationID string // 关联会话ID
	CreatedAt      time.Time
}

// LogError 写入错误日志
func (w *workspaceDBImpl) LogError(category, severity, message, stackTrace, source, convID string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.Exec(`
		INSERT INTO error_logs (category, severity, message, stack_trace, source, conversation_id)
		VALUES (?, ?, ?, ?, ?, ?)
	`, category, severity, message, stackTrace, source, convID)
	return err
}

// GetErrorLogs 查询错误日志（最近 N 条）
func (w *workspaceDBImpl) GetErrorLogs(limit int) ([]*ErrorLogEntry, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	rows, err := w.db.Query(`
		SELECT id, category, severity, message, stack_trace, source, conversation_id, created_at
		FROM error_logs ORDER BY created_at DESC LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*ErrorLogEntry
	for rows.Next() {
		var e ErrorLogEntry
		if err := rows.Scan(&e.ID, &e.Category, &e.Severity, &e.Message,
			&e.StackTrace, &e.Source, &e.ConversationID, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, &e)
	}
	return entries, nil
}

// GetErrorLogsByCategory 按类别查询错误日志
func (w *workspaceDBImpl) GetErrorLogsByCategory(category string, limit int) ([]*ErrorLogEntry, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	rows, err := w.db.Query(`
		SELECT id, category, severity, message, stack_trace, source, conversation_id, created_at
		FROM error_logs WHERE category = ? ORDER BY created_at DESC LIMIT ?
	`, category, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*ErrorLogEntry
	for rows.Next() {
		var e ErrorLogEntry
		if err := rows.Scan(&e.ID, &e.Category, &e.Severity, &e.Message,
			&e.StackTrace, &e.Source, &e.ConversationID, &e.CreatedAt); err != nil {
			continue
		}
		entries = append(entries, &e)
	}
	return entries, nil
}

// CleanOldErrorLogs 清理 N 天前的错误日志
func (w *workspaceDBImpl) CleanOldErrorLogs(days int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.Exec(`
		DELETE FROM error_logs WHERE created_at < datetime('now', ?)
	`, fmt.Sprintf("-%d days", days))
	return err
}

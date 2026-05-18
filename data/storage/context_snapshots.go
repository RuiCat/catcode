package storage

import "time"

// SnapshotInfo 上下文快照简要信息
type SnapshotInfo struct {
	ID             int64
	ConversationID string
	Label          string
	TokenCount     int
	CreatedAt      time.Time
}

// CreateSnapshot 创建上下文快照
func (w *workspaceDBImpl) CreateSnapshot(convID, label, messagesJSON, summary string, tokenCount int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.Exec(`
		INSERT INTO context_snapshots (conversation_id, label, messages_json, summary, token_count)
		VALUES (?, ?, ?, ?, ?)
	`, convID, label, messagesJSON, summary, tokenCount)
	return err
}

// ListSnapshots 列出会话的所有快照
func (w *workspaceDBImpl) ListSnapshots(convID string) ([]*SnapshotInfo, error) {
	rows, err := w.db.Query(`
		SELECT id, conversation_id, label, token_count, created_at
		FROM context_snapshots WHERE conversation_id = ? ORDER BY created_at DESC
	`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*SnapshotInfo
	for rows.Next() {
		var s SnapshotInfo
		if err := rows.Scan(&s.ID, &s.ConversationID, &s.Label, &s.TokenCount, &s.CreatedAt); err != nil {
			continue
		}
		result = append(result, &s)
	}
	return result, nil
}

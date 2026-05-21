package storage

import (
	"fmt"
	"os"
	"runtime/debug"
	"time"
)

// MemoryEntry 长期记忆条目
type MemoryEntry struct {
	ID          int64
	Scope       string // global / workspace
	Key         string
	Content     string
	Description string
	MemoryType  string // user/feedback/project/reference
	Tags        string
	Importance  int
	AccessCount int
	CreatedAt   time.Time
	UpdatedAt   time.Time
	AccessedAt  time.Time
}

// MemoryHeader 记忆索引头部（轻量扫描用）
type MemoryHeader struct {
	Key         string
	Description string
	MemoryType  string
	Importance  int
	AgeDays     int
	MtimeMs     int64
}

// SetMemory 写入或更新记忆条目（含 scope 多级索引字段）
func (w *workspaceDBImpl) SetMemory(scope, key, content, description, memoryType, tags string, importance int) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.Exec(`
		INSERT INTO memory (scope, key, content, description, memory_type, tags, importance, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(scope, key) DO UPDATE SET
			scope = excluded.scope,
			content = excluded.content,
			description = excluded.description,
			memory_type = excluded.memory_type,
			tags = excluded.tags,
			importance = excluded.importance,
			updated_at = CURRENT_TIMESTAMP
	`, scope, key, content, description, memoryType, tags, importance)
	return err
}

// GetMemory 获取记忆条目
func (w *workspaceDBImpl) GetMemory(scope, key string) (*MemoryEntry, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var m MemoryEntry
	err := w.db.QueryRow(`
		SELECT id, scope, key, content, description, memory_type, tags,
			importance, access_count, created_at, updated_at, accessed_at
		FROM memory WHERE scope = ? AND key = ?
	`, scope, key).Scan(&m.ID, &m.Scope, &m.Key, &m.Content, &m.Description, &m.MemoryType, &m.Tags,
		&m.Importance, &m.AccessCount, &m.CreatedAt, &m.UpdatedAt, &m.AccessedAt)
	if err != nil {
		return nil, err
	}
	// 更新访问时间（轻量写，非关键路径使用独立语句，通过写锁序列化）
	go func(scope, key string) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "[PANIC] memory update goroutine: %v\n%s\n", r, debug.Stack())
			}
		}()
		w.mu.Lock()
		defer w.mu.Unlock()
		if _, err := w.db.Exec("UPDATE memory SET access_count = access_count + 1, accessed_at = CURRENT_TIMESTAMP WHERE scope = ? AND key = ?", scope, key); err != nil {
			// 访问计数更新失败不影响主流程，静默忽略
		}
	}(scope, key)
	return &m, nil
}

// DeleteMemory 删除记忆条目
func (w *workspaceDBImpl) DeleteMemory(scope, key string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.Exec("DELETE FROM memory WHERE scope = ? AND key = ?", scope, key)
	return err
}

// ListMemory 列出所有记忆（可按 scope 过滤，空字符串表示全部）
func (w *workspaceDBImpl) ListMemory(scope string) ([]*MemoryEntry, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var query string
	var args []interface{}
	if scope == "" {
		query = `SELECT id, scope, key, content, description, memory_type, tags,
			importance, access_count, created_at, updated_at, accessed_at
		FROM memory ORDER BY importance DESC, updated_at DESC`
	} else {
		query = `SELECT id, scope, key, content, description, memory_type, tags,
			importance, access_count, created_at, updated_at, accessed_at
		FROM memory WHERE scope = ? ORDER BY importance DESC, updated_at DESC`
		args = append(args, scope)
	}

	rows, err := w.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*MemoryEntry
	for rows.Next() {
		var m MemoryEntry
		if err := rows.Scan(&m.ID, &m.Scope, &m.Key, &m.Content, &m.Description, &m.MemoryType, &m.Tags,
			&m.Importance, &m.AccessCount, &m.CreatedAt, &m.UpdatedAt, &m.AccessedAt); err != nil {
			continue
		}
		result = append(result, &m)
	}
	return result, nil
}

// ScanMemoryHeaders 扫描记忆索引头部（轻量级，不加载完整内容）
func (w *workspaceDBImpl) ScanMemoryHeaders(scope string) ([]*MemoryHeader, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var query string
	var args []interface{}
	if scope == "" {
		query = `
			SELECT key, description, memory_type, importance,
				CAST((julianday('now') - julianday(updated_at)) AS INTEGER) as age_days,
				CAST(strftime('%s', updated_at) AS INTEGER) * 1000 as mtime_ms
			FROM memory ORDER BY importance DESC, updated_at DESC LIMIT 100
		`
	} else {
		query = `
			SELECT key, description, memory_type, importance,
				CAST((julianday('now') - julianday(updated_at)) AS INTEGER) as age_days,
				CAST(strftime('%s', updated_at) AS INTEGER) * 1000 as mtime_ms
			FROM memory WHERE scope = ? ORDER BY importance DESC, updated_at DESC LIMIT 100
		`
		args = append(args, scope)
	}

	rows, err := w.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*MemoryHeader
	for rows.Next() {
		var h MemoryHeader
		if err := rows.Scan(&h.Key, &h.Description, &h.MemoryType, &h.Importance, &h.AgeDays, &h.MtimeMs); err != nil {
			continue
		}
		result = append(result, &h)
	}
	return result, nil
}

// FindRelevantMemories 按关键词和类型查找相关记忆
func (w *workspaceDBImpl) FindRelevantMemories(scope, query string, limit int) ([]*MemoryEntry, error) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	var sql string
	var args []interface{}
	likePattern := "%" + query + "%"

	if scope == "" {
		sql = `
			SELECT id, scope, key, content, description, memory_type, tags,
				importance, access_count, created_at, updated_at, accessed_at
			FROM memory
			WHERE content LIKE ? OR description LIKE ? OR tags LIKE ?
			ORDER BY importance DESC, updated_at DESC
			LIMIT ?
		`
		args = append(args, likePattern, likePattern, likePattern, limit)
	} else {
		sql = `
			SELECT id, scope, key, content, description, memory_type, tags,
				importance, access_count, created_at, updated_at, accessed_at
			FROM memory
			WHERE scope = ? AND (content LIKE ? OR description LIKE ? OR tags LIKE ?)
			ORDER BY importance DESC, updated_at DESC
			LIMIT ?
		`
		args = append(args, scope, likePattern, likePattern, likePattern, limit)
	}

	rows, err := w.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*MemoryEntry
	for rows.Next() {
		var m MemoryEntry
		if err := rows.Scan(&m.ID, &m.Scope, &m.Key, &m.Content, &m.Description, &m.MemoryType, &m.Tags,
			&m.Importance, &m.AccessCount, &m.CreatedAt, &m.UpdatedAt, &m.AccessedAt); err != nil {
			continue
		}
		result = append(result, &m)
	}
	return result, nil
}

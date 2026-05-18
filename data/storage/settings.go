package storage

import (
	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Settings 表 CRUD
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// GetSetting 获取单个配置值
func (w *workspaceDBImpl) GetSetting(key string) (value, valueType string, err error) {
	err = w.db.QueryRow(
		"SELECT value, value_type FROM settings WHERE key = ?", key,
	).Scan(&value, &valueType)
	return
}

// SetSetting 写入单个配置
func (w *workspaceDBImpl) SetSetting(key, value, valueType, source string) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.Exec(`
		INSERT INTO settings (key, value, value_type, source, updated_at)
		VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(key) DO UPDATE SET
			value = excluded.value,
			value_type = excluded.value_type,
			source = excluded.source,
			updated_at = CURRENT_TIMESTAMP
	`, key, value, valueType, source)
	return err
}

// GetAllSettings 获取所有配置条目
func (w *workspaceDBImpl) GetAllSettings() (map[string]SettingEntry, error) {
	rows, err := w.db.Query(
		"SELECT key, value, value_type, source, description FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]SettingEntry)
	for rows.Next() {
		var e SettingEntry
		if err := rows.Scan(&e.Key, &e.Value, &e.ValueType, &e.Source, &e.Description); err != nil {
			continue
		}
		result[e.Key] = e
	}
	return result, nil
}

// GetAllSettingsFlattened 获取扁平化 key→value 映射
func (w *workspaceDBImpl) GetAllSettingsFlattened() (map[string]any, error) {
	entries, err := w.GetAllSettings()
	if err != nil {
		return nil, cerr.Wrap(err, "storage: 获取配置失败")
	}

	result := make(map[string]any)
	for _, e := range entries {
		result[e.Key] = parseValue(e.Value, e.ValueType)
	}
	return result, nil
}

// BatchSetSettings 批量写入配置（事务内执行）
func (w *workspaceDBImpl) BatchSetSettings(entries []SettingEntry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	tx, err := w.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, e := range entries {
		if _, err := tx.Exec(`
			INSERT INTO settings (key, value, value_type, source, description, updated_at)
			VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(key) DO UPDATE SET
				value = excluded.value,
				value_type = excluded.value_type,
				source = excluded.source,
				description = excluded.description,
				updated_at = CURRENT_TIMESTAMP
		`, e.Key, e.Value, e.ValueType, e.Source, e.Description); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetAllSettingsMap 以 map[SettingEntry] 形式返回所有配置（供 UnflattenSettings 使用）
func (w *workspaceDBImpl) GetAllSettingsMap() map[string]SettingEntry {
	entries, err := w.GetAllSettings()
	if err != nil {
		return make(map[string]SettingEntry)
	}
	return entries
}

// DeleteSetting 删除配置项
func (w *workspaceDBImpl) DeleteSetting(key string) error {
	_, err := w.db.Exec("DELETE FROM settings WHERE key = ?", key)
	return err
}

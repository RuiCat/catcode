package storage

import (
	"database/sql"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// AgentRow — agent_definitions 表行结构
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// AgentRow agent_definitions 表的一行
type AgentRow struct {
	ID                   int64
	Name                 string
	DisplayName          string
	Type                 string // "agent" | "companion"
	Mode                 string // "primary" | "subagent" | "background"
	Description          string
	SystemPrompt         string
	ModelProvider        string
	ModelName            string
	ModelTemperature     *float64
	ThinkingEnabled      bool
	ThinkingBudgetTokens *int
	ModelLimitContext    *int
	ModelLimitOutput     *int
	PermissionJSON       string
	ToolsJSON            string
	TriggersJSON         string
	PersonaJSON          string
	StatesJSON           string
	Temperature          float64
	Source               string // "builtin" | "user_db" | "user_file"
	SourcePath           string
	Version              int
	Enabled              bool
}

// AgentDef 是 AgentRow 的别名，用于导出
type AgentDef = AgentRow

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Agent CRUD
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// GetAgentByName 按名称获取智能体定义
func (w *workspaceDBImpl) GetAgentByName(name string) (*AgentRow, error) {
	rows, err := w.db.Query(`
		SELECT id, name, display_name, type, mode, description, system_prompt,
			model_provider, model_name, model_temperature,
			thinking_enabled, thinking_budget_tokens,
			model_limit_context, model_limit_output,
			permission_json, tools_json, triggers_json,
			persona_json, states_json,
			temperature, source, source_path, version, enabled
		FROM agent_definitions WHERE name = ?
	`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, cerr.Newf("storage: agent %s 不存在", name)
	}

	return scanAgentRow(rows)
}

// GetAllAgentDefinitions 获取所有智能体定义
func (w *workspaceDBImpl) GetAllAgentDefinitions() ([]*AgentRow, error) {
	rows, err := w.db.Query(`
		SELECT id, name, display_name, type, mode, description, system_prompt,
			model_provider, model_name, model_temperature,
			thinking_enabled, thinking_budget_tokens,
			model_limit_context, model_limit_output,
			permission_json, tools_json, triggers_json,
			persona_json, states_json,
			temperature, source, source_path, version, enabled
		FROM agent_definitions WHERE enabled = 1
		ORDER BY CASE WHEN mode = 'primary' THEN 0 ELSE 1 END, name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*AgentRow
	for rows.Next() {
		a, err := scanAgentRow(rows)
		if err != nil {
			continue
		}
		result = append(result, a)
	}
	return result, nil
}

// UpsertAgent 插入或更新智能体定义（按 name 唯一）
func (w *workspaceDBImpl) UpsertAgent(row *AgentRow) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	_, err := w.db.Exec(`
		INSERT INTO agent_definitions (
			name, display_name, type, mode, description, system_prompt,
			model_provider, model_name, model_temperature,
			thinking_enabled, thinking_budget_tokens,
			model_limit_context, model_limit_output,
			permission_json, tools_json, triggers_json,
			persona_json, states_json,
			temperature, source, source_path, enabled, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(name) DO UPDATE SET
			display_name = excluded.display_name,
			type = excluded.type,
			mode = excluded.mode,
			description = excluded.description,
			system_prompt = excluded.system_prompt,
			model_provider = excluded.model_provider,
			model_name = excluded.model_name,
			model_temperature = excluded.model_temperature,
			thinking_enabled = excluded.thinking_enabled,
			thinking_budget_tokens = excluded.thinking_budget_tokens,
			model_limit_context = excluded.model_limit_context,
			model_limit_output = excluded.model_limit_output,
			permission_json = excluded.permission_json,
			tools_json = excluded.tools_json,
			triggers_json = excluded.triggers_json,
			persona_json = excluded.persona_json,
			states_json = excluded.states_json,
			temperature = excluded.temperature,
			source = excluded.source,
			source_path = excluded.source_path,
			enabled = excluded.enabled,
			version = agent_definitions.version + 1,
			updated_at = CURRENT_TIMESTAMP
	`,
		row.Name, row.DisplayName, row.Type, row.Mode, row.Description, row.SystemPrompt,
		row.ModelProvider, row.ModelName, row.ModelTemperature,
		boolToInt(row.ThinkingEnabled), row.ThinkingBudgetTokens,
		row.ModelLimitContext, row.ModelLimitOutput,
		row.PermissionJSON, row.ToolsJSON, row.TriggersJSON,
		row.PersonaJSON, row.StatesJSON,
		row.Temperature, row.Source, row.SourcePath, boolToInt(row.Enabled),
	)
	return err
}

// DeleteAgent 按名称删除智能体定义
func (w *workspaceDBImpl) DeleteAgent(name string) error {
	_, err := w.db.Exec("DELETE FROM agent_definitions WHERE name = ?", name)
	return err
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 内部辅助
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// insertAgentDefinition 将 AgentRow 写入 agent_definitions 表
func insertAgentDefinition(db *sql.DB, row *AgentRow) error {
	_, err := db.Exec(`
		INSERT INTO agent_definitions (
			name, display_name, type, mode, description, system_prompt,
			model_provider, model_name, model_temperature,
			thinking_enabled, thinking_budget_tokens,
			model_limit_context, model_limit_output,
			permission_json, tools_json, triggers_json,
			persona_json, states_json,
			temperature, source, source_path, enabled, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`,
		row.Name, row.DisplayName, row.Type, row.Mode, row.Description, row.SystemPrompt,
		row.ModelProvider, row.ModelName, row.ModelTemperature,
		boolToInt(row.ThinkingEnabled), row.ThinkingBudgetTokens,
		row.ModelLimitContext, row.ModelLimitOutput,
		row.PermissionJSON, row.ToolsJSON, row.TriggersJSON,
		row.PersonaJSON, row.StatesJSON,
		row.Temperature, row.Source, row.SourcePath, boolToInt(row.Enabled),
	)
	return err
}

// scanAgentRow 从 sql.Rows 扫描一行 AgentRow
func scanAgentRow(rows *sql.Rows) (*AgentRow, error) {
	var a AgentRow
	var mt sql.NullFloat64
	var tbt sql.NullInt64
	var mlc, mlo sql.NullInt64
	var thinkingEnabled int
	var enabled int

	err := rows.Scan(
		&a.ID, &a.Name, &a.DisplayName, &a.Type, &a.Mode, &a.Description, &a.SystemPrompt,
		&a.ModelProvider, &a.ModelName, &mt,
		&thinkingEnabled, &tbt,
		&mlc, &mlo,
		&a.PermissionJSON, &a.ToolsJSON, &a.TriggersJSON,
		&a.PersonaJSON, &a.StatesJSON,
		&a.Temperature, &a.Source, &a.SourcePath, &a.Version, &enabled,
	)
	if err != nil {
		return nil, err
	}

	a.ThinkingEnabled = thinkingEnabled != 0
	a.Enabled = enabled != 0

	if mt.Valid {
		a.ModelTemperature = &mt.Float64
	}
	if tbt.Valid {
		v := int(tbt.Int64)
		a.ThinkingBudgetTokens = &v
	}
	if mlc.Valid {
		v := int(mlc.Int64)
		a.ModelLimitContext = &v
	}
	if mlo.Valid {
		v := int(mlo.Int64)
		a.ModelLimitOutput = &v
	}

	return &a, nil
}

// boolToInt 将 bool 转换为 SQLite INTEGER
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

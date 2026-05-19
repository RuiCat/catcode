// Package storage 提供工作区 SQLite 数据库的封装，管理应用配置、会话记录、记忆与任务调度等持久化数据。
package storage

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	cerr "catcode/core/errors"
	"catcode/data/embed"

	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// WorkspaceDB — 工作区数据库
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// WorkspaceDB 封装工作区 SQLite 数据库的所有操作
// WorkspaceDB 接口 — 工作区数据库操作
type WorkspaceDB interface {
	GetSetting(key string) (value, valueType string, err error)
	SetSetting(key, value, valueType, source string) error
	GetAllSettingsFlattened() (map[string]any, error)
	Seed() error
	SaveConversation(conv *ConversationRow, messages []*MessageRow) error
	LoadConversation(id string) (*ConversationRow, []*MessageRow, error)
	ListConversations() ([]ConversationInfo, error)
	SetMemory(scope, key, content, description, memoryType, tags string, importance int) error
	GetMemory(scope, key string) (*MemoryEntry, error)
	ScanMemoryHeaders(scope string) ([]*MemoryHeader, error)
	FindRelevantMemories(scope, query string, limit int) ([]*MemoryEntry, error)
	DeleteMemory(scope, key string) error
	CreateSnapshot(convID, label, messagesJSON, summary string, tokenCount int) error
	LogError(category, severity, message, stackTrace, source, convID string) error
	GetAllAgentDefinitions() ([]*AgentRow, error)
	MarkTaskRun(id int64) error
	CreateScheduledTask(name, description string, intervalSec int) (*ScheduledTask, error)
	DeleteScheduledTask(id int64) error
	UpdateScheduledTask(id int64, name, description string, intervalSec int, enabled bool) error
	GetAllSettingsMap() map[string]SettingEntry
	DB() *sql.DB
	ListScheduledTasks() ([]*ScheduledTask, error)
	Close() error
}

type workspaceDBImpl struct {
	db *sql.DB
	mu sync.RWMutex
}

// OpenWorkspace 打开或创建指定工作区的数据库
// 返回 WorkspaceDB 实例，以及 isNew 标志（首次创建 = true）
func OpenWorkspace(workDir string) (WorkspaceDB, bool, error) {
	catcodeDir := filepath.Join(workDir, ".catcode")
	dbPath := filepath.Join(catcodeDir, "data.db")

	// 确保 .catcode/ 目录存在
	if err := os.MkdirAll(catcodeDir, 0755); err != nil {
		return nil, false, cerr.Wrap(err, "storage: 创建工作区目录失败")
	}

	// 检查数据库文件是否已存在（首次创建标志）
	_, statErr := os.Stat(dbPath)
	isNew := os.IsNotExist(statErr)

	// 打开数据库（WAL 模式 + busy_timeout）
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, false, cerr.Wrap(err, "storage: 打开数据库失败")
	}

	// 连接池配置：WAL 模式支持并发读，允许多连接
	// MaxOpenConns=5 允许嵌套查询和并发读操作
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)

	wdb := &workspaceDBImpl{db: db}

	// 执行 PRAGMA 并检查/初始化 schema
	if err := wdb.init(); err != nil {
		db.Close()
		return nil, false, err
	}

	return wdb, isNew, nil
}

// init 执行数据库初始化（PRAGMA + schema 迁移）
func (w *workspaceDBImpl) init() error {
	// 设置 PRAGMA
	if _, err := w.db.Exec(pragmas); err != nil {
		return cerr.Wrap(err, "storage: PRAGMA 设置失败")
	}

	// 检查并执行 schema 迁移
	if err := migrateDB(w.db); err != nil {
		return err
	}

	return nil
}

// Seed 种子数据写入（仅在新创建 DB 时调用）
// 将 go:embed 的默认配置和角色写入数据库
func (w *workspaceDBImpl) Seed() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 检查是否已写入种子数据
	var count int
	err := w.db.QueryRow("SELECT COUNT(*) FROM settings").Scan(&count)
	if err != nil {
		return cerr.Wrap(err, "storage: 检查 settings 失败")
	}
	if count > 0 {
		// 检查并迁移旧格式 model name（无 provider 前缀 → 加前缀）
		if err := w.migrateSettingsModelName(); err != nil {
			return cerr.Wrap(err, "storage: 迁移旧配置失败")
		}
		return nil
	}

	// 1. 写入默认配置
	if err := seedDefaultSettings(w.db); err != nil {
		return cerr.Wrap(err, "storage: 写入默认配置失败")
	}

	// 2. 写入默认角色
	if err := seedDefaultRoles(w.db); err != nil {
		return cerr.Wrap(err, "storage: 写入默认角色失败")
	}

	return nil
}

// migrateSettingsModelName 将旧格式 model（无 provider 前缀）迁移为新格式
// 旧: "deepseek-chat" → 新: "deepseek:deepseek-chat"
func (w *workspaceDBImpl) migrateSettingsModelName() error {
	var modelValue string
	err := w.db.QueryRow("SELECT value FROM settings WHERE key = 'model'").Scan(&modelValue)
	if err != nil {
		return nil // 无 model 设置，跳过
	}
	if !strings.Contains(modelValue, ":") {
		newModel := "deepseek:" + modelValue
		if _, err := w.db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'model'", newModel); err != nil {
			return cerr.Wrap(err, "更新 model 失败")
		}
	}

	// small_model 同理
	var smallModelValue string
	err = w.db.QueryRow("SELECT value FROM settings WHERE key = 'small_model'").Scan(&smallModelValue)
	if err != nil {
		return nil
	}
	if smallModelValue != "" && !strings.Contains(smallModelValue, ":") {
		newSmallModel := "deepseek:" + smallModelValue
		if _, err := w.db.Exec("UPDATE settings SET value = ?, updated_at = CURRENT_TIMESTAMP WHERE key = 'small_model'", newSmallModel); err != nil {
			return cerr.Wrap(err, "更新 small_model 失败")
		}
	}

	return nil
}

// seedDefaultSettings 从 embed.DefaultSettings 扁平化写入 settings 表
func seedDefaultSettings(db *sql.DB) error {
	entries := flattenMap("", embed.DefaultSettings)
	for _, e := range entries {
		if _, err := db.Exec(
			"INSERT INTO settings (key, value, value_type, source, description) VALUES (?, ?, ?, 'builtin', '')",
			e.Key, e.Value, e.ValueType,
		); err != nil {
			return cerr.Wrapf(err, "写入配置 %s 失败", e.Key)
		}
	}
	return nil
}

// seedDefaultRoles 从 embed.DefaultRolesFS 写入 agent_definitions 表
func seedDefaultRoles(db *sql.DB) error {
	entries, err := embed.DefaultRolesFS.ReadDir("roles")
	if err != nil {
		return cerr.Wrap(err, "读取嵌入角色目录失败")
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		data, err := embed.DefaultRolesFS.ReadFile("roles/" + entry.Name())
		if err != nil {
			continue
		}

		def, err := parseSeedRoleYAML(data, entry.Name())
		if err != nil {
			continue
		}

		if err := insertAgentDefinition(db, def); err != nil {
			return cerr.Wrapf(err, "写入角色 %s 失败", def.Name)
		}
	}

	return nil
}

// seedYAML 种子角色 YAML 解析用的中间结构体
type seedYAML struct {
	Name         string   `yaml:"name"`
	DisplayName  string   `yaml:"display_name"`
	Type         string   `yaml:"type"`
	Mode         string   `yaml:"mode"`
	Description  string   `yaml:"description"`
	SystemPrompt string   `yaml:"system_prompt"`
	Temperature  float64  `yaml:"temperature"`
	Tools        []string `yaml:"tools"`
	Model        struct {
		Provider    string  `yaml:"provider"`
		Name        string  `yaml:"name"`
		Temperature float64 `yaml:"temperature"`
		Thinking    struct {
			Enabled      bool `yaml:"enabled"`
			BudgetTokens int  `yaml:"budget_tokens"`
		} `yaml:"thinking"`
		Limit struct {
			Context int `yaml:"context"`
			Output  int `yaml:"output"`
		} `yaml:"limit"`
	} `yaml:"model"`
	Permission map[string]any `yaml:"permission"`
	Triggers   []struct {
		Event     string `yaml:"event"`
		Condition string `yaml:"condition"`
		Action    string `yaml:"action"`
		Message   string `yaml:"message"`
		Priority  int    `yaml:"priority"`
	} `yaml:"triggers"`
}

// parseSeedRoleYAML 使用 yaml.v3 解析角色 YAML 为 AgentRow
func parseSeedRoleYAML(data []byte, filename string) (*AgentRow, error) {
	var sy seedYAML
	if err := yaml.Unmarshal(data, &sy); err != nil {
		return nil, err
	}

	row := &AgentRow{
		Name:           sy.Name,
		DisplayName:    sy.DisplayName,
		Type:           sy.Type,
		Mode:           sy.Mode,
		Description:    sy.Description,
		SystemPrompt:   sy.SystemPrompt,
		Temperature:    sy.Temperature,
		ModelProvider:  sy.Model.Provider,
		ModelName:      sy.Model.Name,
		PermissionJSON: "{}",
		ToolsJSON:      "[]",
		TriggersJSON:   "[]",
		PersonaJSON:    "{}",
		StatesJSON:     "[]",
		Source:         "builtin",
		SourcePath:     filename,
		Enabled:        true,
	}

	if sy.Model.Temperature != 0 {
		row.ModelTemperature = &sy.Model.Temperature
	}

	if sy.Model.Thinking.Enabled || sy.Model.Thinking.BudgetTokens != 0 {
		row.ThinkingEnabled = sy.Model.Thinking.Enabled
		row.ThinkingBudgetTokens = &sy.Model.Thinking.BudgetTokens
	}

	if sy.Model.Limit.Context != 0 {
		row.ModelLimitContext = &sy.Model.Limit.Context
	}
	if sy.Model.Limit.Output != 0 {
		row.ModelLimitOutput = &sy.Model.Limit.Output
	}

	if len(sy.Tools) > 0 {
		if data, err := json.Marshal(sy.Tools); err == nil {
			row.ToolsJSON = string(data)
		}
	}

	if sy.Permission != nil {
		if data, err := json.Marshal(sy.Permission); err == nil {
			row.PermissionJSON = string(data)
		}
	}

	if len(sy.Triggers) > 0 {
		if data, err := json.Marshal(sy.Triggers); err == nil {
			row.TriggersJSON = string(data)
		}
	}

	return row, nil
}

// Close 关闭数据库连接
func (w *workspaceDBImpl) Close() error {
	return w.db.Close()
}

// DB 返回底层 sql.DB（供子 store 使用）
func (w *workspaceDBImpl) DB() *sql.DB {
	return w.db
}

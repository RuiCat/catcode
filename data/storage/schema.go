// Package storage — 工作区数据持久化
// 实现基于 SQLite 的完整数据存储，每个工作区独立 data.db
package storage

import (
	"database/sql"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// PRAGMA 配置
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// pragmas SQL 在每次打开数据库时执行
const pragmas = `
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;
PRAGMA cache_size = -20000;
PRAGMA synchronous = NORMAL;
PRAGMA mmap_size = 268435456;
`

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 完整 DDL
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// schemaV1 数据库 v1 完整 DDL
const schemaV1 = `
-- ============================================
-- 表 1: schema_version — 数据库版本管理
-- ============================================
CREATE TABLE IF NOT EXISTS schema_version (
    version     INTEGER PRIMARY KEY,
    applied_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    description TEXT NOT NULL DEFAULT ''
);

-- ============================================
-- 表 2: settings — 工作区配置（键值对模型）
-- ============================================
CREATE TABLE IF NOT EXISTS settings (
    key         TEXT PRIMARY KEY,
    value       TEXT NOT NULL DEFAULT '',
    value_type  TEXT NOT NULL DEFAULT 'string',
    source      TEXT NOT NULL DEFAULT 'builtin',
    description TEXT NOT NULL DEFAULT '',
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_settings_source ON settings(source);

-- ============================================
-- 表 3: agent_definitions — 智能体定义
-- ============================================
CREATE TABLE IF NOT EXISTS agent_definitions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT NOT NULL UNIQUE,

    -- 基础信息
    display_name TEXT NOT NULL DEFAULT '',
    type        TEXT NOT NULL DEFAULT 'agent',
    mode        TEXT NOT NULL DEFAULT 'subagent',
    description TEXT NOT NULL DEFAULT '',
    system_prompt TEXT NOT NULL DEFAULT '',

    -- 模型配置
    model_provider    TEXT NOT NULL DEFAULT 'deepseek',
    model_name        TEXT NOT NULL DEFAULT 'deepseek-chat',
    model_temperature REAL,
    thinking_enabled      INTEGER NOT NULL DEFAULT 0,
    thinking_budget_tokens INTEGER,
    model_limit_context INTEGER,
    model_limit_output  INTEGER,

    -- JSON 列: 权限、工具、触发器
    permission_json TEXT NOT NULL DEFAULT '{}',
    tools_json      TEXT NOT NULL DEFAULT '[]',
    triggers_json   TEXT NOT NULL DEFAULT '[]',

    -- 陪伴型角色特有
    persona_json TEXT NOT NULL DEFAULT '{}',
    states_json  TEXT NOT NULL DEFAULT '[]',

    -- 温度（角色级顶层覆盖）
    temperature REAL NOT NULL DEFAULT 0.1,

    -- 来源与元数据
    source      TEXT NOT NULL DEFAULT 'builtin',
    source_path TEXT NOT NULL DEFAULT '',
    version     INTEGER NOT NULL DEFAULT 1,
    enabled     INTEGER NOT NULL DEFAULT 1,
    created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_agent_defs_name    ON agent_definitions(name);
CREATE INDEX IF NOT EXISTS idx_agent_defs_type    ON agent_definitions(type);
CREATE INDEX IF NOT EXISTS idx_agent_defs_mode    ON agent_definitions(mode);
CREATE INDEX IF NOT EXISTS idx_agent_defs_source  ON agent_definitions(source);
CREATE INDEX IF NOT EXISTS idx_agent_defs_enabled ON agent_definitions(enabled);

-- ============================================
-- 表 4: conversations — 对话会话
-- ============================================
CREATE TABLE IF NOT EXISTS conversations (
    id              TEXT PRIMARY KEY,
    model           TEXT NOT NULL DEFAULT '',
    system_prompt   TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL DEFAULT '',
    compress_threshold INTEGER NOT NULL DEFAULT 80000,
    metadata_json   TEXT NOT NULL DEFAULT '{}',
    message_count   INTEGER NOT NULL DEFAULT 0,
    token_count     INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_conversations_updated ON conversations(updated_at DESC);
CREATE INDEX IF NOT EXISTS idx_conversations_created ON conversations(created_at DESC);

-- ============================================
-- 表 5: messages — 对话消息
-- ============================================
CREATE TABLE IF NOT EXISTS messages (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL,
    seq             INTEGER NOT NULL DEFAULT 0,
    role            TEXT NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    name            TEXT NOT NULL DEFAULT '',
    tool_call_id    TEXT NOT NULL DEFAULT '',
    tool_calls_json TEXT NOT NULL DEFAULT '[]',
    reasoning_content TEXT NOT NULL DEFAULT '',
    enabled          INTEGER NOT NULL DEFAULT 1,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_messages_conv    ON messages(conversation_id, seq);
CREATE INDEX IF NOT EXISTS idx_messages_content ON messages(content);

-- ============================================
-- 表 6: memory — 长期记忆条目（多级索引）
-- ============================================
CREATE TABLE IF NOT EXISTS memory (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    scope           TEXT NOT NULL DEFAULT 'workspace',  -- global / workspace
    key             TEXT NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',       -- 简短描述（用于索引扫描）
    memory_type     TEXT NOT NULL DEFAULT 'project', -- user/feedback/project/reference
    tags            TEXT NOT NULL DEFAULT '',
    importance      INTEGER NOT NULL DEFAULT 0,
    access_count    INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    accessed_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(scope, key)
);
CREATE INDEX IF NOT EXISTS idx_memory_scope       ON memory(scope);
CREATE INDEX IF NOT EXISTS idx_memory_key         ON memory(key);
CREATE INDEX IF NOT EXISTS idx_memory_type        ON memory(memory_type);
CREATE INDEX IF NOT EXISTS idx_memory_tags        ON memory(tags);
CREATE INDEX IF NOT EXISTS idx_memory_accessed     ON memory(accessed_at DESC);
CREATE INDEX IF NOT EXISTS idx_memory_importance  ON memory(importance DESC);

-- ============================================
-- 表 7: context_snapshots — 上下文压缩快照
-- ============================================
CREATE TABLE IF NOT EXISTS context_snapshots (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    conversation_id TEXT NOT NULL,
    label           TEXT NOT NULL DEFAULT '',
    messages_json   TEXT NOT NULL DEFAULT '[]',
    summary         TEXT NOT NULL DEFAULT '',
    token_count     INTEGER NOT NULL DEFAULT 0,
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (conversation_id) REFERENCES conversations(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_snapshots_conv ON context_snapshots(conversation_id);

-- ============================================
-- 表 8: scheduled_tasks — 周期任务
-- ============================================
CREATE TABLE IF NOT EXISTS scheduled_tasks (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    interval_seconds INTEGER NOT NULL DEFAULT 300,
    enabled          INTEGER NOT NULL DEFAULT 1,
    last_run         DATETIME,
    next_run         DATETIME,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- ============================================
-- 表 9: error_logs — 错误日志
-- ============================================
CREATE TABLE IF NOT EXISTS error_logs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    category        TEXT NOT NULL DEFAULT '',       -- 错误类别 (API/工具/权限/LLM/网络/内部)
    severity        TEXT NOT NULL DEFAULT 'error',  -- error / warning / info
    message         TEXT NOT NULL DEFAULT '',       -- 错误消息
    stack_trace     TEXT NOT NULL DEFAULT '',       -- 堆栈跟踪
    source          TEXT NOT NULL DEFAULT '',       -- 来源 (architect/subagent/llm/startup)
    conversation_id TEXT NOT NULL DEFAULT '',       -- 关联会话ID
    created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_error_logs_category ON error_logs(category);
CREATE INDEX IF NOT EXISTS idx_error_logs_created  ON error_logs(created_at DESC);
`

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Schema 版本管理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

const currentSchemaVersion = 7

// initSchema 创建所有表
func initSchema(db *sql.DB) error {
	if _, err := db.Exec(pragmas); err != nil {
		return cerr.Wrap(err, "storage: PRAGMA 设置失败")
	}

	if _, err := db.Exec(schemaV1); err != nil {
		return cerr.Wrap(err, "storage: 创建 schema 失败")
	}

	// 记录当前版本
	if _, err := db.Exec(
		"INSERT INTO schema_version (version, description) VALUES (?, ?)",
		currentSchemaVersion, "初始 schema: 9 张表 + 多级记忆索引 + scope",
	); err != nil {
		return cerr.Wrap(err, "storage: 记录 schema_version 失败")
	}

	return nil
}

// migrateDB 检查并执行 schema 迁移
func migrateDB(db *sql.DB) error {
	// 检查 schema_version 表是否存在
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_version'").Scan(&count)
	if err != nil {
		return cerr.Wrap(err, "storage: 检查 schema_version 失败")
	}

	if count == 0 {
		// 新数据库，创建全部 schema
		return initSchema(db)
	}

	// 已存在，检查版本
	var version int
	err = db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return cerr.Wrap(err, "storage: 查询 schema 版本失败")
	}

	// v1 → v2: memory 表添加多级索引字段
	if version < 2 {
		// 添加新列（SQLite ALTER TABLE 只支持 ADD COLUMN）
		// description 列
		exists, err := columnExists(db, "memory", "description")
		if err != nil {
			return cerr.Wrap(err, "storage: v1→v2 columnExists failed")
		}
		if !exists {
			if _, err := db.Exec(`ALTER TABLE memory ADD COLUMN description TEXT NOT NULL DEFAULT ''`); err != nil {
				return cerr.Wrap(err, "storage: v1→v2 migration ALTER TABLE failed")
			}
		}
		// memory_type 列
		exists, err = columnExists(db, "memory", "memory_type")
		if err != nil {
			return cerr.Wrap(err, "storage: v1→v2 columnExists failed")
		}
		if !exists {
			if _, err := db.Exec(`ALTER TABLE memory ADD COLUMN memory_type TEXT NOT NULL DEFAULT 'project'`); err != nil {
				return cerr.Wrap(err, "storage: v1→v2 migration ALTER TABLE failed")
			}
		}
		// 创建新索引
		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_memory_type ON memory(memory_type)"); err != nil {
			return cerr.Wrap(err, "storage: v1→v2 index creation failed")
		}
		if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_memory_importance ON memory(importance DESC)"); err != nil {
			return cerr.Wrap(err, "storage: v1→v2 index creation failed")
		}
		if err := SetSchemaVersion(db, 2, "v2: 多级记忆索引 (type + description)"); err != nil {
			return cerr.Wrap(err, "storage: v1→v2 version record failed")
		}
	}

	if version < currentSchemaVersion {
		// v2 → v3: memory 表新增 scope 字段（global / workspace）
		if version < 3 {
			exists, err := columnExists(db, "memory", "scope")
			if err != nil {
				return cerr.Wrap(err, "storage: v2→v3 columnExists failed")
			}
			if !exists {
				if _, err := db.Exec(`ALTER TABLE memory ADD COLUMN scope TEXT NOT NULL DEFAULT 'workspace'`); err != nil {
					return cerr.Wrap(err, "storage: v2→v3 migration ALTER TABLE failed")
				}
			}
			if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_memory_scope ON memory(scope)"); err != nil {
				return cerr.Wrap(err, "storage: v2→v3 index creation failed")
			}
			if err := SetSchemaVersion(db, 3, "v3: memory 表新增 scope 字段 (global/workspace)"); err != nil {
				return cerr.Wrap(err, "storage: v2→v3 version record failed")
			}
		}
		// v3 → v4: memory 表 UNIQUE(key) 修正为 UNIQUE(scope, key)
		if version < 4 {
			if err := migrateMemoryUniqueV4(db); err != nil {
				return cerr.Wrap(err, "storage: v3→v4 migration failed")
			}
			if err := SetSchemaVersion(db, 4, "v4: memory UNIQUE(scope,key) 替代 UNIQUE(key)"); err != nil {
				return cerr.Wrap(err, "storage: v3→v4 version record failed")
			}
		}
		// v4 → v5: messages 表添加 reasoning_content 和 enabled 列
		if version < 5 {
			// reasoning_content 列
			exists, err := columnExists(db, "messages", "reasoning_content")
			if err != nil {
				return cerr.Wrap(err, "storage: v4→v5 columnExists failed")
			}
			if !exists {
				if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN reasoning_content TEXT NOT NULL DEFAULT ''`); err != nil {
					return cerr.Wrap(err, "storage: v4→v5 migration ALTER TABLE failed")
				}
			}
			// enabled 列
			exists, err = columnExists(db, "messages", "enabled")
			if err != nil {
				return cerr.Wrap(err, "storage: v4→v5 columnExists failed")
			}
			if !exists {
				if _, err := db.Exec(`ALTER TABLE messages ADD COLUMN enabled INTEGER NOT NULL DEFAULT 1`); err != nil {
					return cerr.Wrap(err, "storage: v4→v5 migration ALTER TABLE failed")
				}
			}
			if err := SetSchemaVersion(db, 5, "v5: messages 表新增 reasoning_content + enabled 列"); err != nil {
				return cerr.Wrap(err, "storage: v4→v5 version record failed")
			}
		}

		// v5 → v6: 新增 error_logs 表
		if version < 6 {
			if _, err := db.Exec(`
			CREATE TABLE IF NOT EXISTS error_logs (
				id              INTEGER PRIMARY KEY AUTOINCREMENT,
				category        TEXT NOT NULL DEFAULT '',
				severity        TEXT NOT NULL DEFAULT 'error',
				message         TEXT NOT NULL DEFAULT '',
				stack_trace     TEXT NOT NULL DEFAULT '',
				source          TEXT NOT NULL DEFAULT '',
				conversation_id TEXT NOT NULL DEFAULT '',
				created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
			CREATE INDEX IF NOT EXISTS idx_error_logs_category ON error_logs(category);
			CREATE INDEX IF NOT EXISTS idx_error_logs_created  ON error_logs(created_at DESC);
		`); err != nil {
				return cerr.Wrap(err, "storage: v5→v6 migration failed")
			}
			if err := SetSchemaVersion(db, 6, "v6: 新增 error_logs 表"); err != nil {
				return cerr.Wrap(err, "storage: v5→v6 version record failed")
			}
		}

		// v6 → v7: scheduled_tasks 表新增 run_once 列
		if version < 7 {
			exists, err := columnExists(db, "scheduled_tasks", "run_once")
			if err != nil {
				return cerr.Wrap(err, "storage: v6→v7 columnExists failed")
			}
			if !exists {
				if _, err := db.Exec(`ALTER TABLE scheduled_tasks ADD COLUMN run_once INTEGER NOT NULL DEFAULT 0`); err != nil {
					return cerr.Wrap(err, "storage: v6→v7 migration ALTER TABLE failed")
				}
			}
			if err := SetSchemaVersion(db, 7, "v7: scheduled_tasks 表新增 run_once 列"); err != nil {
				return cerr.Wrap(err, "storage: v6→v7 version record failed")
			}
		}
	}

	return nil
}

// SetSchemaVersion 用于未来升级时记录新版本
func SetSchemaVersion(db *sql.DB, version int, description string) error {
	_, err := db.Exec(
		"INSERT INTO schema_version (version, description) VALUES (?, ?)",
		version, description,
	)
	return err
}

// columnExists 检查指定表是否存在某列
func columnExists(db *sql.DB, table, column string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?", table, column).Scan(&count)
	return count > 0, err
}

// migrateMemoryUniqueV4 v3→v4: 将 memory 表的唯一约束从 UNIQUE(key) 改为 UNIQUE(scope, key)
// SQLite 不支持 ALTER TABLE DROP CONSTRAINT，需要重建表
func migrateMemoryUniqueV4(db *sql.DB) error {
	// 在事务中执行
	tx, err := db.Begin()
	if err != nil {
		return cerr.Wrap(err, "开始事务失败")
	}
	defer tx.Rollback()

	// 检查当前表结构，如果已经有 UNIQUE(scope, key) 则跳过
	var hasCompositeUnique int
	err = tx.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master 
		WHERE type='table' AND name='memory' 
		AND sql LIKE '%UNIQUE(scope, key)%'
	`).Scan(&hasCompositeUnique)
	if err == nil && hasCompositeUnique > 0 {
		return tx.Commit() // 已经迁移过
	}

	// 1. 创建新表（正确约束）
	_, err = tx.Exec(`
		CREATE TABLE memory_new (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			scope           TEXT NOT NULL DEFAULT 'workspace',
			key             TEXT NOT NULL,
			content         TEXT NOT NULL DEFAULT '',
			description     TEXT NOT NULL DEFAULT '',
			memory_type     TEXT NOT NULL DEFAULT 'project',
			tags            TEXT NOT NULL DEFAULT '',
			importance      INTEGER NOT NULL DEFAULT 0,
			access_count    INTEGER NOT NULL DEFAULT 0,
			created_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			accessed_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(scope, key)
		)
	`)
	if err != nil {
		return cerr.Wrap(err, "创建 memory_new 表失败")
	}

	// 2. 复制数据
	_, err = tx.Exec(`
		INSERT INTO memory_new 
		SELECT id, scope, key, content, description, memory_type, tags,
			importance, access_count, created_at, updated_at, accessed_at
		FROM memory
	`)
	if err != nil {
		return cerr.Wrap(err, "复制 memory 数据失败")
	}

	// 3. 删除旧表
	_, err = tx.Exec("DROP TABLE memory")
	if err != nil {
		return cerr.Wrap(err, "删除旧 memory 表失败")
	}

	// 4. 重命名新表
	_, err = tx.Exec("ALTER TABLE memory_new RENAME TO memory")
	if err != nil {
		return cerr.Wrap(err, "重命名 memory_new 失败")
	}

	// 5. 重建索引
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_memory_scope ON memory(scope)",
		"CREATE INDEX IF NOT EXISTS idx_memory_key ON memory(key)",
		"CREATE INDEX IF NOT EXISTS idx_memory_type ON memory(memory_type)",
		"CREATE INDEX IF NOT EXISTS idx_memory_tags ON memory(tags)",
		"CREATE INDEX IF NOT EXISTS idx_memory_accessed ON memory(accessed_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_memory_importance ON memory(importance DESC)",
	}
	for _, idx := range indexes {
		if _, err := tx.Exec(idx); err != nil {
			return cerr.Wrap(err, "重建索引失败")
		}
	}

	return tx.Commit()
}

// Package role 实现侧加载角色系统
// 角色 = 智能体配置 + 提示词，从 DB + 文件系统动态加载
package role

import (
	"time"

	cerr "catcode/core/errors"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 角色定义类型
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// RoleType 角色类型
type RoleType string

const (
	RoleAgent     RoleType = "agent"     // 功能型智能体
	RoleCompanion RoleType = "companion" // 陪伴型角色
)

// RoleMode 角色运行模式
type RoleMode string

const (
	ModePrimary    RoleMode = "primary"    // 主智能体（只有一个）
	ModeSubAgent   RoleMode = "subagent"   // 子智能体（被主智能体委派）
	ModeBackground RoleMode = "background" // 后台角色（事件驱动）
)

// RoleDef 角色定义（从文件加载）
type RoleDef struct {
	Name         string         `json:"name" yaml:"name"`
	DisplayName  string         `json:"display_name" yaml:"display_name"`
	Type         RoleType       `json:"type" yaml:"type"`
	Mode         RoleMode       `json:"mode" yaml:"mode"`
	Description  string         `json:"description" yaml:"description"`
	SystemPrompt string         `json:"system_prompt" yaml:"system_prompt"`
	Model        ModelConfig    `json:"model" yaml:"model"`
	Temperature  float64        `json:"temperature" yaml:"temperature"`
	Permission   map[string]any `json:"permission" yaml:"permission"`
	Tools        []string       `json:"tools" yaml:"tools"`
	Triggers     []TriggerDef   `json:"triggers" yaml:"triggers"`

	// 陪伴型角色特有
	Persona map[string]int `json:"persona,omitempty" yaml:"persona,omitempty"`
	States  []StateDef     `json:"states,omitempty" yaml:"states,omitempty"`

	// 元数据
	SourcePath string    `json:"-" yaml:"-"`
	LoadedAt   time.Time `json:"-" yaml:"-"`
}

// ModelConfig 模型配置
type ModelConfig struct {
	Provider    string          `json:"provider" yaml:"provider"`
	Name        string          `json:"name" yaml:"name"`
	Temperature float64         `json:"temperature" yaml:"temperature"`
	Thinking    *ThinkingConfig `json:"thinking,omitempty" yaml:"thinking,omitempty"`
	Limit       *ModelLimit     `json:"limit,omitempty" yaml:"limit,omitempty"`
}

// ThinkingConfig thinking 模式
type ThinkingConfig struct {
	Enabled      bool `json:"enabled" yaml:"enabled"`
	BudgetTokens int  `json:"budget_tokens" yaml:"budget_tokens"`
}

// ModelLimit 模型限制
type ModelLimit struct {
	Context int `json:"context" yaml:"context"`
	Output  int `json:"output" yaml:"output"`
}

// TriggerDef 触发器定义
type TriggerDef struct {
	Event     string `json:"event" yaml:"event"`
	Condition string `json:"condition,omitempty" yaml:"condition,omitempty"`
	Action    string `json:"action" yaml:"action"`
	Message   string `json:"message,omitempty" yaml:"message,omitempty"`
	Priority  int    `json:"priority" yaml:"priority"`
}

// StateDef 状态定义（陪伴型角色）
type StateDef struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	RangeMin    int    `json:"range_min" yaml:"range_min"`
	RangeMax    int    `json:"range_max" yaml:"range_max"`
	Description string `json:"description" yaml:"description"`
}

// Validate 校验角色定义合法性
func (d *RoleDef) Validate() error {
	if d.Name == "" {
		return cerr.New("role: name 不能为空")
	}
	if d.Type != RoleAgent && d.Type != RoleCompanion {
		return cerr.Newf("role: 未知的 type: %s", d.Type)
	}
	if d.Mode != ModePrimary && d.Mode != ModeSubAgent && d.Mode != ModeBackground {
		return cerr.Newf("role: 未知的 mode: %s", d.Mode)
	}
	return nil
}

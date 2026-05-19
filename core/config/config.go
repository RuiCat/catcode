// Package config 实现配置管理系统
//
// 优先级（从低到高）：
//
//	default_settings.json → DB settings 表 → 环境变量 (CATCODE_*) → CLI 参数
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"catcode/ai/llm"
	"catcode/core/config/loader"
	cerr "catcode/core/errors"
	"catcode/data/storage"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 配置结构体
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Config 全局配置结构
type Config struct {
	// 模型配置
	Model      string `json:"model"`       // 默认模型
	SmallModel string `json:"small_model"` // 轻量模型（探索/摘要用）

	// 提供商配置
	Providers map[string]ProviderConfig `json:"providers"`

	// Agent 配置
	DefaultAgent string                 `json:"default_agent"` // 默认主智能体
	Agents       map[string]AgentConfig `json:"agents"`

	// 侧加载角色配置
	RolePaths []string `json:"role_paths"` // 角色定义文件搜索路径

	// 权限配置
	Permissions map[string]any `json:"permissions"`

	// TUI 配置
	TUI TUIConfig `json:"tui"`

	// MCP 服务器配置
	MCPServers []MCPServerConfig `json:"mcp_servers"`

	// LSP 配置
	LSP map[string]LSPConfig `json:"lsp"`
}

// ProviderConfig 模型提供商配置
type ProviderConfig struct {
	Name    string                 `json:"name"`     // 提供商名称
	BaseURL string                 `json:"base_url"` // API 基础 URL
	APIKey  string                 `json:"api_key"`  // API 密钥
	Models  map[string]ModelConfig `json:"models"`   // 模型列表
	Options map[string]any         `json:"options"`  // 额外选项
}

// ModelConfig 模型配置
type ModelConfig struct {
	Name    string         `json:"name"`
	Options map[string]any `json:"options,omitempty"`
	Limit   *ModelLimit    `json:"limit,omitempty"`
}

// ModelLimit 模型限制
type ModelLimit struct {
	Context int `json:"context"` // 上下文窗口大小
	Output  int `json:"output"`  // 最大输出 token
}

// AgentConfig 智能体配置
type AgentConfig struct {
	Description string         `json:"description"`
	Mode        string         `json:"mode"` // primary / subagent / background
	Model       string         `json:"model"`
	Temperature float64        `json:"temperature"`
	Permission  map[string]any `json:"permission"`
	Color       string         `json:"color"`
}

// TUIConfig TUI 配置
type TUIConfig struct {
	Theme       string  `json:"theme"`        // 主题: dark / light
	FontSize    int     `json:"font_size"`    // 字体大小
	ChatRatio   float64 `json:"chat_ratio"`   // 聊天面板比例
	EnableMouse bool    `json:"enable_mouse"` // 是否启用鼠标
}

// MCPServerConfig MCP 服务器配置
type MCPServerConfig struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env,omitempty"`
}

// LSPConfig LSP 配置
type LSPConfig struct {
	Command    []string `json:"command"`
	Extensions []string `json:"extensions"`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 配置源类型（实现 loader 数据源语义）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DBSource 从 WorkspaceDB 加载配置
type DBSource struct {
	wdb storage.WorkspaceDB
}

func (s *DBSource) Name() string  { return "database" }
func (s *DBSource) Priority() int { return 10 }
func (s *DBSource) Load() (map[string]any, error) {
	return storage.UnflattenSettings(s.wdb.GetAllSettingsMap()), nil
}

// EnvSource 从环境变量加载配置
type EnvSource struct{}

func (s *EnvSource) Name() string  { return "environment" }
func (s *EnvSource) Priority() int { return 20 }
func (s *EnvSource) Load() (map[string]any, error) {
	return collectEnvOverrides(), nil
}

// newDBSource 创建 DB 配置源（适配 loader.Source 结构体）
func newDBSource(wdb storage.WorkspaceDB) loader.Source {
	s := &DBSource{wdb: wdb}
	return loader.Source{
		Name:     s.Name(),
		Priority: s.Priority(),
		Load:     s.Load,
	}
}

// newEnvSource 创建环境变量配置源（适配 loader.Source 结构体）
func newEnvSource() loader.Source {
	s := &EnvSource{}
	return loader.Source{
		Name:     s.Name(),
		Priority: s.Priority(),
		Load:     s.Load,
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 从工作区加载
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// LoadFromWorkspace 从工作区 DB + 环境变量 + CLI 加载配置
func LoadFromWorkspace(wdb storage.WorkspaceDB, cliModel string, cliTemp float64) (*Config, error) {
	l := loader.New()
	l.AddSource(newDBSource(wdb))
	l.AddSource(newEnvSource())

	var cfg Config
	if err := l.LoadInto(&cfg); err != nil {
		return nil, err
	}

	// CLI 参数覆盖（最高优先级）
	applyCLIOverrides(&cfg, cliModel, cliTemp)

	// CATCODE_BASE_URL 环境变量覆盖所有 provider（需要已加载的 providers）
	applyBaseURLEnvOverride(&cfg)

	// 后处理：确保 API Key 从环境变量补充
	ensureAPIKeys(&cfg)

	return &cfg, cfg.Validate()
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 环境变量覆盖
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// collectEnvOverrides 收集环境变量覆盖为 map（供 EnvSource 使用）
// 仅处理可在数据源层面合并的覆盖：CATCODE_MODEL、CATCODE_THEME
// CATCODE_BASE_URL 需要遍历已加载的 providers，在 applyBaseURLEnvOverride 中处理
func collectEnvOverrides() map[string]any {
	result := make(map[string]any)

	if v := os.Getenv("CATCODE_MODEL"); v != "" {
		result["model"] = v
	}
	if v := os.Getenv("CATCODE_THEME"); v != "" {
		if _, ok := result["tui"]; !ok {
			result["tui"] = make(map[string]any)
		}
		result["tui"].(map[string]any)["theme"] = v
	}

	return result
}

// applyBaseURLEnvOverride 将 CATCODE_BASE_URL 应用到所有 provider
func applyBaseURLEnvOverride(cfg *Config) {
	if v := os.Getenv("CATCODE_BASE_URL"); v != "" {
		for name, p := range cfg.Providers {
			p.BaseURL = v
			cfg.Providers[name] = p
		}
	}
}

// applyCLIOverrides 应用 CLI 参数覆盖
func applyCLIOverrides(cfg *Config, cliModel string, cliTemp float64) {
	if cliModel != "" {
		cfg.Model = cliModel
	}
	if cliTemp > 0 {
		for name, agent := range cfg.Agents {
			agent.Temperature = cliTemp
			cfg.Agents[name] = agent
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ensureAPIKeys 确保 API Key 已设置
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func ensureAPIKeys(cfg *Config) {
	for name, p := range cfg.Providers {
		if p.APIKey == "" {
			envVar := strings.ToUpper(name) + "_API_KEY"
			if v := os.Getenv(envVar); v != "" {
				p.APIKey = v
				cfg.Providers[name] = p
			}
		}
	}

	// 通用回退
	if v := os.Getenv("OPENAI_API_KEY"); v != "" {
		for name, p := range cfg.Providers {
			if p.APIKey == "" {
				p.APIKey = v
				cfg.Providers[name] = p
			}
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 校验
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Validate 校验配置合法性
func (c *Config) Validate() error {
	if c.Model == "" {
		return cerr.New("config: model 不能为空")
	}
	provider, modelName := llm.ParseModelName(c.Model)
	if provider == "" || modelName == "" {
		return cerr.Newf("config: model 必须是 \"provider:modelname\" 格式（两侧均不能为空），当前: %s", c.Model)
	}
	if c.SmallModel != "" {
		sp, sm := llm.ParseModelName(c.SmallModel)
		if sp == "" || sm == "" {
			return cerr.Newf("config: small_model 必须是 \"provider:modelname\" 格式（两侧均不能为空），当前: %s", c.SmallModel)
		}
	}
	if c.DefaultAgent == "" {
		return cerr.New("config: default_agent 不能为空")
	}
	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 序列化
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ToJSON 将配置序列化为 JSON 字符串
func (c *Config) ToJSON() (string, error) {
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return "", cerr.Wrap(err, "config: 序列化失败")
	}
	return string(data), nil
}

// SaveTo 保存配置到文件
func (c *Config) SaveTo(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return cerr.Wrapf(err, "config: 创建目录失败 %s", dir)
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return cerr.Wrap(err, "config: 序列化失败")
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return cerr.Wrapf(err, "config: 写入文件失败 %s", path)
	}

	return nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 便捷方法
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// GetProvider 获取指定名称的提供商配置
func (c *Config) GetProvider(name string) (ProviderConfig, bool) {
	p, ok := c.Providers[name]
	return p, ok
}

// GetAgent 获取指定名称的智能体配置
func (c *Config) GetAgent(name string) (AgentConfig, bool) {
	a, ok := c.Agents[name]
	return a, ok
}

// DefaultProvider 从 Model 解析默认提供商
func (c *Config) DefaultProvider() ProviderConfig {
	providerName, _ := llm.ParseModelName(c.Model)
	if providerName != "" {
		if p, ok := c.Providers[providerName]; ok {
			return p
		}
	}
	// 回退：返回第一个 provider
	for _, p := range c.Providers {
		return p
	}
	return ProviderConfig{}
}

// ResolveModel 从 Config.Model 解析 provider 配置和纯模型名
func (c *Config) ResolveModel() (ProviderConfig, string) {
	return c.resolveModelName(c.Model)
}

// ResolveSmallModel 从 Config.SmallModel 解析 provider 配置和纯模型名
func (c *Config) ResolveSmallModel() (ProviderConfig, string) {
	if c.SmallModel == "" {
		return c.ResolveModel()
	}
	return c.resolveModelName(c.SmallModel)
}

func (c *Config) resolveModelName(fullName string) (ProviderConfig, string) {
	providerName, modelName := llm.ParseModelName(fullName)
	if providerName == "" {
		return c.DefaultProvider(), modelName
	}
	if p, ok := c.Providers[providerName]; ok {
		return p, modelName
	}
	return c.DefaultProvider(), modelName
}

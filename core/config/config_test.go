package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"catcode/data/embed"
)

func loadDefaults(cliModel string, cliTemp float64) *Config {
	cfg := &Config{}
	defaults := embed.DefaultSettings
	jsonData, _ := json.Marshal(defaults)
	json.Unmarshal(jsonData, cfg)
	// 环境变量覆盖（与 LoadFromWorkspace 行为一致）
	applyBaseURLEnvOverride(cfg)
	if v := os.Getenv("CATCODE_MODEL"); v != "" {
		cfg.Model = v
	}
	if v := os.Getenv("CATCODE_THEME"); v != "" {
		cfg.TUI.Theme = v
	}
	if cliModel != "" {
		cfg.Model = cliModel
	}
	if cliTemp > 0 {
		for name, agent := range cfg.Agents {
			agent.Temperature = cliTemp
			cfg.Agents[name] = agent
		}
	}
	ensureAPIKeys(cfg)
	cfg.Validate()
	return cfg
}

func TestLoadDefaults(t *testing.T) {
	cfg := loadDefaults("", 0)

	if cfg.Model != "deepseek:deepseek-chat" {
		t.Errorf("期望默认 model=deepseek:deepseek-chat, 得到 %s", cfg.Model)
	}
	if cfg.DefaultAgent != "architect" {
		t.Errorf("期望默认 default_agent=architect, 得到 %s", cfg.DefaultAgent)
	}
	if cfg.TUI.Theme != "dark" {
		t.Errorf("期望默认 tui.theme=dark, 得到 %s", cfg.TUI.Theme)
	}
}

func TestLoadWithCLIOverrides(t *testing.T) {
	cfg := loadDefaults("deepseek:gpt-4", 0.7)

	if cfg.Model != "deepseek:gpt-4" {
		t.Errorf("期望 model=deepseek:gpt-4, 得到 %s", cfg.Model)
	}
}

func TestValidate(t *testing.T) {
	cfg := &Config{}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("空配置应校验失败")
	}

	cfg.Model = "test:test-model"
	cfg.DefaultAgent = "test-agent"
	err = cfg.Validate()
	if err != nil {
		t.Fatalf("有效配置校验失败: %v", err)
	}
}

func TestGetProvider(t *testing.T) {
	cfg := loadDefaults("", 0)

	p, ok := cfg.GetProvider("deepseek")
	if !ok {
		t.Fatal("应能找到 deepseek provider")
	}
	if p.Name != "DeepSeek" {
		t.Errorf("期望 provider name=DeepSeek, 得到 %s", p.Name)
	}

	_, ok = cfg.GetProvider("nonexistent")
	if ok {
		t.Fatal("不应找到不存在的 provider")
	}
}

func TestGetAgent(t *testing.T) {
	cfg := loadDefaults("", 0)

	a, ok := cfg.GetAgent("architect")
	if !ok {
		t.Fatal("应能找到 architect agent")
	}
	if a.Mode != "primary" {
		t.Errorf("期望 agent mode=primary, 得到 %s", a.Mode)
	}
}

func TestDefaultProvider(t *testing.T) {
	cfg := loadDefaults("", 0)

	p := cfg.DefaultProvider()
	if p.Name == "" {
		t.Fatal("默认 provider 不应为空")
	}
}

func TestToJSON(t *testing.T) {
	cfg := loadDefaults("", 0)

	jsonStr, err := cfg.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON() 失败: %v", err)
	}
	if jsonStr == "" {
		t.Fatal("JSON 不应为空")
	}
}

func TestSaveTo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_config.json")

	cfg := loadDefaults("", 0)

	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("SaveTo() 失败: %v", err)
	}

	// 验证文件存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Fatal("保存的文件不存在")
	}
}

func TestEnsureAPIKeys(t *testing.T) {
	// 设置环境变量
	os.Setenv("DEEPSEEK_API_KEY", "test-key-deepseek")
	defer os.Unsetenv("DEEPSEEK_API_KEY")

	cfg := &Config{
		Providers: map[string]ProviderConfig{
			"deepseek": {Name: "DeepSeek"},
		},
	}

	ensureAPIKeys(cfg)

	if cfg.Providers["deepseek"].APIKey != "test-key-deepseek" {
		t.Errorf("期望 API Key=test-key-deepseek, 得到 %s", cfg.Providers["deepseek"].APIKey)
	}
}

func TestLoadDefaultsWithoutWorkspace(t *testing.T) {
	// 测试无工作区时的回退加载
	cfg := loadDefaults("", 0)
	if cfg.Model == "" {
		t.Fatal("配置模型不应为空")
	}
}

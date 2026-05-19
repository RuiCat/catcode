// Package embed 提供编译时嵌入的默认数据
//
// 使用 go:embed 将默认角色定义和默认配置嵌入到二进制中。
// 首次运行时会将这些种子数据写入工作区数据库。
package embed

import (
	"embed"
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed default_settings.json
var DefaultSettingsJSON []byte

//go:embed roles/*.yaml
var DefaultRolesFS embed.FS

//go:embed prompts/*.txt
var DefaultPromptsFS embed.FS

// DefaultSettings 解析后的默认配置 map（用于扁平化写入 settings 表）
var DefaultSettings map[string]any

func init() {
	DefaultSettings = make(map[string]any)
	if err := json.Unmarshal(DefaultSettingsJSON, &DefaultSettings); err != nil {
		panic("embed: 解析 default_settings.json 失败: " + err.Error())
	}
}

// AgentPrompt 从嵌入的 roles/*.yaml 中提取的智能体提示词和模型配置
type AgentPrompt struct {
	SystemPrompt string
	ModelName    string
	Temperature  float64
	ContextLimit int
	OutputLimit  int
	Provider     string
}

// agentYAML 用于 yaml.v3 解析角色 YAML 文件
type agentYAML struct {
	SystemPrompt string         `yaml:"system_prompt"`
	Temperature  float64        `yaml:"temperature"`
	Model        agentModelYAML `yaml:"model"`
	Tools        []string       `yaml:"tools"`
}

type agentModelYAML struct {
	Provider    string         `yaml:"provider"`
	Name        string         `yaml:"name"`
	Temperature float64        `yaml:"temperature"`
	Limit       agentLimitYAML `yaml:"limit"`
}

type agentLimitYAML struct {
	Context int `yaml:"context"`
	Output  int `yaml:"output"`
}

// GetAgentPrompt 从嵌入的 roles/<name>.yaml 读取并提取系统提示词和模型配置
func GetAgentPrompt(name string) (*AgentPrompt, error) {
	data, err := DefaultRolesFS.ReadFile("roles/" + name + ".yaml")
	if err != nil {
		return nil, err
	}

	var ay agentYAML
	if err := yaml.Unmarshal(data, &ay); err != nil {
		return nil, err
	}

	prompt := &AgentPrompt{
		SystemPrompt: strings.TrimSpace(ay.SystemPrompt),
		Temperature:  0.1,
		ContextLimit: 131072,
		OutputLimit:  8000,
	}

	if ay.Model.Name != "" {
		prompt.ModelName = ay.Model.Name
	}
	if ay.Model.Provider != "" {
		prompt.Provider = ay.Model.Provider
	}
	if ay.Model.Temperature != 0 {
		prompt.Temperature = ay.Model.Temperature
	} else if ay.Temperature != 0 {
		prompt.Temperature = ay.Temperature
	}
	if ay.Model.Limit.Context != 0 {
		prompt.ContextLimit = ay.Model.Limit.Context
	}
	if ay.Model.Limit.Output != 0 {
		prompt.OutputLimit = ay.Model.Limit.Output
	}

	// 组合完整模型名 "provider:modelname"
	if prompt.Provider != "" && prompt.ModelName != "" {
		prompt.ModelName = prompt.Provider + ":" + prompt.ModelName
	}

	return prompt, nil
}

// GetAgentTools 从嵌入的 roles/<name>.yaml 读取工具列表
func GetAgentTools(name string) []string {
	data, err := DefaultRolesFS.ReadFile("roles/" + name + ".yaml")
	if err != nil {
		return nil
	}

	var ay agentYAML
	if err := yaml.Unmarshal(data, &ay); err != nil {
		return nil
	}

	return ay.Tools
}

// GetPrompt 从嵌入的 prompts/<name>.txt 读取提示词模板
func GetPrompt(name string) (string, error) {
	data, err := DefaultPromptsFS.ReadFile("prompts/" + name + ".txt")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

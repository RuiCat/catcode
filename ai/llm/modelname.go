// Package llm 实现 LLM 提供商类型定义和 OpenAI 兼容客户端
package llm

import "strings"

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 模型名解析/组合工具
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ParseModelName 解析完整模型名 "provider:modelname"
// 无 ":" 时返回 ("", fullName)，表示使用默认 provider
// 示例:
//
//	"deepseek:deepseek-chat" → ("deepseek", "deepseek-chat")
//	"openai:gpt-4"          → ("openai", "gpt-4")
//	"deepseek-chat"         → ("", "deepseek-chat")
func ParseModelName(fullName string) (provider, model string) {
	idx := strings.Index(fullName, ":")
	if idx < 0 {
		return "", fullName
	}
	return fullName[:idx], fullName[idx+1:]
}

// BuildModelName 组合 provider 和 model 为完整模型名
//
//	BuildModelName("deepseek", "deepseek-chat") → "deepseek:deepseek-chat"
func BuildModelName(provider, model string) string {
	if provider == "" {
		return model
	}
	return provider + ":" + model
}

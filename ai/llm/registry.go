// Package llm 实现 LLM 提供商抽象与多 provider 管理
package llm

import "sync"

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ProviderRegistry — 多 Provider 管理器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ProviderRegistry 管理多个 LLM Provider 实例
// 允许不同智能体使用不同 provider（不同 API key / baseURL）
type ProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]Provider // provider name → instance
	default_  Provider
}

// NewProviderRegistry 创建 provider 注册表
func NewProviderRegistry(defaultProvider Provider) *ProviderRegistry {
	return &ProviderRegistry{
		providers: make(map[string]Provider),
		default_:  defaultProvider,
	}
}

// Register 注册一个 provider
func (pr *ProviderRegistry) Register(name string, p Provider) {
	pr.mu.Lock()
	defer pr.mu.Unlock()
	pr.providers[name] = p
}

// Get 按名称获取 provider，未找到返回默认
func (pr *ProviderRegistry) Get(name string) Provider {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	if name == "" {
		return pr.default_
	}
	if p, ok := pr.providers[name]; ok {
		return p
	}
	return pr.default_
}

// Default 返回默认 provider
func (pr *ProviderRegistry) Default() Provider {
	return pr.default_
}

// Names 返回所有已注册 provider 名称
func (pr *ProviderRegistry) Names() []string {
	pr.mu.RLock()
	defer pr.mu.RUnlock()
	names := make([]string, 0, len(pr.providers))
	for name := range pr.providers {
		names = append(names, name)
	}
	return names
}

// ResolveModel 从完整模型名解析对应的 Provider 实例和纯模型名
// "deepseek:deepseek-chat" → (deepseek-provider, "deepseek-chat")
// "deepseek-chat"         → (default-provider, "deepseek-chat")
func (pr *ProviderRegistry) ResolveModel(fullName string) (Provider, string) {
	providerName, modelName := ParseModelName(fullName)
	if providerName == "" {
		return pr.default_, modelName
	}
	pr.mu.RLock()
	p, ok := pr.providers[providerName]
	pr.mu.RUnlock()
	if !ok {
		// 未知 provider，回退默认
		return pr.default_, modelName
	}
	return p, modelName
}

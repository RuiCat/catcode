package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"catcode/ai/llm"
	"catcode/core/config"
	"catcode/data/storage"
)

// initProviders 从配置初始化所有 Provider 并返回 ProviderRegistry
func initProviders(cfg *config.Config, wdb storage.WorkspaceDB) *llm.ProviderRegistry {
	if len(cfg.Providers) == 0 {
		fmt.Fprintf(os.Stderr, "❌ 未配置任何 Provider\n")
		os.Exit(1)
	}

	var registry *llm.ProviderRegistry
	first := true

	for name, p := range cfg.Providers {
		apiKey := p.APIKey
		if apiKey == "" {
			// 从环境变量读取: <NAME>_API_KEY（通用）
			envVar := strings.ToUpper(name) + "_API_KEY"
			apiKey = os.Getenv(envVar)
		}
		if apiKey == "" {
			// 通用回退
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			// 从数据库 settings 读取（兼容加密存储）
			settingKey := "providers." + name + ".api_key"
			if val, _, err := wdb.GetSetting(settingKey); err == nil && val != "" {
				if decrypted, decErr := storage.DecryptAPIKey(val); decErr == nil {
					apiKey = decrypted
				}
			}
		}
		if apiKey == "" {
			fmt.Fprintf(os.Stderr, "  ⚠ Provider %s 缺少 API Key，跳过\n", name)
			continue
		}

		baseURL := p.BaseURL
		if baseURL == "" {
			fmt.Fprintf(os.Stderr, "  ⚠ Provider %s 缺少 Base URL，跳过\n", name)
			continue
		}

		prov := llm.NewOpenAI(p.Name, baseURL, apiKey)
		if first {
			registry = llm.NewProviderRegistry(prov)
			first = false
		} else {
			registry.Register(name, prov)
		}
		fmt.Printf("  🔌 Provider 就绪        %s (%s)\n", name, baseURL)
	}

	if first {
		// 没有可用的 Provider，交互式提示输入 API Key
		registry = promptForAPIKey(cfg, wdb)
		if registry == nil {
			fmt.Fprintf(os.Stderr, "❌ 未提供有效的 API Key\n")
			os.Exit(1)
		}
	}

	return registry
}

// promptForAPIKey 交互式提示用户输入 API Key，存储到 DB 并初始化 Provider
func promptForAPIKey(cfg *config.Config, wdb storage.WorkspaceDB) *llm.ProviderRegistry {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("\n  ╭──────────────────────────────────────────────╮")
	fmt.Println("  │  🔑 需要 API Key                             │")
	fmt.Println("  ├──────────────────────────────────────────────┤")
	for name, p := range cfg.Providers {
		if p.BaseURL != "" {
			fmt.Printf("  │  %s (%s)\n", name, p.Name)
		}
	}
	fmt.Println("  ╰──────────────────────────────────────────────╯")

	for name, p := range cfg.Providers {
		if p.BaseURL == "" {
			continue
		}
		fmt.Printf("\n  🔑 请输入 %s 的 API Key (直接回车跳过): ", p.Name)
		input, err := reader.ReadString('\n')
		if err != nil {
			continue
		}
		apiKey := strings.TrimSpace(input)
		if apiKey == "" {
			continue
		}

		// 存入 DB（加密存储）
		settingKey := "providers." + name + ".api_key"
		encryptedAPIKey, encErr := storage.EncryptAPIKey(apiKey)
		if encErr != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ 加密 API Key 失败: %v\n", encErr)
			continue
		}
		if err := wdb.SetSetting(settingKey, encryptedAPIKey, "string", "user"); err != nil {
			fmt.Fprintf(os.Stderr, "  ⚠ 存储 API Key 失败: %v\n", err)
		}

		// 更新运行时配置
		p.APIKey = apiKey
		cfg.Providers[name] = p

		// 创建 Provider
		prov := llm.NewOpenAI(p.Name, p.BaseURL, apiKey)
		fmt.Printf("  🔌 Provider 就绪        %s (%s)\n", name, p.BaseURL)
		return llm.NewProviderRegistry(prov)
	}

	return nil
}

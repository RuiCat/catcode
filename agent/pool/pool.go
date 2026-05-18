// Package agent 实现子智能体并发执行池
// 借鉴 opencat 的 WorkerPool + SafeGo 设计
// 每个子智能体拥有独立 LLM 会话、权限集和工具集
package agent

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"catcode/agent/subagent"
	"catcode/ai/llm"
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/data/embed"
	"catcode/data/storage"
	"catcode/tool"
)

// PoolInterface 子智能体池接口
type PoolInterface interface {
	Execute(ctx context.Context, agentType, task, contextSummary string) (<-chan string, error)
	ExecuteAsync(ctx context.Context, agentType, task, contextSummary string)
	GetOrCreate(agentType string) (subagent.SubAgent, error)
	Snapshot() []subagent.AgentSnapshot
	ActiveCount() int
	TotalCount() int
	Shutdown()
	SetWorkspaceDB(wdb storage.WorkspaceDB, contextLimit int)
	SetMemoryService(ms storage.MemoryService)
	SetWorkDir(dir string)
	SetGuardReviewer()
	GetAll(agentType string) ([]subagent.SubAgent, bool)
	GetAllAgents() []subagent.SubAgent
}

type Pool struct {
	agents      map[string][]subagent.SubAgent // type → instances
	providers   *llm.ProviderRegistry           // 多 provider 注册表
	bus         event.EventBus
	configs     map[string]subagent.Config // type → config
	toolFactory func(string) *tool.Tool    // 工具工厂函数

	// 并发控制
	semaphore     chan struct{} // 全局信号量
	maxConcurrent int

	wdb           storage.WorkspaceDB // 数据库引用
	contextLimit  int                  // 默认上下文限制
	memoryService storage.MemoryService
	workDir       string

	mu sync.RWMutex
}

// PoolConfig 池配置
type PoolConfig struct {
	MaxConcurrent int                        // 最大并发子智能体数
	AgentConfigs  map[string]subagent.Config // 类型 → 配置
	ToolFactory   func(string) *tool.Tool    // 工具工厂（可选）
}

// NewPool 创建子智能体池
func NewPool(cfg PoolConfig, providers *llm.ProviderRegistry, bus event.EventBus) PoolInterface {
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 3
	}
	return &Pool{
		agents:        make(map[string][]subagent.SubAgent),
		providers:     providers,
		bus:           bus,
		configs:       cfg.AgentConfigs,
		toolFactory:   cfg.ToolFactory,
		semaphore:     make(chan struct{}, cfg.MaxConcurrent),
		maxConcurrent: cfg.MaxConcurrent,
	}
}

// SetGuardReviewer 为所有子智能体设置 guard 审查器
func (p *Pool) SetGuardReviewer() {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, instances := range p.agents {
		for _, sa := range instances {
			sa.SetGuardReviewer(p)
		}
	}
}

// SetWorkspaceDB 设置数据库引用并应用于所有子智能体
func (p *Pool) SetWorkspaceDB(wdb storage.WorkspaceDB, contextLimit int) {
	p.wdb = wdb
	p.contextLimit = contextLimit
}

// SetMemoryService 设置记忆服务并传播给所有子智能体
func (p *Pool) SetMemoryService(ms storage.MemoryService) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.memoryService = ms
	for _, instances := range p.agents {
		for _, inst := range instances {
			inst.SetMemoryService(ms)
		}
	}
}

// SetWorkDir 设置工作目录并传播给所有子智能体
func (p *Pool) SetWorkDir(dir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.workDir = dir
	for _, instances := range p.agents {
		for _, inst := range instances {
			inst.SetWorkDir(dir)
		}
	}
}

// GetOrCreate 获取或创建指定类型的子智能体实例
// 优先返回空闲实例，若无则创建新实例
func (p *Pool) GetOrCreate(agentType string) (subagent.SubAgent, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	cfg, ok := p.configs[agentType]
	if !ok {
		return nil, cerr.Newf("agent: 未知的子智能体类型: %s", agentType)
	}

	// 查找空闲实例
	if instances, ok := p.agents[agentType]; ok {
		for _, inst := range instances {
			if !inst.IsBusy() {
				return inst, nil
			}
		}
	}

	// 创建新实例
	inst := subagent.New(cfg, p.providers, p.bus, p.toolFactory)
	if p.wdb != nil {
		inst.SetWorkspaceDB(p.wdb)
		inst.SetContextLimit(p.contextLimit)
	}
	if p.memoryService != nil {
		inst.SetMemoryService(p.memoryService)
	}
	inst.SetGuardReviewer(p) // 自动设置 guard 审查器
	if p.workDir != "" {
		inst.SetWorkDir(p.workDir)
	}
	p.agents[agentType] = append(p.agents[agentType], inst)
	return inst, nil
}

// Execute 提交任务到子智能体池
// 阻塞等待信号量可用，然后执行任务
func (p *Pool) Execute(ctx context.Context, agentType, task, contextSummary string) (<-chan string, error) {
	// 获取信号量
	select {
	case p.semaphore <- struct{}{}:
		// 获取成功
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(120 * time.Second):
		return nil, cerr.New("agent: 等待子智能体超时")
	}

	inst, err := p.GetOrCreate(agentType)
	if err != nil {
		<-p.semaphore // 释放信号量
		return nil, err
	}

	// 包装以在完成时释放信号量
	resultCh := make(chan string, 32)
	go func() {
		defer func() {
			<-p.semaphore // 释放信号量
			if r := recover(); r != nil {
				msg := fmt.Sprintf("panic: %v", r)
				resultCh <- fmt.Sprintf("❌ 子智能体池内部错误 (panic): %v", r)
				logPoolError(p.wdb, "内部", "error", msg, "pool", "")
			}
			close(resultCh) // 有且仅有一次关闭
		}()

		ch, err := inst.Execute(ctx, task, contextSummary)
		if err != nil {
			resultCh <- fmt.Sprintf("❌ %s 执行失败: %v", agentType, err)
			return
		}
		for text := range ch {
			resultCh <- text
		}
	}()

	return resultCh, nil
}

// ExecuteAsync 异步执行，通过 EventBus 返回结果
func (p *Pool) ExecuteAsync(ctx context.Context, agentType, task, contextSummary string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("panic: %v", r)
				p.bus.Publish(event.EventSubAgentError, map[string]any{
					"type":  agentType,
					"error": msg,
				})
				logPoolError(p.wdb, "内部", "error", msg, "pool", "")
			}
		}()

		ch, err := p.Execute(ctx, agentType, task, contextSummary)
		if err != nil {
			p.bus.Publish(event.EventSubAgentError, map[string]any{
				"type":  agentType,
				"error": err.Error(),
			})
			return
		}
		var result string
		for text := range ch {
			result += text
		}
		p.bus.Publish(event.EventSubAgentResult, map[string]any{
			"type":   agentType,
			"result": result,
			"task":   task,
		})
	}()
}

// ActiveCount 返回当前活跃（正在执行）的子智能体数量
func (p *Pool) ActiveCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, instances := range p.agents {
		for _, inst := range instances {
			if inst.IsBusy() {
				count++
			}
		}
	}
	return count
}

// TotalCount 返回池中子智能体总数
func (p *Pool) TotalCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	count := 0
	for _, instances := range p.agents {
		count += len(instances)
	}
	return count
}

// ResetAllSessions 重置所有子智能体会话
func (p *Pool) ResetAllSessions() {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, agents := range p.agents {
		for _, sa := range agents {
			sa.ResetSession()
		}
	}
}

// GetAll 获取指定类型的所有子智能体实例
func (p *Pool) GetAll(agentType string) ([]subagent.SubAgent, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	instances, ok := p.agents[agentType]
	return instances, ok
}

// GetAllAgents 获取所有子智能体实例（所有类型）
func (p *Pool) GetAllAgents() []subagent.SubAgent {
	p.mu.RLock()
	defer p.mu.RUnlock()
	var all []subagent.SubAgent
	for _, instances := range p.agents {
		all = append(all, instances...)
	}
	return all
}

// Snapshot 获取所有子智能体的状态快照
func (p *Pool) Snapshot() []subagent.AgentSnapshot {
	all := p.GetAllAgents()
	snapshots := make([]subagent.AgentSnapshot, 0, len(all))
	for _, inst := range all {
		snapshots = append(snapshots, inst.Snapshot())
	}
	return snapshots
}

// Shutdown 关闭池，清空所有子智能体
func (p *Pool) Shutdown() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.agents = make(map[string][]subagent.SubAgent)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 子智能体配置（从 DB 读取）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// AgentConfigsFromDB 从 agent_definitions 表构建子智能体配置
// 过滤 mode='subagent' 且 enabled=true 的角色
func AgentConfigsFromDB(wdb storage.WorkspaceDB) map[string]subagent.Config {
	defs, err := wdb.GetAllAgentDefinitions()
	if err != nil {
		return DefaultAgentConfigs() // 回退
	}

	configs := make(map[string]subagent.Config)
	for _, d := range defs {
		if d.Mode != "subagent" || !d.Enabled {
			continue
		}
		maxTokens := 8000
		if d.ModelLimitOutput != nil {
			maxTokens = *d.ModelLimitOutput
		}

		// 解析工具列表
		var tools []string
		storage.FromJSON(d.ToolsJSON, &tools)
		if tools == nil {
			tools = []string{}
		}

		configs[d.Name] = subagent.Config{
			Type:         d.Name,
			Model:        llm.BuildModelName(d.ModelProvider, d.ModelName),
			SystemPrompt: d.SystemPrompt,
			Temperature:  d.Temperature,
			MaxTokens:    maxTokens,
			Tools:        tools,
			ProviderName: d.ModelProvider,
			Permissions:  parsePermissions(d.PermissionJSON),
		}
	}
	// 数据库 tools_json 为空时回退到默认工具列表
	for name, cfg := range configs {
		if len(cfg.Tools) == 0 {
			if dc, ok := DefaultAgentConfigs()[name]; ok {
				cfg.Tools = dc.Tools
				configs[name] = cfg
			}
		}
	}
	return configs
}

// DefaultAgentConfigs 返回默认子智能体配置
// 从嵌入的 roles/*.yaml 读取完整提示词、模型配置和工具列表
func DefaultAgentConfigs() map[string]subagent.Config {
	agentNames := []string{"explore", "plan", "general", "reviewer", "verifier", "guard", "lean4"}

	configs := make(map[string]subagent.Config)
	for _, name := range agentNames {
		prompt, err := embed.GetAgentPrompt(name)
		if err != nil || prompt.SystemPrompt == "" {
			continue
		}

		modelName := prompt.ModelName
		if modelName == "" {
			if name == "explore" {
				modelName = "deepseek:deepseek-v4-flash"
			} else if name == "guard" {
				modelName = "deepseek:deepseek-chat"
			} else {
				modelName = "deepseek:deepseek-v4-pro"
			}
		}

		cfg := subagent.Config{
			Type:         name,
			Model:        modelName,
			SystemPrompt: prompt.SystemPrompt,
			Temperature:  prompt.Temperature,
			MaxTokens:    prompt.OutputLimit,
		}

		if tools := embed.GetAgentTools(name); len(tools) > 0 {
			cfg.Tools = tools
		}

		configs[name] = cfg
	}
	return configs
}

// parsePermissions 从 JSON 解析权限规则
func parsePermissions(jsonStr string) []tool.PermissionRule {
	var permMap map[string]any
	if err := storage.FromJSON(jsonStr, &permMap); err != nil || permMap == nil {
		return nil
	}
	return tool.PermissionFromMap(permMap)
}

// logPoolError Pool 层错误持久化
func logPoolError(wdb storage.WorkspaceDB, category, severity, message, source, convID string) {
	if wdb == nil {
		return
	}
	_ = wdb.LogError(category, severity, message, getPoolStack(), source, convID)
}

// getPoolStack 获取当前 goroutine 的堆栈跟踪
func getPoolStack() string {
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	return string(buf[:n])
}

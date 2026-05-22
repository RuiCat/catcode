// Package orchestrator 实现主智慧体编排逻辑
// Architect 是顶层编排者，协调侧加载角色、子智能体和规划引擎
package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"catcode/agent/plan"
	agent "catcode/agent/pool"
	"catcode/agent/role"
	"catcode/ai/compact"
	"catcode/ai/llm"
	"catcode/ai/session"
	"catcode/core/config"
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/core/utils"
	"catcode/data/embed"
	"catcode/data/storage"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Architect — 主智慧体
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// maxToolCallRounds 主智能体最大 tool-loop 轮次。
// 子智能体使用更小的值 10（定义于 base.go），防止子任务过度递归。
const maxToolCallRounds = 20

// MemoryIndex 更新控制（配合方案1优化：只在边界条件更新，而非每轮）
const (
	memoryIndexRefreshRounds   = 8               // 每 8 轮 tool-loop 刷新一次
	memoryIndexRefreshInterval = 5 * time.Minute // 或每 5 分钟刷新
)

// Architect 主智慧体，负责：
// 1. 理解用户需求
// 2. 编排侧加载角色和子智能体
// 3. 通过 EventBus 协调全局工作流
// ArchitectInterface 定义主智能体的完整接口，按消费者可拆分为：
//   - InputProcessor — ProcessInput (TUI/REPL 使用)
//   - SubAgentContextBuilder — BuildSubAgentContext (子智能体委派使用)
//   - SessionProvider — GetSession (测试/调试使用)
//   - ToolRegistrar — RegisterTool (启动配置使用)
//   - Configurator — SetWorkDir/SetPlanEngine/InjectMemoryIndex/LoadHistory (初始化使用)
// TODO: 在接口消费者稳定后，按 Go 接口隔离原则拆分为小接口。

// ArchitectInterface 主智能体编排接口
type ArchitectInterface interface {
	ProcessInput(ctx context.Context, userInput string) (<-chan string, error)
	BuildSubAgentContext(taskDesc, subagentType string) string
	GetSession() *session.Session
	RegisterTool(t *tool.Tool) error
	SetWorkDir(dir string)
	SetPlanEngine(pe plan.PlanEngineInterface)
	InjectMemoryIndex()
	LoadHistory(messages []*storage.MessageRow)
}

// Architect 主智能体编排器，管理对话流程和工具执行
type Architect struct {
	config      *Config
	provider    llm.Provider
	roleReg     role.RegistryInterface
	bus         event.EventBus
	mainSession *session.Session
	agentPool   agent.PoolInterface
	wdb         storage.WorkspaceDB // 工作区数据库（压缩+记忆）

	// 记忆系统
	memoryService storage.MemoryService // 统一记忆服务（全局+工作区）

	// 错误处理
	toolErrors *cerr.ErrorCollector // 统一错误收集器（工具+API错误）

	planStatus       string                   // 当前规划状态（显示在标题栏）
	planEngine       plan.PlanEngineInterface // planning engine for plan_enter/plan_exit mode
	toolCallRounds   int                      // 当前请求内的 tool call 轮次数
	maxToolResultLen int                      // 工具结果最大字符数（从 DB 配置 tool.max_result_length 读取，默认 4000）

	// 工作区和指令文件
	workDir      string                    // 工作区路径
	instructions *storage.InstructionFiles // 缓存的指令文件

	// 原始 SystemPrompt（不含记忆索引）
	originalSystemPrompt string

	// MemoryIndex 更新控制（配合方案1优化：只在边界条件更新，而非每轮）
	memoryIndexRounds    int       // 自上次更新后的 tool-loop 轮次
	memoryIndexUpdatedAt time.Time // 上次更新时间
}

// Config Architect 配置
type Config struct {
	Model        string
	SystemPrompt string
	Temperature  float64
	ContextLimit int // 上下文窗口大小（从角色 model.limit.context 读取）
	MaxOutput    int // 最大输出 token（从角色 model.limit.output 读取）
}

// DefaultArchitectConfig 默认配置 — 优先从嵌入的 architect.yaml 获取提示词和模型配置
func DefaultArchitectConfig() *Config {
	cfg := &Config{
		Model:        config.DefaultChatModel,
		Temperature:  0.3,
		ContextLimit: 65536,
		MaxOutput:    8192,
	}

	// 优先从嵌入的 architect.yaml 读取完整配置
	if prompt, err := embed.GetAgentPrompt("architect"); err == nil && prompt.SystemPrompt != "" {
		cfg.SystemPrompt = prompt.SystemPrompt
		if prompt.ModelName != "" {
			cfg.Model = prompt.ModelName
		}
		if prompt.Temperature > 0 {
			cfg.Temperature = prompt.Temperature
		}
		if prompt.ContextLimit > 0 {
			cfg.ContextLimit = prompt.ContextLimit
		}
		if prompt.OutputLimit > 0 {
			cfg.MaxOutput = prompt.OutputLimit
		}
	}

	return cfg
}

// NewArchitect 创建主智慧体实例
func NewArchitect(cfg *Config, provider llm.Provider, roleReg role.RegistryInterface, bus event.EventBus, agentPool agent.PoolInterface, wdb storage.WorkspaceDB, memoryService storage.MemoryService) ArchitectInterface {
	if cfg == nil {
		cfg = DefaultArchitectConfig()
	}

	arch := &Architect{
		config:               cfg,
		provider:             provider,
		roleReg:              roleReg,
		bus:                  bus,
		mainSession:          session.New("architect-main", cfg.Model, cfg.SystemPrompt),
		agentPool:            agentPool,
		wdb:                  wdb,
		memoryService:        memoryService,
		originalSystemPrompt: cfg.SystemPrompt,
		toolErrors:           cerr.NewCollector(3), // 最多3次自纠正
		maxToolResultLen:     4000,                 // 工具结果截断长度默认值
	}

	// 从 DB 读取工具结果截断长度配置
	if wdb != nil {
		if valStr, _, err := wdb.GetSetting("tool.max_result_length"); err == nil && valStr != "" {
			var v int
			if _, scanErr := fmt.Sscanf(valStr, "%d", &v); scanErr == nil && v > 0 {
				arch.maxToolResultLen = v
			}
		}
	}

	// 注入智能记忆选择器（打破 import cycle）
	if memoryService != nil {
		memoryService.SetMemorySelector(compact.SelectRelevantMemories)
	}

	// 使用角色定义的上下文大小设置压缩阈值（保留 15% 缓冲）
	if cfg.ContextLimit > 0 {
		arch.mainSession.CompressThreshold = cfg.ContextLimit * 50 / 100
	}

	arch.mainSession.SetTemperature(cfg.Temperature)
	arch.mainSession.SetMaxTokens(cfg.MaxOutput)
	arch.mainSession.MaxToolResultLen = arch.maxToolResultLen

	// 订阅编排事件
	bus.Subscribe("architect", event.EventRoleResult, arch.onRoleResult, 100)
	bus.Subscribe("architect", event.EventSubAgentResult, arch.onSubAgentResult, 100)

	return arch
}

// SetPlanEngine 设置规划引擎引用，用于计划模式支持
func (a *Architect) SetPlanEngine(pe plan.PlanEngineInterface) {
	a.planEngine = pe
}

// SetWorkDir 设置工作区路径并加载指令文件，同时注入到主会话
func (a *Architect) SetWorkDir(dir string) {
	a.workDir = dir
	a.instructions = storage.LoadInstructions(dir)
	if a.instructions != nil && !a.instructions.IsEmpty() && a.mainSession != nil {
		a.mainSession.SetInstructionsContent(a.instructions.FormatContext(8000))
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 对话处理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ProcessInput 处理用户输入
// 返回响应 channel，发送流式响应文本
func (a *Architect) ProcessInput(ctx context.Context, userInput string) (<-chan string, error) {
	// 添加用户消息
	a.resetErrors()
	a.mainSession.AddMessage("user", userInput)

	// 发布事件
	a.bus.PublishAsync(event.EventUserRequestReceived, map[string]any{
		"input": userInput,
	})

	// 清理上次可能遗留的未闭合 tool_calls
	a.mainSession.CleanOrphanedToolCalls()

	// 注入记忆索引到 SystemPrompt
	a.injectMemoryIndex()
	a.memoryIndexRounds = 0
	a.memoryIndexUpdatedAt = time.Now()

	// 构建请求
	req, err := a.mainSession.BuildRequest()
	if err != nil {
		return nil, cerr.Wrap(err, "orchestrator: 构建请求失败")
	}

	// 发送流式请求
	streamCh, err := a.provider.Chat(ctx, req)
	if err != nil {
		return nil, cerr.Wrap(err, "orchestrator: LLM 请求失败")
	}

	// 处理流式响应
	responseCh := make(chan string, 32)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				errMsg := fmt.Sprintf("\n❌ 主智能体内部错误 (panic): %v", r)
				responseCh <- errMsg
				a.logError("内部", "error", fmt.Sprintf("panic: %v", r), utils.GetStack(), "architect")
			}
			close(responseCh)
		}()
		a.runToolLoop(ctx, streamCh, responseCh)
	}()

	return responseCh, nil
}

// runToolLoop 驱动 tool call 迭代循环（替代原递归模式）
// 用 for 循环在每轮迭代后退出函数，消除递归栈累积
// NOTE: 此函数与 agent/subagent/base_tools.go 的 runToolLoop 高度重复（约 150 行共享逻辑）。
// 差异点：最大重试次数 (Architect=2, SubAgent=1)、
//      processStream 签名、错误事件发布。
// TODO: 提取共享的 "工具执行循环引擎" 到 agent/tool_loop.go

func (a *Architect) runToolLoop(ctx context.Context, firstStreamCh <-chan *llm.StreamEvent, responseCh chan<- string) {
	streamCh := firstStreamCh
	const maxRetries = 2

	for {
		// 上下文准备（第一轮跳过，ProcessInput 已做）
		if a.toolCallRounds > 0 {
			a.prepareNextRound(ctx, responseCh)
		}

		// 处理流式响应（含重试循环）
		var result StreamResult
		var streamErr error

		for attempt := 0; attempt <= maxRetries; attempt++ {
			if attempt > 0 {
				responseCh <- fmt.Sprintf("\n🔄 重试中 (%d/%d)...", attempt, maxRetries)
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Duration(attempt) * time.Second):
				}

				// 重建请求
				req, err := a.mainSession.BuildRequest()
				if err != nil {
					responseCh <- fmt.Sprintf("\n❌ 构建后续请求失败: %v", err)
					return
				}
				streamCh, err = a.provider.Chat(ctx, req)
				if err != nil {
					if attempt < maxRetries && cerr.IsRetryable(err) {
						continue
					}
					streamErr = err
					break
				}
			}

			result, streamErr = a.processStream(ctx, streamCh, responseCh)
			if streamErr == nil {
				break
			}

			if attempt < maxRetries {
				continue
			}
		}

		if streamErr != nil {
			errMsg := streamErr.Error()
			if len(errMsg) > 300 {
				errMsg = errMsg[:300] + "..."
			}
			responseCh <- fmt.Sprintf("\n❌ LLM 请求失败: %s", errMsg)
			if strings.Contains(streamErr.Error(), "400") {
				a.mainSession.CleanOrphanedToolCalls()
			}
			a.logError("API", "error", streamErr.Error(), errStack(streamErr), "architect")
			a.handleError(responseCh, "API", streamErr, "已清理会话状态")
			return
		}

		// 无工具调用 → 最终回复
		if len(result.ToolCalls) == 0 {
			a.bus.PublishAsync(event.EventUserRequestCompleted, map[string]any{
				"content":    result.Content,
				"tool_calls": 0,
			})
			return
		}

		// 执行工具调用
		a.executeToolCalls(ctx, result.ToolCalls, responseCh)

		// 检查是否达到 tool call 轮次上限
		if a.toolCallRounds > maxToolCallRounds {
			a.bus.PublishAsync(event.EventUserRequestCompleted, map[string]any{
				"content":    "",
				"tool_calls": len(result.ToolCalls),
			})
			return
		}

		// 构建下一轮请求
		responseCh <- "\n💭"
		a.planStatus = "思考中"

		req, err := a.mainSession.BuildRequest()
		if err != nil {
			responseCh <- fmt.Sprintf("\n❌ 构建后续请求失败: %v", err)
			return
		}

		streamCh, err = a.provider.Chat(ctx, req)
		if err != nil {
			if strings.Contains(err.Error(), "400") {
				a.mainSession.CleanOrphanedToolCalls()
			}
			a.logError("API", "error", err.Error(), errStack(err), "architect")
			a.handleError(responseCh, "API", err, "已清理会话状态")
			return
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 会话管理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// GetSession 返回主会话
func (a *Architect) GetSession() *session.Session {
	return a.mainSession
}

// RegisterTool 向主会话注册工具
func (a *Architect) RegisterTool(t *tool.Tool) error {
	return a.mainSession.AddTool(t)
}

// SetSystemPrompt 更新系统提示词（同时更新原始缓存）
func (a *Architect) SetSystemPrompt(prompt string) {
	a.mainSession.SetSystemPrompt(prompt)
	a.originalSystemPrompt = prompt
}

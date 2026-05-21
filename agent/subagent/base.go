package subagent

import (
	"context"
	"fmt"
	"sync"
	"time"

	"catcode/ai/llm"
	"catcode/ai/session"
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/core/utils"
	"catcode/data/storage"
	"catcode/tool"
)

// BaseAgent 子智能体基础实现
type BaseAgent struct {
	id        string
	agentType string
	model     string
	session   *session.Session
	provider  llm.Provider
	tools     *tool.Registry
	perms     *tool.PermissionChecker
	bus       event.EventBus

	temperature float64
	maxTokens   int

	mu          sync.RWMutex
	status      string
	task        string
	currentTool string
	toolCount   int
	startTime   time.Time
	duration    time.Duration
	errorMsg    string
	config      Config
	fullOutput  string

	maxToolCallRounds int
	toolCallRounds    int
	toolErrors        *cerr.ErrorCollector
	guardReviewer     GuardReviewer
	guardCache        *guardLRUCache
	askArchitectFn    func(question string) string

	wdb              storage.WorkspaceDB
	memoryService    storage.MemoryService
	conversationID   string
	contextLimit     int
	maxToolResultLen int
	contextBuilder   ContextBuilder // Hook 上下文构建器（可选）
	workDir          string         // 仅在 Execute 前通过 SetWorkDir 设置，executeToolCalls 中只读
}

// ID 返回子智能体唯一标识
func (sa *BaseAgent) ID() string { return sa.id }

// Type 返回子智能体类型
func (sa *BaseAgent) Type() string { return sa.agentType }

// New 创建子智能体
func New(cfg Config, providers *llm.ProviderRegistry, bus event.EventBus, toolFactory func(string) *tool.Tool) SubAgent {
	id := fmt.Sprintf("subagent-%s", cfg.Type)
	prov := providers.Get(cfg.ProviderName)
	sess := session.New(id, cfg.Model, cfg.SystemPrompt)

	sa := &BaseAgent{
		id:                id,
		agentType:         cfg.Type,
		model:             cfg.Model,
		session:           sess,
		provider:          prov,
		tools:             tool.NewRegistry(),
		perms:             tool.NewPermissionChecker(cfg.Permissions),
		bus:               bus,
		temperature:       cfg.Temperature,
		maxTokens:         cfg.MaxTokens,
		status:            "idle",
		config:            cfg,
		maxToolCallRounds: 10,
		toolErrors:        cerr.NewCollector(3),
		conversationID:    id,
		contextLimit:      65536,
		maxToolResultLen:  4000,
		guardCache:        newGuardLRUCache(guardCacheMaxSize),
		workDir:           ".",
	}
	sess.MaxToolResultLen = sa.maxToolResultLen

	if toolFactory != nil {
		for _, toolName := range cfg.Tools {
			if t := toolFactory(toolName); t != nil {
				sa.RegisterTool(t)
			}
		}
	}

	askTool := &tool.Tool{
		Function: tool.FuncDef{
			Name:        "ask_architect",
			Description: "向主智能体提问或请求决策。当任务遇到不确定性、需要架构决策、需要更多上下文信息、或需要用户偏好时使用。主智能体会同步响应。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"question": {Type: "string", Description: "向主智能体提出的问题"},
				},
				Required: []string{"question"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			question, _ := args["question"].(string)
			if sa.askArchitectFn != nil {
				answer := sa.askArchitectFn(question)
				return answer, nil
			}
			return "⚠️ ask_architect: 通信通道未建立", nil
		},
	}
	if err := sa.session.AddTool(askTool); err != nil {
		// ask_architect 工具注册失败不影响主流程
	}

	return sa
}

// GetSession 返回子智能体会话
func (sa *BaseAgent) GetSession() *session.Session {
	return sa.session
}

// Snapshot 返回子智能体状态快照（线程安全）
func (sa *BaseAgent) Snapshot() AgentSnapshot {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return AgentSnapshot{
		Name:        sa.agentType,
		ID:          sa.id,
		Status:      sa.status,
		Task:        utils.TruncateStr(sa.task, 60),
		FullTask:    sa.task,
		CurrentTool: sa.currentTool,
		ToolCount:   sa.toolCount,
		StartTime:   sa.startTime,
		Duration:    sa.duration,
		ErrorMsg:    sa.errorMsg,
		FullOutput:  sa.fullOutput,
	}
}

// SetWorkspaceDB 设置工作区数据库引用（启用持久化）
func (sa *BaseAgent) SetWorkspaceDB(wdb storage.WorkspaceDB) {
	sa.wdb = wdb

	if wdb != nil {
		if valStr, _, err := wdb.GetSetting("tool.max_result_length"); err == nil && valStr != "" {
			var v int
			if _, scanErr := fmt.Sscanf(valStr, "%d", &v); scanErr == nil && v > 0 {
				sa.maxToolResultLen = v
				sa.session.MaxToolResultLen = v
			}
		}
	}
}

// SetMemoryService 设置记忆服务引用
func (sa *BaseAgent) SetMemoryService(ms storage.MemoryService) {
	sa.memoryService = ms
}

// SetGuardReviewer 设置 guard 审查器（用于 bash 命令的 LLM 级审查）
func (sa *BaseAgent) SetGuardReviewer(reviewer GuardReviewer) {
	if sa.agentType == "guard" {
		return
	}
	sa.guardReviewer = reviewer
}

// SetAskArchitectCallback 设置向主智能体提问的回调
func (sa *BaseAgent) SetAskArchitectCallback(fn func(question string) string) {
	sa.askArchitectFn = fn
}

// logError 将子智能体错误持久化到数据库
func (sa *BaseAgent) logError(category, severity, message string) {
	if sa.wdb == nil {
		return
	}
	_ = sa.wdb.LogError(category, severity, message, utils.GetStack(), "subagent", sa.conversationID) // 日志持久化失败不影响主流程
}


// SetContextLimit 设置上下文窗口大小（用于压缩判断）
func (sa *BaseAgent) SetContextLimit(limit int) {
	if limit > 0 {
		sa.contextLimit = limit
	}
}

// SetContextBuilder 设置上下文构建器（Hook系统）
func (sa *BaseAgent) SetContextBuilder(builder ContextBuilder) {
	sa.contextBuilder = builder
}

// SetWorkDir 设置工作目录（仅在 Execute 前调用，非线程安全）
func (sa *BaseAgent) SetWorkDir(dir string) {
	sa.workDir = dir
}

// RegisterTool 注册工具到子智能体
func (sa *BaseAgent) RegisterTool(t *tool.Tool) error {
	return sa.session.AddTool(t)
}

// IsBusy 检查是否正在执行
func (sa *BaseAgent) IsBusy() bool {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.status == "pending" || sa.status == "running"
}

// Execute 执行子任务
func (sa *BaseAgent) Execute(ctx context.Context, task, contextSummary string) (<-chan string, error) {
	sa.mu.Lock()
	if sa.status == "running" || sa.status == "pending" {
		sa.mu.Unlock()
		return nil, cerr.Newf("agent: %s 正在执行其他任务", sa.agentType)
	}
	sa.status = "running"
	sa.task = task
	sa.startTime = time.Now()
	if sa.wdb != nil {
		conv, msgs, err := sa.wdb.LoadConversation(sa.conversationID)
		if err == nil && len(msgs) > 0 {
			sa.loadMessages(msgs)
			if conv.Summary != "" {
				sa.session.SetSummary(conv.Summary)
			}
		}
	}
	sa.toolCount = 0
	sa.toolCallRounds = 0
	sa.toolErrors.Reset()
	sa.currentTool = ""
	sa.duration = 0
	sa.errorMsg = ""
	sa.mu.Unlock()

	sa.publishAgentStatus()

	if sa.memoryService != nil {
		index := sa.memoryService.BuildIndex(task)
		if index != "" {
			sa.session.MemoryIndex = index
		}
	}
	// 尝试使用 Hook 构建上下文
	if sa.contextBuilder != nil {
		input := &ContextBuildInput{
			Task:           task,
			ContextSummary: contextSummary,
			AgentType:      sa.config.Type,
		}
		if result, err := sa.contextBuilder.BuildContext(ctx, sa.session, input); err == nil && result != nil {
			if result.SystemPrompt != "" {
				sa.session.SetSystemPrompt(result.SystemPrompt)
			}
			if result.MemoryIndex != "" {
				sa.session.SetMemoryIndex(result.MemoryIndex)
			}
			for _, msg := range result.ExtraSystemMessages {
				sa.session.AddMessage("system", msg)
			}
		}
	}

	if contextSummary != "" {
		sa.session.AddMessage("system", "[上下文]\n"+contextSummary)
	}

	sa.session.AddMessage("user", task)

	if sa.bus != nil {
		sa.bus.PublishAsync(event.EventTaskStarted, map[string]any{
			"agent": sa.agentType,
			"id":    sa.id,
			"task":  task,
		})
	}

	sa.session.Temperature = sa.temperature
	sa.session.MaxTokens = sa.maxTokens

	req, err := sa.session.BuildRequest()
	if err != nil {
		sa.mu.Lock()
		sa.status = "error"
		sa.errorMsg = fmt.Sprintf("构建请求失败: %v", err)
		sa.duration = time.Since(sa.startTime)
		sa.mu.Unlock()
		sa.publishAgentStatus()
		return nil, cerr.Wrap(err, "agent: 构建请求失败")
	}

	streamCh, err := sa.provider.Chat(ctx, req)
	if err != nil {
		sa.mu.Lock()
		sa.status = "error"
		sa.errorMsg = fmt.Sprintf("LLM 请求失败: %v", err)
		sa.duration = time.Since(sa.startTime)
		sa.mu.Unlock()
		sa.publishAgentStatus()
		return nil, cerr.Wrap(err, "agent: LLM 请求失败")
	}

	responseCh := make(chan string, 32)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				msg := fmt.Sprintf("panic: %v", r)
				responseCh <- fmt.Sprintf("\n❌ 子智能体内部错误: %v", r)
				sa.logError("内部", "error", msg)
			}
			close(responseCh)
		}()
		sa.runToolLoop(ctx, streamCh, responseCh, task)
		if sa.wdb != nil {
			sa.session.CleanOrphanedToolCalls()
			conv := sa.session.ToConversationRow()
			msgs := sa.session.ToMessageRows()
			_ = sa.wdb.SaveConversation(conv, msgs)
		}
	}()

	return responseCh, nil
}

// ResetSession 重置子智能体会话（保留工具注册，清除消息历史）
func (sa *BaseAgent) ResetSession() {
	sa.session.Clear()
}

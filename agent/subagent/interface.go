// Package subagent 定义子智能体的标准接口和配置，7种子智能体类型共享 BaseAgent 实现。
package subagent

import (
	"context"
	"time"

	"catcode/ai/session"
	"catcode/data/storage"
	"catcode/tool"
)

// AgentSnapshot 子智能体状态快照
type AgentSnapshot struct {
	Name        string
	ID          string
	Status      string
	Task        string
	FullTask    string
	CurrentTool string
	ToolCount   int
	StartTime   time.Time
	Duration    time.Duration
	ErrorMsg    string
	FullOutput  string
}

// GuardReviewer guard审查器接口（避免与 pool 包的循环依赖）
type GuardReviewer interface {
	Execute(ctx context.Context, agentType, task, contextSummary string) (<-chan string, error)
	GetOrCreate(agentType string) (SubAgent, error)
}

// SubAgent 子智能体接口
type SubAgent interface {
	ID() string
	Type() string
	Execute(ctx context.Context, task, contextSummary string) (<-chan string, error)
	IsBusy() bool
	Snapshot() AgentSnapshot
	SetWorkspaceDB(wdb storage.WorkspaceDB)
	SetMemoryService(ms storage.MemoryService)
	SetGuardReviewer(reviewer GuardReviewer)
	SetAskArchitectCallback(fn func(question string) string)
	SetContextLimit(limit int)
	RegisterTool(t *tool.Tool) error
	ResetSession()
	GetSession() *session.Session
	SetWorkDir(dir string)
}

// ContextBuildInput 上下文构建的输入
type ContextBuildInput struct {
	Task           string
	ContextSummary string
	AgentType      string
	Extra          map[string]any
}

// ContextBuildResult 上下文构建的输出
type ContextBuildResult struct {
	SystemPrompt        string
	MemoryIndex         string
	ExtraSystemMessages []string
}

// ContextBuilder 上下文构建器接口
// Hook 系统通过此接口注入到子智能体的执行生命周期
type ContextBuilder interface {
	Name() string
	BuildContext(ctx context.Context, sa *session.Session, input *ContextBuildInput) (*ContextBuildResult, error)
}

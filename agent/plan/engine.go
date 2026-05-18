// Package plan 实现规划追踪引擎
// 任务分解、依赖管理、状态追踪、工作流状态机
package plan

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	cerr "catcode/core/errors"
	"catcode/core/event"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 任务项
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Status 任务状态
type Status string

const (
	StatusPending    Status = "pending"
	StatusInProgress Status = "in_progress"
	StatusCompleted  Status = "completed"
	StatusCancelled  Status = "cancelled"
	StatusFailed     Status = "failed"
)

// Priority 优先级
type Priority string

const (
	PriorityHigh   Priority = "high"
	PriorityMedium Priority = "medium"
	PriorityLow    Priority = "low"
)

// TodoItem 任务项
type TodoItem struct {
	ID          string     `json:"id"`
	Content     string     `json:"content"`
	Status      Status     `json:"status"`
	Priority    Priority   `json:"priority"`
	CreatedAt   time.Time  `json:"created_at"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
}

// Plan 规划
type Plan struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Todos       []*TodoItem `json:"todos"`
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 规划引擎
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Engine 规划追踪引擎
// PlanEngineInterface 规划引擎接口
type PlanEngineInterface interface {
	CreatePlan(description string, todos []TodoItem) *Plan
	CreatePlanFromJSON(description, todosJSON string) (*Plan, error)
	GetActivePlan() *Plan
	Progress(planID string) float64
	EnterPlanMode(reason string) (string, error)
	ExitPlanMode(response string) (string, error)
	ListTodos(planID string) string
	IsPlanMode() bool
	Close()
}


type Engine struct {
	plans         map[string]*Plan // 所有规划
	activePlanID  string           // 当前活跃规划 ID
	bus           event.EventBus
	mu            sync.RWMutex
	subscriptions []*event.Subscriber // 保存订阅引用以便取消
	closed        bool

	// PlanMode fields
	planMode       bool   // whether currently in plan mode
	planModeReason string // reason for entering plan mode
}

// NewEngine 创建规划引擎
func NewEngine(bus event.EventBus) PlanEngineInterface {
	e := &Engine{
		plans:         make(map[string]*Plan),
		bus:           bus,
		subscriptions: make([]*event.Subscriber, 0),
	}
	// 订阅任务相关事件
	if bus != nil {
		e.subscriptions = append(e.subscriptions,
			bus.Subscribe("plan_engine", event.EventTaskStarted, e.onTaskStarted, 50))
		e.subscriptions = append(e.subscriptions,
			bus.Subscribe("plan_engine", event.EventTaskCompleted, e.onTaskCompleted, 50))
		e.subscriptions = append(e.subscriptions,
			bus.Subscribe("plan_engine", event.EventTaskFailed, e.onTaskFailed, 50))
	}
	return e
}

// Close 关闭规划引擎，取消所有事件订阅
func (e *Engine) Close() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.closed = true
	for _, sub := range e.subscriptions {
		if e.bus != nil {
			e.bus.Unsubscribe(sub)
		}
	}
	e.subscriptions = nil
}

// EnterPlanMode 进入计划模式，禁用 edit/write/bash 等修改工具
func (e *Engine) EnterPlanMode(reason string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.planMode {
		return "已在 Plan 模式中", nil
	}
	e.planMode = true
	e.planModeReason = reason

	if e.bus != nil {
		e.bus.PublishAsync(event.EventPlanModeEntered, map[string]any{
			"reason": reason,
		})
	}

	msg := "✓ 已进入 Plan 模式。edit/write/bash 工具已禁用，仅保留只读工具。"
	if reason != "" {
		msg += fmt.Sprintf(" 原因: %s", reason)
	}
	return msg + "\n使用 plan_exit 工具退出 Plan 模式。", nil
}

// ExitPlanMode 退出计划模式，恢复所有工具可用
func (e *Engine) ExitPlanMode(response string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.planMode {
		return "当前不在 Plan 模式中", nil
	}
	e.planMode = false
	reason := e.planModeReason
	e.planModeReason = ""

	if e.bus != nil {
		e.bus.PublishAsync(event.EventPlanModeExited, map[string]any{
			"response": response,
			"reason":   reason,
		})
	}

	msg := "✓ 已退出 Plan 模式。所有工具已恢复可用。"
	if response != "" {
		msg += fmt.Sprintf("\n用户响应: %s", response)
	}
	return msg, nil
}

// IsPlanMode 返回当前是否处于计划模式
func (e *Engine) IsPlanMode() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.planMode
}

// CreatePlan 创建新规划
func (e *Engine) CreatePlan(description string, todos []TodoItem) *Plan {
	e.mu.Lock()
	defer e.mu.Unlock()

	plan := &Plan{
		ID:          fmt.Sprintf("plan-%d", time.Now().UnixNano()),
		Description: description,
		Todos:       make([]*TodoItem, len(todos)),
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	for i := range todos {
		item := todos[i]
		item.ID = fmt.Sprintf("todo-%d-%d", time.Now().UnixNano(), i)
		item.CreatedAt = time.Now()
		plan.Todos[i] = &item
	}

	e.plans[plan.ID] = plan
	e.activePlanID = plan.ID

	if e.bus != nil {
		e.bus.PublishAsync(event.EventPlanCreated, map[string]any{
			"plan_id":     plan.ID,
			"description": plan.Description,
			"todo_count":  len(plan.Todos),
		})
	}
	return plan
}

// CreatePlanFromJSON 从 JSON 创建规划（供 LLM 工具调用使用）
func (e *Engine) CreatePlanFromJSON(description, todosJSON string) (*Plan, error) {
	var todos []TodoItem
	if err := json.Unmarshal([]byte(todosJSON), &todos); err != nil {
		return nil, cerr.Wrap(err, "plan: JSON 解析失败")
	}
	return e.CreatePlan(description, todos), nil
}

// GetActivePlan 获取当前活跃规划
func (e *Engine) GetActivePlan() *Plan {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.activePlanID == "" {
		return nil
	}
	return e.plans[e.activePlanID]
}

// ClearActivePlan 清除当前活跃规划
func (e *Engine) ClearActivePlan() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.activePlanID = ""
}

// GetPlan 按 ID 获取规划
func (e *Engine) GetPlan(id string) *Plan {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.plans[id]
}

// UpdateTodoStatus 更新任务状态
func (e *Engine) UpdateTodoStatus(planID, todoID string, status Status) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	plan, ok := e.plans[planID]
	if !ok {
		return cerr.Newf("plan: 规划 %s 不存在", planID)
	}

	for _, todo := range plan.Todos {
		if todo.ID == todoID {
			oldStatus := todo.Status
			todo.Status = status
			plan.UpdatedAt = time.Now()

			if status == StatusCompleted || status == StatusFailed {
				now := time.Now()
				todo.CompletedAt = &now
			}

			// 发布事件
			if e.bus != nil {
				if status == StatusInProgress && oldStatus != StatusInProgress {
					e.bus.PublishAsync(event.EventPlanStepStart, map[string]any{
						"plan_id": planID,
						"todo_id": todoID,
					})
				}
				if status == StatusCompleted {
					e.bus.PublishAsync(event.EventPlanStepDone, map[string]any{
						"plan_id": planID,
						"todo_id": todoID,
					})
				}
			}

			// 检查规划是否全部完成
			if e.allCompleted(plan) {
				if e.bus != nil {
					e.bus.PublishAsync(event.EventPlanCompleted, map[string]any{
						"plan_id": planID,
					})
				}
			}
			return nil
		}
	}
	return cerr.Newf("plan: 任务 %s 不存在", todoID)
}

// allCompleted 检查是否所有任务已完成
func (e *Engine) allCompleted(plan *Plan) bool {
	for _, todo := range plan.Todos {
		if todo.Status != StatusCompleted && todo.Status != StatusCancelled {
			return false
		}
	}
	return true
}

// Progress 返回规划进度 (0.0 ~ 1.0)
func (e *Engine) Progress(planID string) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.plans[planID]
	if !ok || len(plan.Todos) == 0 {
		return 0
	}

	done := 0
	for _, todo := range plan.Todos {
		if todo.Status == StatusCompleted || todo.Status == StatusCancelled {
			done++
		}
	}
	return float64(done) / float64(len(plan.Todos))
}

// ListTodos 列出规划中的所有任务
func (e *Engine) ListTodos(planID string) string {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.plans[planID]
	if !ok {
		return "规划不存在"
	}

	statusIcons := map[Status]string{
		StatusPending:    "⬜",
		StatusInProgress: "🔄",
		StatusCompleted:  "✅",
		StatusCancelled:  "❌",
		StatusFailed:     "❌",
	}

	result := fmt.Sprintf("规划: %s\n", plan.Description)
	result += "━━━━━━━━━━━━━━━━━━━━\n"
	for _, todo := range plan.Todos {
		icon := statusIcons[todo.Status]
		result += fmt.Sprintf("%s [%s] %s\n", icon, todo.Priority, todo.Content)
	}
	result += fmt.Sprintf("━━━━━━━━━━━━━━━━━━━━\n进度: %.0f%%\n", e.Progress(planID)*100)
	return result
}

// GetTodosJSON 获取任务列表 JSON（供 LLM 上下文）
func (e *Engine) GetTodosJSON(planID string) (string, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	plan, ok := e.plans[planID]
	if !ok {
		return "[]", nil
	}

	data, err := json.Marshal(plan.Todos)
	if err != nil {
		return "[]", err
	}
	return string(data), nil
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 事件处理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (e *Engine) onTaskStarted(evt event.Event) {
	// 自动切换到下一个 pending 任务
	plan := e.GetActivePlan()
	if plan == nil {
		return
	}
	for _, todo := range plan.Todos {
		if todo.Status == StatusPending {
			e.UpdateTodoStatus(plan.ID, todo.ID, StatusInProgress)
			break
		}
	}
}

func (e *Engine) onTaskCompleted(evt event.Event) {
	plan := e.GetActivePlan()
	if plan == nil {
		return
	}
	// 标记当前 in_progress 为 completed
	for _, todo := range plan.Todos {
		if todo.Status == StatusInProgress {
			e.UpdateTodoStatus(plan.ID, todo.ID, StatusCompleted)
			break
		}
	}
}

func (e *Engine) onTaskFailed(evt event.Event) {
	plan := e.GetActivePlan()
	if plan == nil {
		return
	}
	for _, todo := range plan.Todos {
		if todo.Status == StatusInProgress {
			e.UpdateTodoStatus(plan.ID, todo.ID, StatusFailed)
			break
		}
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 工作流状态机
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// WorkflowState 工作流状态
type WorkflowState string

const (
	StateIdle      WorkflowState = "idle"
	StateExploring WorkflowState = "exploring"
	StatePlanning  WorkflowState = "planning"
	StateExecuting WorkflowState = "executing"
	StateReviewing WorkflowState = "reviewing"
	StateVerifying WorkflowState = "verifying"
	StateCompleted WorkflowState = "completed"
)

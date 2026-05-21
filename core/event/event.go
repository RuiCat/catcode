// Package event 实现事件总线和触发器系统
// 借鉴 opencat 的 EventBus + TriggerManager 设计，
// 作为 catcode 内部编排的通信主干。
package event

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	cerr "catcode/core/errors"
	"catcode/core/utils"
)

// Event 事件结构
type Event struct {
	Name string         // 事件名称（如 "role.dispatch"）
	Data map[string]any // 事件携带数据
}

// Subscriber 事件订阅者
type Subscriber struct {
	ID       string
	Pattern  string      // 匹配模式（支持 * 通配符）
	Handler  func(Event) // 处理函数
	Priority int         // 优先级（数值越大越先执行）
	mu       sync.Mutex  // 保证 Handler 串行执行
}

// EventBus 发布-订阅事件总线
// 安全使用：并发安全，可同时发布和订阅
// EventBus 接口 — 解耦全局事件系统
type EventBus interface {
	Subscribe(id string, pattern string, handler func(Event), priority int) *Subscriber
	Unsubscribe(sub *Subscriber)
	Publish(name string, data map[string]any)
	PublishAsync(name string, data map[string]any)
	SubscriberCount() int
}

type eventBusImpl struct {
	mu           sync.RWMutex
	subscribers  map[string][]*Subscriber // pattern → subscribers
	history      []Event                  // 事件历史（最近 N 个）
	historyMax   int
	historyMu    sync.RWMutex
	publishDepth atomic.Int32 // 递归深度计数器（防止事件链死循环）
}

// NewBus 创建新的事件总线
func NewBus() EventBus {
	return &eventBusImpl{
		subscribers: make(map[string][]*Subscriber),
		history:     make([]Event, 0),
		historyMax:  100,
	}
}

// Subscribe 订阅事件
// pattern 支持通配符：* 匹配任意事件，prefix.* 匹配前缀，*.suffix 匹配后缀
func (bus *eventBusImpl) Subscribe(id string, pattern string, handler func(Event), priority int) *Subscriber {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	sub := &Subscriber{
		ID:       id,
		Pattern:  pattern,
		Handler:  handler,
		Priority: priority,
	}
	bus.subscribers[pattern] = append(bus.subscribers[pattern], sub)
	// 按优先级降序排列
	sort.Slice(bus.subscribers[pattern], func(i, j int) bool {
		return bus.subscribers[pattern][i].Priority > bus.subscribers[pattern][j].Priority
	})
	return sub
}

// Unsubscribe 取消订阅
func (bus *eventBusImpl) Unsubscribe(sub *Subscriber) {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	subs := bus.subscribers[sub.Pattern]
	for i, s := range subs {
		if s == sub {
			bus.subscribers[sub.Pattern] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
}

// Publish 发布事件（同步调用所有匹配的订阅者）
// 每个订阅者的 Handler 在独立 goroutine 中串行化执行（通过 subscriber.mu 保证）
func (bus *eventBusImpl) Publish(name string, data map[string]any) {
	depth := bus.publishDepth.Add(1)
	defer bus.publishDepth.Add(-1)
	if depth > 10 {
		return
	}

	evt := Event{Name: name, Data: data}

	// 记录历史
	bus.historyMu.Lock()
	if len(bus.history) >= bus.historyMax {
		bus.history = bus.history[1:]
	}
	bus.history = append(bus.history, evt)
	bus.historyMu.Unlock()

	// 获取匹配的订阅者
	bus.mu.RLock()
	var matched []*Subscriber
	for pattern, subs := range bus.subscribers {
		if matchPattern(pattern, name) {
			matched = append(matched, subs...)
		}
	}
	bus.mu.RUnlock()

	// 按优先级排序后执行
	sort.Slice(matched, func(i, j int) bool {
		return matched[i].Priority > matched[j].Priority
	})

	// 同步执行（可选改为 goroutine 异步）
	for _, sub := range matched {
		sub.mu.Lock()
		sub.Handler(evt)
		sub.mu.Unlock()
	}
}

// PublishAsync 异步发布事件（不阻塞调用者）
func (bus *eventBusImpl) PublishAsync(name string, data map[string]any) {
	utils.SafeGo("event-publish-"+name, func() {
		bus.Publish(name, data)
	})
}

// History 返回事件历史（最近 N 个）
func (bus *eventBusImpl) History() []Event {
	bus.historyMu.RLock()
	defer bus.historyMu.RUnlock()
	result := make([]Event, len(bus.history))
	copy(result, bus.history)
	return result
}

// SubscriberCount 返回订阅者总数
func (bus *eventBusImpl) SubscriberCount() int {
	bus.mu.RLock()
	defer bus.mu.RUnlock()
	count := 0
	for _, subs := range bus.subscribers {
		count += len(subs)
	}
	return count
}

// matchPattern 事件名匹配模式
// 支持 * 通配符、prefix.*、*.suffix
func matchPattern(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == name {
		return true
	}
	// prefix.* 例如 "role.*" 匹配 "role.dispatch"
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return strings.HasPrefix(name, prefix+".")
	}
	// *.suffix 例如 "*.completed" 匹配 "task.completed"
	if strings.HasPrefix(pattern, "*.") {
		suffix := strings.TrimPrefix(pattern, "*.")
		return strings.HasSuffix(name, "."+suffix)
	}
	return false
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Trigger 系统 — 条件事件触发器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Trigger 条件触发器定义
type Trigger struct {
	Name      string           // 触发器名称
	Event     string           // 匹配的事件名（支持 * 通配符）
	Condition func(Event) bool // 条件检查（可选，nil 表示总是触发）
	Action    func(Event)      // 触发时执行的动作
	Priority  int              // 优先级
	Once      bool             // 是否只触发一次
	fired     bool
	mu        sync.Mutex
}

// TriggerManager 条件触发器管理器（预留功能，当前未集成到启动流程）
// 监听 EventBus 上的事件，匹配触发器并执行动作
type TriggerManager struct {
	bus      EventBus
	triggers []*Trigger
	mu       sync.RWMutex
}

// NewTriggerManager 创建触发器管理器并自动订阅 EventBus
func NewTriggerManager(bus EventBus) *TriggerManager {
	tm := &TriggerManager{
		bus:      bus,
		triggers: make([]*Trigger, 0),
	}
	// 订阅所有事件，由 manager 内部匹配
	bus.Subscribe("__trigger_manager", "*", tm.onEvent, 0)
	return tm
}

// Register 注册触发器
func (tm *TriggerManager) Register(t *Trigger) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.triggers = append(tm.triggers, t)
	sort.Slice(tm.triggers, func(i, j int) bool {
		return tm.triggers[i].Priority > tm.triggers[j].Priority
	})
}

// Unregister 注销触发器
func (tm *TriggerManager) Unregister(name string) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	for i, t := range tm.triggers {
		if t.Name == name {
			tm.triggers = append(tm.triggers[:i], tm.triggers[i+1:]...)
			break
		}
	}
}

// onEvent 事件到达时检查所有触发器
func (tm *TriggerManager) onEvent(evt Event) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	for _, t := range tm.triggers {
		if !matchPattern(t.Event, evt.Name) {
			continue
		}
		t.mu.Lock()
		if t.Once && t.fired {
			t.mu.Unlock()
			continue
		}
		if t.Condition != nil && !t.Condition(evt) {
			t.mu.Unlock()
			continue
		}
		t.fired = true
		t.mu.Unlock()

		// 异步执行动作，不阻塞事件流
		go t.Action(evt)
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 内置事件类型常量
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// 用户交互事件
const (
	EventUserRequestReceived  = "user.request.received"
	EventUserRequestCompleted = "user.request.completed"
)

// 角色生命周期事件
const (
	EventRoleLoaded    = "role.loaded"
	EventRoleActivated = "role.activated"
	EventRoleDispatch  = "role.dispatch"
	EventRoleResult    = "role.result"
	EventRoleError     = "role.error"
	EventRoleUpdated   = "role.updated"
	EventRoleUnloaded  = "role.unloaded"
)

// 子智能体事件
const (
	EventSubAgentDispatch   = "subagent.dispatch"
	EventSubAgentResult     = "subagent.result"
	EventSubAgentError      = "subagent.error"
	EventAgentStatusChanged = "agent.status.changed"
)

// 规划引擎事件
const (
	EventPlanCreated     = "plan.created"
	EventPlanStepStart   = "plan.step.start"
	EventPlanStepDone    = "plan.step.done"
	EventPlanCompleted   = "plan.completed"
	EventPlanModeEntered = "plan.mode.entered"
	EventPlanModeExited  = "plan.mode.exited"
)

// 任务状态事件
const (
	EventTaskStarted   = "task.started"
	EventTaskCompleted = "task.completed"
	EventTaskFailed    = "task.failed"
)

// 周期任务触发事件
const (
	EventScheduledTaskTrigger = "scheduled.task.trigger"
	EventScheduledTaskChanged = "scheduled.task.changed"
)

// 子智能体工具事件
const (
	EventAgentToolStart = "agent.tool.start" // 子智能体开始执行工具
	EventAgentToolEnd   = "agent.tool.end"   // 子智能体工具执行完成
)

// 工具调用事件
const (
	EventToolCallStart = "tool.call.start"
	EventToolCallEnd   = "tool.call.end"
)

// Session 事件
const (
	EventSessionCreated = "session.created"
	EventSessionSaved   = "session.saved"
)

// Companion 陪伴角色事件
const (
	EventCompanionTalk    = "companion.talk"
	EventCompanionRespond = "companion.respond"
	EventCompanionStatus  = "companion.status"
)

// 对话框消息事件（供 send_message 工具使用）
const (
	EventDialogSend = "dialog.send"
)

// 选项框事件
const (
	EventQuestionAsked = "question.asked"
)

// NewEvent 便捷构造器
func NewEvent(name string, data map[string]any) Event {
	if data == nil {
		data = make(map[string]any)
	}
	return Event{Name: name, Data: data}
}

// ValidateEventName 校验事件名格式
func ValidateEventName(name string) error {
	if name == "" {
		return cerr.New("事件名不能为空")
	}
	if strings.Contains(name, " ") {
		return cerr.New("事件名不能包含空格")
	}
	return nil
}

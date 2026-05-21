// Package schedule 实现周期任务调度与空闲检测
// 当用户无输入且无智能体运行时，执行预定义的默认任务
package schedule

import (
	"sync"
	"time"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// IdleDetector — 空闲检测器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// IdleDetector 跟踪系统和用户空闲状态
type IdleDetector struct {
	mu              sync.RWMutex
	lastUserInput   time.Time // 最后用户输入时间
	lastAgentActive time.Time // 最后智能体活跃时间
	idleThreshold   time.Duration
}

// NewIdleDetector 创建空闲检测器
func NewIdleDetector(idleThreshold time.Duration) *IdleDetector {
	now := time.Now()
	return &IdleDetector{
		lastUserInput:   now,
		lastAgentActive: now,
		idleThreshold:   idleThreshold,
	}
}

// Touch 标记用户活动
func (d *IdleDetector) Touch() {
	d.mu.Lock()
	d.lastUserInput = time.Now()
	d.mu.Unlock()
}

// MarkAgentActive 标记智能体活动
func (d *IdleDetector) MarkAgentActive() {
	d.mu.Lock()
	d.lastAgentActive = time.Now()
	d.mu.Unlock()
}

// MarkAgentIdle 重置智能体空闲计时器（使时间归零，表示智能体已空闲"很久"）
func (d *IdleDetector) MarkAgentIdle() {
	d.mu.Lock()
	d.lastAgentActive = time.Time{}
	d.mu.Unlock()
}

// IsIdle 检查是否处于空闲状态（用户无输入 AND 智能体无活动）
func (d *IdleDetector) IsIdle() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	now := time.Now()
	userIdle := now.Sub(d.lastUserInput) >= d.idleThreshold
	agentIdle := now.Sub(d.lastAgentActive) >= d.idleThreshold
	return userIdle && agentIdle
}

// IdleDuration 返回用户空闲时长
func (d *IdleDetector) IdleDuration() time.Duration {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return time.Since(d.lastUserInput)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// IdleTask — 空闲任务接口
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// TaskResult 任务执行结果
type TaskResult struct {
	Name    string
	Output  string
	Skipped bool
	Error   error
}

// IdleTask 空闲时执行的任务
type IdleTask interface {
	// Name 任务名称
	Name() string
	// Interval 最小执行间隔（避免频繁触发）
	Interval() time.Duration
	// Condition 额外的触发条件（nil 表示总是满足）
	Condition() func() bool
	// Run 执行任务
	Run() TaskResult
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Scheduler — 周期任务调度器
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// Scheduler 管理所有周期任务
type Scheduler struct {
	detector *IdleDetector
	tasks    []IdleTask
	lastRun  map[string]time.Time
	mu       sync.Mutex
	results  []TaskResult
}

// NewScheduler 创建调度器
func NewScheduler(detector *IdleDetector) *Scheduler {
	return &Scheduler{
		detector: detector,
		lastRun:  make(map[string]time.Time),
	}
}

// Register 注册任务
func (s *Scheduler) Register(task IdleTask) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
}

// Check 检查是否应执行任务（由 TickMsg 驱动）
// 返回需要执行的任务结果列表
func (s *Scheduler) Check(agentBusy bool) []TaskResult {
	if agentBusy {
		s.detector.MarkAgentActive()
		return nil
	}
	// 智能体空闲，重置空闲计时器以允许任务立即触发
	s.detector.MarkAgentIdle()
	if !s.detector.IsIdle() {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var results []TaskResult
	for _, task := range s.tasks {
		// 检查间隔
		if last, ok := s.lastRun[task.Name()]; ok {
			if now.Sub(last) < task.Interval() {
				continue
			}
		}
		// 检查额外条件
		if task.Condition() != nil && !task.Condition()() {
			continue
		}
		// 执行
		result := task.Run()
		s.lastRun[task.Name()] = now
		results = append(results, result)
	}

	s.results = append(s.results, results...)
	if len(s.results) > 50 {
		s.results = s.results[len(s.results)-50:]
	}
	return results
}

// Results 返回最近执行结果
func (s *Scheduler) Results() []TaskResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := make([]TaskResult, len(s.results))
	copy(r, s.results)
	return r
}

// Detector 返回空闲检测器
func (s *Scheduler) Detector() *IdleDetector {
	return s.detector
}

// Reload 清空现有任务并重新加载（用于 DB 任务变更后同步）
func (s *Scheduler) Reload(loadFn func(*Scheduler) error) error {
	s.mu.Lock()
	s.tasks = nil
	s.lastRun = make(map[string]time.Time)
	s.mu.Unlock()
	return loadFn(s)
}

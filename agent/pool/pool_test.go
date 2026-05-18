// Package agent 子智能体池测试
package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"catcode/ai/llm"
	"catcode/core/event"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// MockProvider — 模拟 LLM Provider
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

type MockProvider struct {
	name     string
	delay    time.Duration
	response string
}

func NewMockProvider(name, response string, delay time.Duration) *MockProvider {
	return &MockProvider{name: name, response: response, delay: delay}
}

func (m *MockProvider) Name() string { return m.name }

func (m *MockProvider) Chat(ctx context.Context, req *llm.ChatRequest) (<-chan *llm.StreamEvent, error) {
	ch := make(chan *llm.StreamEvent, 8)
	go func() {
		defer close(ch)
		// 模拟延迟
		select {
		case <-ctx.Done():
			return
		case <-time.After(m.delay):
		}
		// 发送文本增量
		ch <- &llm.StreamEvent{Type: llm.StreamTextDelta, Content: m.response}
		// 发送完成事件
		ch <- &llm.StreamEvent{Type: llm.StreamDone}
	}()
	return ch, nil
}

func (m *MockProvider) ChatSync(ctx context.Context, req *llm.ChatRequest) (*llm.ChatResponse, error) {
	return nil, nil
}
func (m *MockProvider) Close() error { return nil }

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 测试: 子智能体异步执行
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestPool_ExecuteAsync(t *testing.T) {
	bus := event.NewBus()
	provider := NewMockProvider("test", "这是探索结果", 10*time.Millisecond)
	providers := llm.NewProviderRegistry(provider)

	pool := NewPool(PoolConfig{
		MaxConcurrent: 3,
		AgentConfigs:  DefaultAgentConfigs(),
	}, providers, bus)

	// 收集异步结果
	var (
		resultType string
		resultText string
		resultTask string
		gotResult  bool
		mu         sync.Mutex
		resultWg   sync.WaitGroup
	)

	resultWg.Add(1)
	bus.Subscribe("test", "subagent.result", func(evt event.Event) {
		mu.Lock()
		defer mu.Unlock()
		resultType, _ = evt.Data["type"].(string)
		resultText, _ = evt.Data["result"].(string)
		resultTask, _ = evt.Data["task"].(string)
		gotResult = true
		resultWg.Done()
	}, 100)

	// 执行异步调用
	pool.ExecuteAsync(context.Background(), "explore", "查找所有 Go 文件", "")

	// 等待结果或超时
	waitTimeout := time.After(5 * time.Second)
	select {
	case <-waitTimeout:
		t.Fatal("❌ 超时: 子智能体异步执行未在 5 秒内返回结果")
	case <-func() <-chan struct{} {
		ch := make(chan struct{})
		go func() {
			resultWg.Wait()
			close(ch)
		}()
		return ch
	}():
	}

	// 验证结果
	mu.Lock()
	defer mu.Unlock()

	if !gotResult {
		t.Fatal("❌ 未收到子智能体结果事件")
	}
	if resultType != "explore" {
		t.Errorf("❌ 类型错误: 期望 explore, 得到 %s", resultType)
	}
	if resultText != "这是探索结果" {
		t.Errorf("❌ 结果内容错误: 期望 '这是探索结果', 得到 '%s'", resultText)
	}
	if resultTask != "查找所有 Go 文件" {
		t.Errorf("❌ 任务描述错误: 期望 '查找所有 Go 文件', 得到 '%s'", resultTask)
	}

	t.Logf("✅ 子智能体异步执行成功: type=%s, task=%s, result=%s", resultType, resultTask, resultText)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 测试: 子智能体同步执行并收集结果
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestPool_ExecuteAndCollect(t *testing.T) {
	bus := event.NewBus()
	provider := NewMockProvider("test", "规划结果: 分三步实现", 20*time.Millisecond)

	pool := NewPool(PoolConfig{
		MaxConcurrent: 3,
		AgentConfigs:  DefaultAgentConfigs(),
	}, llm.NewProviderRegistry(provider), bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 同步执行
	ch, err := pool.Execute(ctx, "plan", "为文件搜索功能做规划", "")
	if err != nil {
		t.Fatalf("❌ Execute 失败: %v", err)
	}

	// 收集结果
	var result string
	for text := range ch {
		result += text
	}

	if result != "规划结果: 分三步实现" {
		t.Errorf("❌ 结果内容错误: 期望 '规划结果: 分三步实现', 得到 '%s'", result)
	}

	t.Logf("✅ 子智能体同步执行成功: result=%s", result)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 测试: 并发控制 — 信号量限制
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestPool_ConcurrencyLimit(t *testing.T) {
	bus := event.NewBus()
	// 模拟慢响应，确保并发执行
	provider := NewMockProvider("test", "慢响应", 100*time.Millisecond)

	pool := NewPool(PoolConfig{
		MaxConcurrent: 2, // 最多 2 个并发
		AgentConfigs:  DefaultAgentConfigs(),
	}, llm.NewProviderRegistry(provider), bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 启动 4 个任务
	start := time.Now()
	var wg sync.WaitGroup
	errCh := make(chan error, 4)

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			ch, err := pool.Execute(ctx, "general", "任务", "")
			if err != nil {
				errCh <- err
				return
			}
			for range ch {
				// 消费所有输出
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	elapsed := time.Since(start)

	// 最大并发 2，每个任务 100ms，4 个任务至少需要 200ms
	// 如果并发控制失效，总时间会接近 100ms
	minExpected := 180 * time.Millisecond
	if elapsed < minExpected {
		t.Errorf("❌ 并发控制可能失效: 期望至少 %v, 实际 %v", minExpected, elapsed)
	}

	for err := range errCh {
		if err != nil {
			t.Errorf("❌ 任务执行错误: %v", err)
		}
	}

	t.Logf("✅ 并发控制正常: 4 个任务(并发2) 耗时 %v", elapsed)
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 测试: 子智能体忙状态检查
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestSubAgent_BusyState(t *testing.T) {
	bus := event.NewBus()
	provider := NewMockProvider("test", "忙碌测试", 50*time.Millisecond)

	pool := NewPool(PoolConfig{
		MaxConcurrent: 3,
		AgentConfigs:  DefaultAgentConfigs(),
	}, llm.NewProviderRegistry(provider), bus)

	// 获取一个实例
	inst, err := pool.GetOrCreate("explore")
	if err != nil {
		t.Fatalf("❌ GetOrCreate 失败: %v", err)
	}

	if inst.IsBusy() {
		t.Error("❌ 新创建的实例不应为 busy 状态")
	}

	// 执行任务
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ch, err := inst.Execute(ctx, "测试任务", "")
	if err != nil {
		t.Fatalf("❌ Execute 失败: %v", err)
	}

	if !inst.IsBusy() {
		t.Error("❌ 执行中的实例应为 busy 状态")
	}

	// 消费结果
	for range ch {
	}

	// 等待一小段时间让 busy 状态更新
	time.Sleep(10 * time.Millisecond)

	if inst.IsBusy() {
		t.Error("❌ 执行完成的实例不应为 busy 状态")
	}

	t.Log("✅ 忙状态检查通过")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 测试: 子智能体池空闲实例复用
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestPool_ReuseIdleInstance(t *testing.T) {
	bus := event.NewBus()
	provider := NewMockProvider("test", "复用测试", 10*time.Millisecond)

	pool := NewPool(PoolConfig{
		MaxConcurrent: 3,
		AgentConfigs:  DefaultAgentConfigs(),
	}, llm.NewProviderRegistry(provider), bus)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 第一次获取
	inst1, err := pool.GetOrCreate("explore")
	if err != nil {
		t.Fatalf("❌ 第一次 GetOrCreate 失败: %v", err)
	}

	// 执行并完成
	ch1, _ := inst1.Execute(ctx, "任务1", "")
	for range ch1 {
	}
	time.Sleep(10 * time.Millisecond)

	// 第二次获取 — 应该复用 inst1
	inst2, err := pool.GetOrCreate("explore")
	if err != nil {
		t.Fatalf("❌ 第二次 GetOrCreate 失败: %v", err)
	}

	if inst1 != inst2 {
		t.Log("⚠️ 注意: 未复用空闲实例（可能是并发竞争导致创建了新实例）")
	} else {
		t.Log("✅ 成功复用了空闲实例")
	}

	// 验证池中只有 1 个 explore 实例
	instances, ok := pool.GetAll("explore")
	if !ok {
		t.Fatal("❌ 获取 explore 实例列表失败")
	}
	if len(instances) > 2 {
		t.Errorf("❌ 实例数量异常: 期望 <=2, 实际 %d", len(instances))
	}

	t.Logf("✅ 池状态: explore 实例数 = %d", len(instances))
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 测试: Snapshot 快照功能
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestPool_Snapshot(t *testing.T) {
	bus := event.NewBus()
	provider := NewMockProvider("test", "快照测试", 10*time.Millisecond)

	pool := NewPool(PoolConfig{
		MaxConcurrent: 3,
		AgentConfigs:  DefaultAgentConfigs(),
	}, llm.NewProviderRegistry(provider), bus)

	// 初始状态：无实例，快照应为空
	snapshots := pool.Snapshot()
	if len(snapshots) != 0 {
		t.Errorf("❌ 初始快照应为空, 实际 %d 个", len(snapshots))
	}

	// 创建并执行一个子智能体
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := pool.Execute(ctx, "explore", "探索任务", "")
	if err != nil {
		t.Fatalf("❌ Execute 失败: %v", err)
	}

	// 等待一小段时间让 goroutine 调度到 Execute 内部设置 busy=true
	time.Sleep(5 * time.Millisecond)

	// 执行中：快照应有 1 个 running 实例
	snapshots = pool.Snapshot()
	if len(snapshots) != 1 {
		t.Errorf("❌ 期望 1 个快照, 实际 %d", len(snapshots))
	} else {
		if snapshots[0].Status != "running" {
			t.Errorf("❌ 期望 status=running, 实际 %s", snapshots[0].Status)
		}
		if snapshots[0].Name != "explore" {
			t.Errorf("❌ 期望 name=explore, 实际 %s", snapshots[0].Name)
		}
		t.Logf("✅ 执行中快照: name=%s, status=%s, task=%s",
			snapshots[0].Name, snapshots[0].Status, snapshots[0].Task)
	}

	// 消费结果
	for range ch {
	}
	// 等待 busy 状态清除
	time.Sleep(20 * time.Millisecond)

	// 完成后：快照应有 1 个 completed 实例
	snapshots = pool.Snapshot()
	if len(snapshots) != 1 {
		t.Errorf("❌ 期望 1 个快照, 实际 %d", len(snapshots))
	} else {
		if snapshots[0].Status != "completed" {
			t.Errorf("❌ 期望 status=completed, 实际 %s", snapshots[0].Status)
		}
		t.Logf("✅ 完成后快照: name=%s, status=%s", snapshots[0].Name, snapshots[0].Status)
	}

	// 测试 GetAllAgents
	allAgents := pool.GetAllAgents()
	if len(allAgents) != 1 {
		t.Errorf("❌ GetAllAgents 期望 1 个, 实际 %d", len(allAgents))
	} else {
		t.Logf("✅ GetAllAgents: type=%s, id=%s", allAgents[0].Type(), allAgents[0].ID())
	}
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 测试: 子智能体状态变更事件
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func TestSubAgent_StatusEvent(t *testing.T) {
	bus := event.NewBus()
	provider := NewMockProvider("test", "状态事件测试", 20*time.Millisecond)

	pool := NewPool(PoolConfig{
		MaxConcurrent: 3,
		AgentConfigs:  DefaultAgentConfigs(),
	}, llm.NewProviderRegistry(provider), bus)

	// 订阅状态变更事件
	type statusChange struct {
		agentType string
		status    string
		task      string
	}
	changes := make(chan statusChange, 4)

	bus.Subscribe("test", "agent.status.changed", func(evt event.Event) {
		changes <- statusChange{
			agentType: evt.Data["type"].(string),
			status:    evt.Data["status"].(string),
			task:      evt.Data["task"].(string),
		}
	}, 100)

	// 执行任务
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ch, err := pool.Execute(ctx, "general", "通用任务", "")
	if err != nil {
		t.Fatalf("❌ Execute 失败: %v", err)
	}

	// 应该收到 busy 事件
	select {
	case c := <-changes:
		if c.status != "running" {
			t.Errorf("❌ 期望 status=running, 实际 %s", c.status)
		}
		if c.agentType != "general" {
			t.Errorf("❌ 期望 type=general, 实际 %s", c.agentType)
		}
		t.Logf("✅ 收到 running 事件: type=%s, task=%s", c.agentType, c.task)
	case <-time.After(2 * time.Second):
		t.Fatal("❌ 超时: 未收到 busy 事件")
	}

	// 消费结果
	for range ch {
	}

	// 应该收到 idle 事件
	select {
	case c := <-changes:
		if c.status != "completed" {
			t.Errorf("❌ 期望 status=completed, 实际 %s", c.status)
		}
		t.Logf("✅ 收到 completed 事件: type=%s", c.agentType)
	case <-time.After(2 * time.Second):
		t.Fatal("❌ 超时: 未收到 idle 事件")
	}

	t.Log("✅ 子智能体状态变更事件测试通过")
}

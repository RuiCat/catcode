package schedule

import (
	"strings"
	"time"

	"catcode/data/storage"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// DBTask — 数据库持久化的周期任务
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// DBTask 从 DB 加载的周期任务实现 IdleTask 接口
type DBTask struct {
	Row  *storage.ScheduledTask
	Wdb  storage.WorkspaceDB
	Busy func() bool // 检查是否有智能体在运行的函数
}

func (t *DBTask) Name() string            { return t.Row.Name }
func (t *DBTask) Interval() time.Duration { return time.Duration(t.Row.IntervalSeconds) * time.Second }
func (t *DBTask) Condition() func() bool {
	return func() bool {
		if !t.Row.Enabled {
			return false
		}
		if t.Busy != nil && t.Busy() {
			return false
		}
		return true
	}
}

func (t *DBTask) Run() TaskResult {
	// 检查任务是否已禁用
	if !t.Row.Enabled {
		return TaskResult{Name: t.Row.Name, Skipped: true}
	}
	// 检查智能体是否繁忙
	if t.Busy != nil && t.Busy() {
		return TaskResult{Name: t.Row.Name, Skipped: true, Output: "智能体运行中，跳过执行"}
	}

	// 标记任务执行时间
	if t.Wdb != nil {
		t.Wdb.MarkTaskRun(t.Row.ID)
	}

	// 执行任务描述中定义的简单操作
	output := t.executeTask()

	// run_once 任务执行后自动禁用
	if t.Row.RunOnce && t.Wdb != nil {
		t.Wdb.UpdateScheduledTask(t.Row.ID, t.Row.Name, t.Row.Description,
			t.Row.IntervalSeconds, false)
		t.Row.Enabled = false
		t.Row.RunOnce = false
	}

	return TaskResult{Name: t.Row.Name, Output: output}
}

// executeTask 记录任务检查结果（安全策略：不再执行从数据库读取的命令）
func (t *DBTask) executeTask() string {
	desc := strings.TrimSpace(t.Row.Description)
	if desc == "" {
		return "完成 (无具体操作描述)"
	}
	// 安全策略：周期任务仅用于提醒和状态检查，不执行任意 shell 命令
	// LLM 可修改 scheduled_tasks 表，执行命令存在注入风险
	return desc
}
// LoadDBTasks 从 DB 加载所有启用的任务到调度器
func LoadDBTasks(s *Scheduler, wdb storage.WorkspaceDB, busy func() bool) error {
	tasks, err := wdb.ListScheduledTasks()
	if err != nil {
		return err
	}
	for _, row := range tasks {
		if !row.Enabled {
			continue
		}
		s.Register(&DBTask{Row: row, Wdb: wdb, Busy: busy})
	}
	return nil
}

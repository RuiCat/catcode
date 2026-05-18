package schedule

import (
	"context"
	"fmt"
	"os/exec"
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
	if t.Busy == nil {
		return nil
	}
	return func() bool {
		return !t.Busy() // 仅在智能体空闲时触发
	}
}

func (t *DBTask) Run() TaskResult {
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
	return TaskResult{Name: t.Row.Name, Output: output}
}

// executeTask 执行任务描述中的简单操作
func (t *DBTask) executeTask() string {
	desc := strings.TrimSpace(t.Row.Description)
	if desc == "" {
		return "完成 (无具体操作描述)"
	}
	// 如果描述是一个可执行命令，尝试执行
	lines := strings.SplitN(desc, "\n", 2)
	cmdStr := strings.TrimSpace(lines[0])
	// 仅执行看起来像命令的描述（不以自然语言开头的）
	if !looksLikeCommand(cmdStr) {
		return fmt.Sprintf("任务: %s — 已检查 (非命令描述)", desc)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Sprintf("任务: %s — 执行失败: %v\n输出: %s", desc, err, string(output))
	}
	return fmt.Sprintf("任务: %s — 执行成功\n%s", desc, string(output))
}

func looksLikeCommand(s string) bool {
	return strings.Contains(s, " ") || strings.HasPrefix(s, "./") ||
		strings.HasPrefix(s, "go ") || strings.HasPrefix(s, "git ") ||
		strings.HasPrefix(s, "ls ") || strings.HasPrefix(s, "cat ") ||
		strings.HasPrefix(s, "echo ")
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

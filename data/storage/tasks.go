package storage

import "time"

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// ScheduledTask — 周期任务行
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ScheduledTask 周期任务记录
type ScheduledTask struct {
	ID              int64
	Name            string
	Description     string
	IntervalSeconds int
	Enabled         bool
	RunOnce         bool
	LastRun         *time.Time
	NextRun         *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ListScheduledTasks 列出所有周期任务
func (w *workspaceDBImpl) ListScheduledTasks() ([]*ScheduledTask, error) {
	rows, err := w.db.Query(`
		SELECT id, name, description, interval_seconds, enabled, run_once,
			last_run, next_run, created_at, updated_at
		FROM scheduled_tasks ORDER BY enabled DESC, created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []*ScheduledTask
	for rows.Next() {
		var t ScheduledTask
		var enabled int
		var runOnce int
		var lastRun, nextRun *time.Time
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.IntervalSeconds,
			&enabled, &runOnce, &lastRun, &nextRun, &t.CreatedAt, &t.UpdatedAt); err != nil {
			continue
		}
		t.Enabled = enabled != 0
		t.RunOnce = runOnce != 0
		t.LastRun = lastRun
		t.NextRun = nextRun
		result = append(result, &t)
	}
	return result, nil
}

// CreateScheduledTask 创建周期任务
func (w *workspaceDBImpl) CreateScheduledTask(name, description string, intervalSec int, runOnce bool) (*ScheduledTask, error) {
	runOnceVal := 0
	if runOnce {
		runOnceVal = 1
	}
	result, err := w.db.Exec(`
		INSERT INTO scheduled_tasks (name, description, interval_seconds, run_once)
		VALUES (?, ?, ?, ?)
	`, name, description, intervalSec, runOnceVal)
	if err != nil {
		return nil, err
	}
	id, _ := result.LastInsertId()
	return &ScheduledTask{
		ID: id, Name: name, Description: description,
		IntervalSeconds: intervalSec, Enabled: true,
		RunOnce: runOnce,
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}, nil
}

// UpdateScheduledTask 更新周期任务
func (w *workspaceDBImpl) UpdateScheduledTask(id int64, name, description string, intervalSec int, enabled bool) error {
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}
	_, err := w.db.Exec(`
		UPDATE scheduled_tasks SET name=?, description=?, interval_seconds=?, enabled=?, updated_at=CURRENT_TIMESTAMP
		WHERE id=?
	`, name, description, intervalSec, enabledInt, id)
	return err
}

// DeleteScheduledTask 删除周期任务
func (w *workspaceDBImpl) DeleteScheduledTask(id int64) error {
	_, err := w.db.Exec("DELETE FROM scheduled_tasks WHERE id=?", id)
	return err
}

// MarkTaskRun 标记任务已执行
func (w *workspaceDBImpl) MarkTaskRun(id int64) error {
	_, err := w.db.Exec(`
		UPDATE scheduled_tasks SET last_run=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE id=?
	`, id)
	return err
}

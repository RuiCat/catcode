package builtin

import (
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/data/storage"
	"catcode/tool"
	"fmt"
	"strings"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Schedule — 周期任务管理工具
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// ScheduleCreateTool 创建周期任务
func ScheduleCreateTool(wdb storage.WorkspaceDB, bus event.EventBus) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "schedule_create",
			Description: "创建周期任务。设置定时执行的后台任务，如代码检查、自动格式化等。interval_seconds 为执行间隔（秒），如 300=5分钟。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"name":             {Type: "string", Description: "任务名称"},
					"description":      {Type: "string", Description: "任务描述（也是执行内容）"},
					"interval_seconds": {Type: "integer", Description: "执行间隔（秒），默认300"},
					"run_once":         {Type: "boolean", Description: "是否仅执行一次，默认false"},
				},
				Required: []string{"name", "description"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			name, _ := args["name"].(string)
			desc, _ := args["description"].(string)
			interval := 300
			if v, ok := args["interval_seconds"].(float64); ok {
				interval = int(v)
			}
			runOnce := false
			if v, ok := args["run_once"].(bool); ok {
				runOnce = v
			}
			task, err := wdb.CreateScheduledTask(name, desc, interval, runOnce)
			if err != nil {
				return "", cerr.Wrap(err, "创建任务失败")
			}
			if bus != nil {
				bus.PublishAsync(event.EventTaskStarted, map[string]any{"action": "created", "id": task.ID})
				bus.PublishAsync(event.EventScheduledTaskChanged, map[string]any{"action": "created", "id": task.ID})
			}
			return fmt.Sprintf("✓ 任务已创建 #%d: %s (间隔 %ds)", task.ID, name, interval), nil
		},
	}
}

// ScheduleListTool 列出周期任务
func ScheduleListTool(wdb storage.WorkspaceDB) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "schedule_list",
			Description: "列出所有已创建的周期任务及其状态。",
			Parameters:  tool.MustMarshalSchema(tool.Schema{Type: "object", Properties: map[string]tool.Property{}}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			tasks, err := wdb.ListScheduledTasks()
			if err != nil {
				return "", err
			}
			if len(tasks) == 0 {
				return "暂无周期任务", nil
			}
			var sb strings.Builder
			for _, t := range tasks {
				status := "✅"
				if !t.Enabled {
					status = "⏸"
				}
				sb.WriteString(fmt.Sprintf("%s #%d %s (%ds) — %s\n",
					status, t.ID, t.Name, t.IntervalSeconds, t.Description))
			}
			return sb.String(), nil
		},
	}
}

// ScheduleDeleteTool 删除周期任务
func ScheduleDeleteTool(wdb storage.WorkspaceDB, bus event.EventBus) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "schedule_delete",
			Description: "删除指定的周期任务。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type:       "object",
				Properties: map[string]tool.Property{"id": {Type: "integer", Description: "任务ID"}},
				Required:   []string{"id"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			id, _ := args["id"].(float64)
			if err := wdb.DeleteScheduledTask(int64(id)); err != nil {
				return "", err
			}
			if bus != nil {
				bus.PublishAsync(event.EventTaskCompleted, map[string]any{"action": "deleted", "id": int64(id)})
				bus.PublishAsync(event.EventScheduledTaskChanged, map[string]any{"action": "deleted", "id": int64(id)})
			}
			return fmt.Sprintf("✓ 任务 #%d 已删除", int64(id)), nil
		},
	}
}

// ScheduleToggleTool 启用/禁用周期任务
func ScheduleToggleTool(wdb storage.WorkspaceDB, bus event.EventBus) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "schedule_toggle",
			Description: "启用或禁用指定周期任务。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"id":     {Type: "integer", Description: "任务ID"},
					"enable": {Type: "boolean", Description: "true=启用, false=禁用"},
				},
				Required: []string{"id", "enable"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			id, _ := args["id"].(float64)
			enable, _ := args["enable"].(bool)
			// 需要先获取原任务信息
			tasks, _ := wdb.ListScheduledTasks()
			for _, t := range tasks {
				if t.ID == int64(id) {
					wdb.UpdateScheduledTask(t.ID, t.Name, t.Description, t.IntervalSeconds, enable)
					if bus != nil {
						bus.PublishAsync(event.EventTaskCompleted, map[string]any{"action": "toggled", "id": t.ID})
						bus.PublishAsync(event.EventScheduledTaskChanged, map[string]any{"action": "toggled", "id": t.ID})
					}
					status := "启用"
					if !enable {
						status = "禁用"
					}
					return fmt.Sprintf("✓ 任务 #%d 已%s", t.ID, status), nil
				}
			}
			return "", cerr.Newf("任务 #%d 不存在", int64(id))
		},
	}
}

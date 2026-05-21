package main

import (
	"context"
	"encoding/json"

	"catcode/agent/orchestrator"
	"catcode/core/event"
	"catcode/schedule"
	"catcode/tool"
	"catcode/ui/tui"
)

// registerEventCallbacks 注册所有事件回调
func registerEventCallbacks(app *Application, arch orchestrator.ArchitectInterface) {
	app.Bus.Subscribe("role.list", "role.list", func(evt event.Event) {
		listRolesHandler(app)
	}, 10)

	app.Bus.Subscribe("tui.agents", event.EventAgentStatusChanged, func(evt event.Event) {
		if app.TUIProgram != nil {
			agents := make([]tui.AgentEntry, 0)
			if app.AgentPool != nil {
				for _, snap := range app.AgentPool.Snapshot() {
					agents = append(agents, tui.AgentEntry{
						Name:        snap.Name,
						ID:          snap.ID,
						Status:      snap.Status,
						Task:        snap.Task,
						FullTask:    snap.FullTask,
						CurrentTool: snap.CurrentTool,
						ToolCount:   snap.ToolCount,
						StartTime:   snap.StartTime,
						Duration:    snap.Duration,
						ErrorMsg:    snap.ErrorMsg,
						FullOutput:  snap.FullOutput,
					})
				}
			}
			app.TUIProgram.Send(tui.UpdateAgentsMsg{Agents: agents})
		}
	}, 100)

	app.Bus.Subscribe("tui.plan", event.EventPlanCreated, func(evt event.Event) { sendPlanUpdate(app) }, 100)
	app.Bus.Subscribe("tui.plan", event.EventPlanStepStart, func(evt event.Event) { sendPlanUpdate(app) }, 100)
	app.Bus.Subscribe("tui.plan", event.EventPlanStepDone, func(evt event.Event) { sendPlanUpdate(app) }, 100)
	app.Bus.Subscribe("tui.plan", event.EventPlanCompleted, func(evt event.Event) { sendPlanUpdate(app) }, 100)

	// 注册猫猫陪伴事件（侧边栏状态同步）
	app.Bus.Subscribe("companion", event.EventCompanionRespond, func(evt event.Event) {
		if app.TUIProgram != nil {
			// 更新侧边栏猫猫状态
			mood, _ := evt.Data["mood"].(string)
			intimacy, _ := evt.Data["intimacy"].(int)
			excitement, _ := evt.Data["excitement"].(int)
			shyness, _ := evt.Data["shyness"].(int)
			fatigue, _ := evt.Data["fatigue"].(int)
			app.TUIProgram.Send(tui.UpdateCompanionMsg{
				Mood: mood, Intimacy: intimacy,
				Excitement: excitement, Shyness: shyness, Fatigue: fatigue,
			})
		}
	}, 100)

	// 注册对话框消息事件（send_message 工具使用）
	app.Bus.Subscribe("dialog", event.EventDialogSend, func(evt event.Event) {
		if app.TUIProgram != nil {
			msg, _ := evt.Data["message"].(string)
			sender, _ := evt.Data["sender"].(string)
			if msg != "" {
				app.TUIProgram.Send(tui.AddMessageMsg{
					Type:    tui.MsgAssistant,
					Content: msg,
					Sender:  sender,
				})
			}
		}
	}, 100)

	// 注册选项框事件（question 工具使用）
	app.Bus.Subscribe("question", event.EventQuestionAsked, func(evt event.Event) {
		if app.TUIProgram == nil {
			return
		}
		questionsJSON, _ := evt.Data["questions"].(string)
		replyCh, _ := evt.Data["reply"].(chan tool.QuestionAnswer)

		if questionsJSON == "" {
			return // questions 为空，不发送空消息
		}
		// 解析问题列表
		var questions []tui.QuestionInfo
		if err := json.Unmarshal([]byte(questionsJSON), &questions); err != nil || len(questions) == 0 {
			return // 解析失败或无问题，不发送空消息
		}

		// 发送给 TUI，回答通过 replyCh 回传
		app.TUIProgram.Send(tui.QuestionRequestMsg{
			Questions: questions,
			ReplyCh:   replyCh,
		})
	}, 100)

	// 注册周期任务变更事件（侧边栏同步 + Scheduler 同步）
	app.Bus.Subscribe("schedule.sync", event.EventTaskStarted, func(evt event.Event) {
		reloadTasks(app)
	}, 100)
	app.Bus.Subscribe("schedule.sync", event.EventTaskCompleted, func(evt event.Event) {
		reloadTasks(app)
	}, 100)
	app.Bus.Subscribe("schedule.sync", event.EventScheduledTaskChanged, func(evt event.Event) {
		if app.Scheduler != nil && app.Wdb != nil {
			app.Scheduler.Reload(func(s *schedule.Scheduler) error {
				return schedule.LoadDBTasks(s, app.Wdb, func() bool {
					return app.AgentPool != nil && app.AgentPool.ActiveCount() > 0
				})
			})
		}
	}, 10)

	// 注册周期任务触发事件
	app.Bus.Subscribe("scheduled", event.EventScheduledTaskTrigger, handleScheduledTaskTrigger(app, arch), 10)
}

// sendPlanUpdate 发送规划更新到 TUI
func sendPlanUpdate(app *Application) {
	if app.TUIProgram == nil || app.PlanEngine == nil {
		return
	}
	plan := app.PlanEngine.GetActivePlan()
	if plan == nil {
		app.TUIProgram.Send(tui.UpdateTodosMsg{Todos: []tui.TodoEntry{}})
		return
	}
	todos := make([]tui.TodoEntry, len(plan.Todos))
	for i, t := range plan.Todos {
		todos[i] = tui.TodoEntry{Content: t.Content, Status: string(t.Status)}
	}
	app.TUIProgram.Send(tui.UpdateTodosMsg{Todos: todos})
}

// reloadTasks 从 DB 重新加载周期任务到调度器和 TUI 侧边栏
func reloadTasks(app *Application) {
	if app.Wdb == nil || app.Scheduler == nil {
		return
	}
	app.Scheduler.Reload(func(s *schedule.Scheduler) error {
		return schedule.LoadDBTasks(s, app.Wdb, func() bool {
			return app.AgentPool != nil && app.AgentPool.ActiveCount() > 0
		})
	})
	tasks, err := app.Wdb.ListScheduledTasks()
	if err != nil {
		return
	}
	infos := make([]tui.ScheduledTaskInfo, 0, len(tasks))
	for _, t := range tasks {
		enabled := t.Enabled
		infos = append(infos, tui.ScheduledTaskInfo{
			ID: t.ID, Name: t.Name, Description: t.Description,
			IntervalSeconds: t.IntervalSeconds, Enabled: enabled,
			RunOnce: t.RunOnce,
		})
	}
	if app.TUIProgram != nil {
		go func() { app.TUIProgram.Send(tui.UpdateTasksMsg{Tasks: infos}) }()
	}
}

// handleScheduledTaskTrigger 处理周期任务触发
func handleScheduledTaskTrigger(app *Application, arch orchestrator.ArchitectInterface) func(e event.Event) {
	return func(e event.Event) {
		taskDesc, _ := e.Data["description"].(string)
		taskName, _ := e.Data["name"].(string)
		if taskDesc == "" || taskName == "" {
			return
		}
		go func() {
			app.ArchBusy.Store(true)
			app.Scheduler.Detector().MarkAgentActive()
			ch, err := arch.ProcessInput(context.Background(), taskDesc)
			if err != nil {
				app.ArchBusy.Store(false)
				return
			}
			for msg := range ch {
				if app.TUIProgram != nil {
					app.TUIProgram.Send(tui.StreamMsg(msg))
				}
			}
			if app.TUIProgram != nil {
				app.TUIProgram.Send(tui.StreamDoneMsg{})
			}
			app.ArchBusy.Store(false)
		}()
	}
}

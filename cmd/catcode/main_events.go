package main

import (
	"encoding/json"

	"catcode/core/event"
	"catcode/tool"
	"catcode/ui/tui"
)

// registerEventCallbacks 注册所有事件回调
func registerEventCallbacks(app *Application) {
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

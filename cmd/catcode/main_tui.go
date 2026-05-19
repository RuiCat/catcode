package main

import (
	"context"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"catcode/agent/orchestrator"
	"catcode/core/config"
	"catcode/tool/builtin"
	"catcode/ui/tui"

	tea "github.com/charmbracelet/bubbletea"
)

// runTUI 启动 TUI 模式
func runTUI(arch orchestrator.ArchitectInterface, cfg *config.Config, app *Application) {
	// 从 DB 读取侧边栏宽度（默认 28）
	sidebarWidth := 28
	if app.Wdb != nil {
		if v, _, err := app.Wdb.GetSetting("tui.sidebar_width"); err == nil && v != "" {
			fmt.Sscanf(v, "%d", &sidebarWidth)
		}
	}

	app.TUIModel = tui.New(arch.GetSession().Model, arch.GetSession().ToolCount(), app.RoleReg.Count(), sidebarWidth,
		buildInputHandler(app, arch),
	)

	loadTUIState(app, arch)

	app.TUIProgram = tea.NewProgram(
		app.TUIModel,
		tea.WithAltScreen(),
		tea.WithInputTTY(),
		tea.WithMouseCellMotion(),
	)

	// 发送初始猫猫状态到侧边栏
	builtin.PublishInitialCompanionStatus(app.Bus)

	// 启动时同步插件面板到侧边栏
	if app.UIAPI != nil {
		apiPanels := app.UIAPI.GetPanels()
		if len(apiPanels) > 0 {
			panels := make(map[string]tui.PluginPanel, len(apiPanels))
			for k, p := range apiPanels {
				panels[k] = tui.PluginPanel{Key: p.Key, Title: p.Title, Content: p.Content}
			}
			app.TUIModel.Update(tui.UpdatePluginPanelsMsg{Panels: panels, ActivateFirst: true})
			for k := range panels {
				app.TUIModel.Update(tui.ActivateSidebarTabMsg{Key: k})
				break
			}
		}
	}

	if _, err := app.TUIProgram.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI 错误: %v\n", err)
	}
}

// buildInputHandler 构建 TUI 输入处理闭包
func buildInputHandler(app *Application, arch orchestrator.ArchitectInterface) func(string) {
	return func(input string) {
		app.Scheduler.Detector().Touch() // 标记用户活动
		// 解析 @ 命令 — 直接调用子智能体池
		atCmd := tui.ParseAtCommand(input)
		if atCmd.IsAtCmd {
			app.TUIProgram.Send(tui.AddMessageMsg{
				Type:    tui.MsgUser,
				Content: fmt.Sprintf("@%s %s", atCmd.AgentType, atCmd.Task),
			})
			app.TUIProgram.Send(tui.StreamMsg(fmt.Sprintf("\n🤖 调用 @%s 子智能体...\n", atCmd.AgentType)))
			// 直接通过池同步执行，流式输出到 TUI
			// 使用 context.Background() 因为 TUI 输入回调中无可取消的请求上下文
			ctx := context.Background()
			ch, err := app.AgentPool.Execute(ctx, atCmd.AgentType, atCmd.Task, arch.BuildSubAgentContext(atCmd.Task, atCmd.AgentType))
			if err != nil {
				app.TUIProgram.Send(tui.StreamMsg(fmt.Sprintf("❌ 启动失败: %v\n", err)))
				app.TUIProgram.Send(tui.StreamDoneMsg{})
				return
			}
			var result strings.Builder
			for text := range ch {
				result.WriteString(text)
				app.TUIProgram.Send(tui.StreamMsg(text))
			}
			// 注入主会话保留上下文 — 使用 system 消息避免伪造 tool_call_id 导致 API 400 错误
			arch.GetSession().AddMessage("system",
				fmt.Sprintf("[@%s 执行完成] 结果:\n%s", atCmd.AgentType, result.String()))
			app.TUIProgram.Send(tui.StreamDoneMsg{})
			app.TUIProgram.Send(tui.StatusMsg{MsgCount: arch.GetSession().MessageCount()})
			// 自动保存会话（异步，不阻塞 UI）
			if app.Wdb != nil {
				go func() {
					defer func() {
						if r := recover(); r != nil {
							fmt.Fprintf(os.Stderr, "panic in save goroutine: %v\n%s", r, debug.Stack())
						}
					}()
					sess := arch.GetSession()
					conv := sess.ToConversationRow()
					msgs := sess.ToMessageRows()
					_ = app.Wdb.SaveConversation(conv, msgs)
				}()
			}
			return
		}

		// 使用 context.Background() 因为 TUI 输入回调中无可取消的请求上下文
		ctx := context.Background()
		ch, err := arch.ProcessInput(ctx, input)
		if err != nil {
			app.TUIProgram.Send(tui.AddMessageMsg{Type: tui.MsgError, Content: err.Error()})
			return
		}
		for text := range ch {
			app.TUIProgram.Send(tui.StreamMsg(text))
		}
		app.TUIProgram.Send(tui.StreamDoneMsg{})
		app.TUIProgram.Send(tui.StatusMsg{MsgCount: arch.GetSession().MessageCount()})
		// 自动保存会话（异步，不阻塞 UI）
		if app.Wdb != nil {
			go func() {
				defer func() {
					if r := recover(); r != nil {
						fmt.Fprintf(os.Stderr, "panic in save goroutine: %v\n%s", r, debug.Stack())
					}
				}()
				sess := arch.GetSession()
				conv := sess.ToConversationRow()
				msgs := sess.ToMessageRows()
				_ = app.Wdb.SaveConversation(conv, msgs)
			}()
		}

		// 更新规划状态
		if app.PlanEngine != nil {
			plan := app.PlanEngine.GetActivePlan()
			if plan != nil {
				todos := make([]tui.TodoEntry, len(plan.Todos))
				for i, t := range plan.Todos {
					todos[i] = tui.TodoEntry{Content: t.Content, Status: string(t.Status)}
				}
				app.TUIProgram.Send(tui.UpdateTodosMsg{Todos: todos})
			} else {
				app.TUIProgram.Send(tui.UpdateTodosMsg{Todos: []tui.TodoEntry{}})
			}
		}

		// 更新子智能体状态
		if app.AgentPool != nil {
			agents := make([]tui.AgentEntry, 0)
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
			app.TUIProgram.Send(tui.UpdateAgentsMsg{Agents: agents})
		}
	}
}

// loadTUIState 加载 TUI 侧边栏状态（智能体列表、周期任务、会话信息等）
func loadTUIState(app *Application, arch orchestrator.ArchitectInterface) {
	// 侧边栏宽度变更时保存到 DB
	app.TUIModel.SetOnSidebarWidthChange(func(w int) {
		if app.Wdb != nil {
			app.Wdb.SetSetting("tui.sidebar_width", fmt.Sprintf("%d", w), "int", "user")
		}
	})

	// 从 DB 获取智能体描述列表（供 @mention autocomplete）
	if defs, err := app.Wdb.GetAllAgentDefinitions(); err == nil {
		agents := make([]tui.AgentInfo, 0, len(defs))
		for _, d := range defs {
			if d.Mode == "subagent" && d.Enabled {
				agents = append(agents, tui.AgentInfo{
					Name:        d.Name,
					Description: d.Description,
				})
			}
		}
		app.TUIModel.SetAgentList(agents)
	}

	// 周期任务：空闲时检查 + 执行
	app.TUIModel.SetOnTick(func() {
		// 同步插件面板到侧边栏
		if app.UIAPI != nil {
			apiPanels := app.UIAPI.GetPanels()
			if len(apiPanels) > 0 {
				panels := make(map[string]tui.PluginPanel, len(apiPanels))
				for k, p := range apiPanels {
					panels[k] = tui.PluginPanel{Key: p.Key, Title: p.Title, Content: p.Content}
				}
				app.TUIModel.Update(tui.UpdatePluginPanelsMsg{Panels: panels})
			}
		}

		agentBusy := app.AgentPool != nil && app.AgentPool.ActiveCount() > 0
		results := app.Scheduler.Check(agentBusy)
		for _, r := range results {
			if r.Error != nil {
				continue
			}
			if r.Skipped {
				continue
			}
			// 空闲任务结果输出到日志
			if r.Output != "" && app.TUIProgram != nil {
				app.TUIProgram.Send(tui.UpdateLogMsg{
					Time:    time.Now().Format("15:04:05"),
					Content: "⏰ " + r.Name + ": " + r.Output,
					Level:   "info",
				})
			}
		}
	})

	// 发送周期任务列表到 TUI
	if tasks, err := app.Wdb.ListScheduledTasks(); err == nil {
		infos := make([]tui.ScheduledTaskInfo, len(tasks))
		for i, t := range tasks {
			enabled := false
			if t.Enabled {
				enabled = true
			}
			infos[i] = tui.ScheduledTaskInfo{
				ID: t.ID, Name: t.Name, Description: t.Description,
				IntervalSeconds: t.IntervalSeconds, Enabled: enabled,
			}
		}
		app.TUIModel.Update(tui.UpdateTasksMsg{Tasks: infos})
	}

	// 发送会话信息到侧边栏（使用全局变量）
	app.TUIModel.Update(tui.SessionInfoMsg{
		WorkspacePath:  app.WorkDir,
		PluginCount:    len(app.PluginMgr.List()),
		MCPServerCount: app.McpMgr.ServerCount(),
	})
	list, err := app.Wdb.ListConversations()
	if err == nil {
		sessionInfos := make([]tui.SessionInfo, len(list))
		for i, s := range list {
			sessionInfos[i] = tui.SessionInfo{
				ID:           s.ID,
				Model:        s.Model,
				MessageCount: s.MessageCount,
				IsActive:     s.ID == arch.GetSession().ID,
			}
		}
		app.TUIModel.Update(tui.UpdateSessionsMsg{Sessions: sessionInfos})
	}
}

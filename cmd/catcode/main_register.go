package main

import (
	"fmt"
	"os"

	"catcode/agent/orchestrator"
	cerr "catcode/core/errors"
	"catcode/tool"
	"catcode/tool/builtin"
)

// registerBuiltinTools 注册所有内置工具
func registerBuiltinTools(arch orchestrator.ArchitectInterface, app *Application) {
	deps := builtin.ToolDeps{
		Wdb:           app.Wdb,
		MemoryService: app.MemoryService,
		Bus:           app.Bus,
		Provider:      app.Provider,
		RoleReg:       app.RoleReg,
		PlanEngine:    app.PlanEngine,
	}
	// 主智能体禁用的工具
	skipTools := map[string]bool{
		"bash": true, // 主智能体禁用 bash，命令执行通过子智能体委派
	}
	for name, factory := range builtin.BuiltinRegistry {
		if skipTools[name] {
			continue
		}
		t := factory(deps)
		if t == nil {
			continue
		}
		if err := arch.RegisterTool(t); err != nil {
			fmt.Fprintf(os.Stderr, "注册工具 %s 失败: %v\n", name, err)
		}
	}

	// Task 工具（子智能体委派）
	arch.RegisterTool(&tool.Tool{
		Function: tool.FuncDef{
			Name:        "task",
			Description: "委派任务给子智能体。当需要搜索代码库时用 explore，需要架构设计时用 plan，需要多文件修改时用 general，需要代码审查时用 reviewer，需要验证测试时用 verifier。子智能体会独立执行并返回完整结果。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"subagent_type": {Type: "string", Description: "子智能体类型",
						Enum: []string{"explore", "plan", "general", "reviewer", "verifier", "guard", "lean4"}},
					"description": {Type: "string", Description: "任务描述"},
				},
				Required: []string{"subagent_type", "description"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			subType, _ := args["subagent_type"].(string)
			desc, _ := args["description"].(string)
			if app.AgentPool != nil {
				app.AgentPool.ExecuteAsync(ctx.Ctx, subType, desc, arch.BuildSubAgentContext(desc, subType))
				return fmt.Sprintf("[task] 子智能体 %s 已启动: %s", subType, desc), nil
			}
			return fmt.Sprintf("[task] 子智能体 %s: %s", subType, desc), nil
		},
	})

	// Todo 工具
	arch.RegisterTool(&tool.Tool{
		Function: tool.FuncDef{
			Name:        "todo",
			Description: "管理任务列表。创建/更新/查看规划中的任务进度。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"action": {Type: "string", Description: "操作",
						Enum: []string{"create", "update", "list"}},
					"description": {Type: "string", Description: "规划描述 (create时必填)"},
					"todos":       {Type: "string", Description: "JSON 任务列表 (create时必填)"},
				},
				Required: []string{"action"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			action, _ := args["action"].(string)
			switch action {
			case "create":
				desc, _ := args["description"].(string)
				todosJSON, _ := args["todos"].(string)
				if app.PlanEngine != nil {
					_, err := app.PlanEngine.CreatePlanFromJSON(desc, todosJSON)
					if err != nil {
						return "", cerr.Wrap(err, "创建规划失败")
					}
					return "✓ 规划已创建", nil
				}
				return "[todo] 规划已创建", nil
			case "list":
				if app.PlanEngine != nil {
					plan := app.PlanEngine.GetActivePlan()
					if plan != nil {
						return app.PlanEngine.ListTodos(plan.ID), nil
					}
				}
				return "[todo] 暂无活跃规划", nil
			default:
				return fmt.Sprintf("[todo] 操作: %s", action), nil
			}
		},
	})
}

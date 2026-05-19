package builtin

import (
	"catcode/agent/plan"
	cerr "catcode/core/errors"
	"catcode/tool"
)

// PlanEnterTool 创建 plan_enter 工具
func PlanEnterTool(planEngine plan.PlanEngineInterface) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "plan_enter",
			Description: "进入计划模式。在计划模式下，edit/write/bash 等修改工具被禁用，仅保留 read/glob/grep/webfetch/skill 等只读工具。用于在进行复杂实现前先规划方案。使用 plan_exit 退出计划模式。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"reason": {Type: "string", Description: "进入计划模式的原因（可选）"},
				},
				Required: []string{},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			reason, _ := args["reason"].(string)
			if planEngine == nil {
				return "", cerr.Newf("plan_enter: 规划引擎未初始化")
			}
			return planEngine.EnterPlanMode(reason)
		},
	}
}

// PlanExitTool 创建 plan_exit 工具
func PlanExitTool(planEngine plan.PlanEngineInterface) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "plan_exit",
			Description: "退出计划模式，恢复所有工具的使用权限。在退出时需要提供接下来要做什么的说明。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"response": {Type: "string", Description: "退出计划模式后要做什么的说明（必填）"},
				},
				Required: []string{"response"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			response, _ := args["response"].(string)
			if response == "" {
				return "", cerr.Newf("plan_exit: response 参数不能为空，请说明退出计划模式后要做什么")
			}
			if planEngine == nil {
				return "", cerr.Newf("plan_exit: 规划引擎未初始化")
			}
			return planEngine.ExitPlanMode(response)
		},
	}
}

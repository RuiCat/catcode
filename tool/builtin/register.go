// Package builtin 提供 catcode 内置工具的注册与实现，包括文件操作、命令执行、网络请求等核心工具。
package builtin

import (
	"catcode/agent/plan"
	"catcode/agent/role"
	"catcode/ai/llm"
	"catcode/core/event"
	"catcode/data/storage"
	"catcode/tool"
)

// ToolDeps 工具创建所需的运行时依赖
// 所有内置工具通过 BuiltinRegistry 统一注册，
// registerBuiltinTools 和 ToolFactory 均从此读取，消除双重列举。
type ToolDeps struct {
	Wdb           storage.WorkspaceDB
	MemoryService storage.MemoryService
	Bus           event.EventBus
	Provider      llm.Provider
	RoleReg       role.RegistryInterface
	PlanEngine    plan.PlanEngineInterface
}

// ToolFactoryFunc 工具工厂函数类型
type ToolFactoryFunc func(deps ToolDeps) *tool.Tool

// BuiltinRegistry 内置工具注册表
// 所有内置工具在此注册，registerBuiltinTools 和 ToolFactory 均从此读取。
var BuiltinRegistry = make(map[string]ToolFactoryFunc)

func init() {
	// 无依赖工具
	BuiltinRegistry["read"] = func(deps ToolDeps) *tool.Tool { return ReadTool() }
	BuiltinRegistry["write"] = func(deps ToolDeps) *tool.Tool { return WriteTool() }
	BuiltinRegistry["edit"] = func(deps ToolDeps) *tool.Tool { return EditTool() }
	BuiltinRegistry["glob"] = func(deps ToolDeps) *tool.Tool { return GlobTool() }
	BuiltinRegistry["grep"] = func(deps ToolDeps) *tool.Tool { return GrepTool() }
	BuiltinRegistry["bash"] = func(deps ToolDeps) *tool.Tool { return BashTool() }
	BuiltinRegistry["diff"] = func(deps ToolDeps) *tool.Tool { return DiffTool() }
	BuiltinRegistry["webfetch"] = func(deps ToolDeps) *tool.Tool { return WebFetchTool() }
	BuiltinRegistry["skill"] = func(deps ToolDeps) *tool.Tool { return SkillTool() }
	BuiltinRegistry["apply_patch"] = func(deps ToolDeps) *tool.Tool { return ApplyPatchTool() }
	BuiltinRegistry["log_issue"] = func(deps ToolDeps) *tool.Tool { return LogIssueTool() }

	// 依赖 Bus 的工具
	BuiltinRegistry["send_message"] = func(deps ToolDeps) *tool.Tool { return SendMessageTool(deps.Bus) }
	BuiltinRegistry["question"] = func(deps ToolDeps) *tool.Tool { return QuestionTool(deps.Bus) }

	// 依赖 Provider + RoleReg + Bus 的工具
	BuiltinRegistry["companion_talk"] = func(deps ToolDeps) *tool.Tool {
		return CompanionTalkTool(deps.Provider, deps.RoleReg, deps.Bus)
	}

	// 依赖 Wdb 的工具
	BuiltinRegistry["db_query"] = func(deps ToolDeps) *tool.Tool {
		if deps.Wdb != nil {
			return DBQueryTool(deps.Wdb)
		}
		return nil
	}
	BuiltinRegistry["db_exec"] = func(deps ToolDeps) *tool.Tool {
		if deps.Wdb != nil {
			return DBExecTool(deps.Wdb)
		}
		return nil
	}
	BuiltinRegistry["db_tables"] = func(deps ToolDeps) *tool.Tool {
		if deps.Wdb != nil {
			return DBTablesTool(deps.Wdb)
		}
		return nil
	}
	BuiltinRegistry["go_run"] = func(deps ToolDeps) *tool.Tool {
		if deps.Wdb != nil {
			return GoRunTool(deps.Wdb)
		}
		return nil
	}
	BuiltinRegistry["schedule_create"] = func(deps ToolDeps) *tool.Tool {
		if deps.Wdb != nil {
			return ScheduleCreateTool(deps.Wdb, deps.Bus)
		}
		return nil
	}
	BuiltinRegistry["schedule_list"] = func(deps ToolDeps) *tool.Tool {
		if deps.Wdb != nil {
			return ScheduleListTool(deps.Wdb)
		}
		return nil
	}
	BuiltinRegistry["schedule_delete"] = func(deps ToolDeps) *tool.Tool {
		if deps.Wdb != nil {
			return ScheduleDeleteTool(deps.Wdb, deps.Bus)
		}
		return nil
	}
	BuiltinRegistry["schedule_toggle"] = func(deps ToolDeps) *tool.Tool {
		if deps.Wdb != nil {
			return ScheduleToggleTool(deps.Wdb, deps.Bus)
		}
		return nil
	}

	// 依赖 MemoryService 的工具
	BuiltinRegistry["memory_set"] = func(deps ToolDeps) *tool.Tool {
		if deps.MemoryService != nil {
			return MemorySetTool(deps.MemoryService)
		}
		return nil
	}
	BuiltinRegistry["memory_get"] = func(deps ToolDeps) *tool.Tool {
		if deps.MemoryService != nil {
			return MemoryGetTool(deps.MemoryService)
		}
		return nil
	}
	BuiltinRegistry["memory_search"] = func(deps ToolDeps) *tool.Tool {
		if deps.MemoryService != nil {
			return MemorySearchTool(deps.MemoryService)
		}
		return nil
	}
	BuiltinRegistry["memory_list"] = func(deps ToolDeps) *tool.Tool {
		if deps.MemoryService != nil {
			return MemoryListTool(deps.MemoryService)
		}
		return nil
	}
	BuiltinRegistry["memory_delete"] = func(deps ToolDeps) *tool.Tool {
		if deps.MemoryService != nil {
			return MemoryDeleteTool(deps.MemoryService)
		}
		return nil
	}

	// 依赖 PlanEngine 的工具（仅主智能体使用）
	BuiltinRegistry["plan_enter"] = func(deps ToolDeps) *tool.Tool {
		if deps.PlanEngine != nil {
			return PlanEnterTool(deps.PlanEngine)
		}
		return nil
	}
	BuiltinRegistry["plan_exit"] = func(deps ToolDeps) *tool.Tool {
		if deps.PlanEngine != nil {
			return PlanExitTool(deps.PlanEngine)
		}
		return nil
	}
}

// ToolFactory 返回一个工具工厂函数，根据名称创建工具实例
func ToolFactory(provider llm.Provider, roleReg role.RegistryInterface, bus event.EventBus,
	wdb storage.WorkspaceDB, memoryService storage.MemoryService) func(string) *tool.Tool {
	deps := ToolDeps{
		Provider:      provider,
		RoleReg:       roleReg,
		Bus:           bus,
		Wdb:           wdb,
		MemoryService: memoryService,
	}
	return func(name string) *tool.Tool {
		if factory, ok := BuiltinRegistry[name]; ok {
			return factory(deps)
		}
		return nil
	}
}

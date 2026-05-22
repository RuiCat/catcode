package orchestrator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"catcode/ai/compact"
	"catcode/ai/llm"
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/core/utils"
	"catcode/tool"
)

// executeToolCalls 执行 LLM 请求的工具调用
func (a *Architect) executeToolCalls(ctx context.Context, toolCalls []*llm.ToolCall, responseCh chan<- string) {
	// 收集所有工具名
	var toolNames []string
	for _, tc := range toolCalls {
		toolNames = append(toolNames, tc.Function.Name)
	}
	// 发送工具执行开始标记（携带工具名，让 TUI 显示对应状态）
	responseCh <- "\n⚙️" + strings.Join(toolNames, ", ")

	// 收集工具执行期间的错误，延迟到循环结束后统一注入
	// 避免 system 消息插入在 tool 结果之间导致 API 400 错误
	var toolErrorsMsgs []string

	for _, tc := range toolCalls {
		// 解析参数
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			responseCh <- fmt.Sprintf("\n❌ 工具参数解析失败: %v", err)
			a.mainSession.AddToolResult(tc.ID, tc.Function.Name, fmt.Sprintf("参数解析失败: %v", err))
			continue
		}

		toolName := tc.Function.Name

		// Plan mode tool restriction check (must come before task/GetTool checks to avoid bypass)
		if a.planEngine != nil && a.planEngine.IsPlanMode() {
			if isRestrictedInPlanMode(toolName) {
				responseCh <- fmt.Sprintf("\n⛔ Plan 模式: %s 已禁用", toolName)
				a.mainSession.AddToolResult(tc.ID, toolName,
					fmt.Sprintf("⛔ 当前处于 Plan 模式，%s 工具已禁用。仅允许只读工具（read/glob/grep/webfetch/skill）。使用 plan_exit 退出 Plan 模式后重试。", toolName))
				continue
			}
		}

		// 检查是否是子智能体委派
		if toolName == "task" {
			// 处理子智能体委派 — 传入 LLM 原始 tool_call_id
			a.dispatchSubAgent(ctx, tc.ID, args, responseCh)
			continue
		}

		// 本地工具执行
		t, ok := a.mainSession.GetTool(toolName)
		if !ok {
			// 未知工具：尝试按角色名调度
			if subRole, ok := args["role"]; ok {
				roleName := fmt.Sprintf("%v", subRole)
				if inst, ok := a.roleReg.Get(roleName); ok && inst.Active {
					a.bus.Publish(event.EventRoleDispatch, map[string]any{
						"role":    roleName,
						"task":    args["task"],
						"context": args["context"],
					})
					responseCh <- fmt.Sprintf("\n📤 委派角色: %s", roleName)
					a.mainSession.AddToolResult(tc.ID, roleName, fmt.Sprintf("已委派角色 %s 执行任务", roleName))
					continue
				}
			}
			responseCh <- fmt.Sprintf("\n⚠️ 未知工具: %s", toolName)
			a.mainSession.AddToolResult(tc.ID, toolName, fmt.Sprintf("未知工具: %s", toolName))
			continue
		}

		// 执行工具（含权限检查）
		wd := a.workDir
		if wd == "" {
			wd = "."
		}
		toolCtx := &tool.Context{
			Ctx:        ctx,
			SessionID:  a.mainSession.ID,
			WorkDir:    wd,
			ToolCallID: tc.ID,
			Permission: a.checkPermission(toolName),
			Extra:      map[string]any{"session": a.mainSession},
		}
		if toolCtx.Permission == tool.Deny {
			a.mainSession.AddToolResult(tc.ID, toolName, fmt.Sprintf("权限拒绝: %s", toolName))
			errMsg := a.collectToolError(responseCh, cerr.CategoryPermission, cerr.Newf("工具 %s 被角色规则限制", toolName), "")
			if errMsg != "" {
				toolErrorsMsgs = append(toolErrorsMsgs, errMsg)
			}
			continue
		}
		result, err := t.Call(toolCtx, args)
		if err != nil {
			a.mainSession.AddToolResult(tc.ID, toolName, fmt.Sprintf("执行失败: %v", err))
			errMsg := a.collectToolError(responseCh, cerr.CategoryTool, err, fmt.Sprintf("工具: %s", toolName))
			if errMsg != "" {
				toolErrorsMsgs = append(toolErrorsMsgs, errMsg)
			}
			continue
		}

		displayName := toolName
		if toolName == "companion_talk" {
			displayName = "猫猫"
		}
		responseCh <- fmt.Sprintf("\n✅ [%s 完成]", displayName)
		a.mainSession.AddToolResult(tc.ID, displayName, result)
		a.planStatus = ""
	}

	// 所有 tool 结果添加完毕后，统一注入错误反馈（避免破坏 tool 消息配对）
	if feedback := a.toolErrors.FormatFeedback(); feedback != "" {
		a.mainSession.AddMessage("system", feedback)
	}

	// 工具执行完毕后，继续 LLM 循环
	// 递增 tool call 轮次计数器，防止无限递归
	a.toolCallRounds++
	if a.toolCallRounds > maxToolCallRounds {
		responseCh <- fmt.Sprintf("\n⚠️ tool call 轮次已达上限 (%d)，强制终止工具调用。", maxToolCallRounds)
		a.mainSession.AddMessage("system",
			fmt.Sprintf("【工具调用轮次已达上限 (%d)】请基于已获取的工具结果，直接给出最终回答，不要再调用任何工具。", maxToolCallRounds))
		return
	}
}

// prepareNextRound 准备下一轮 tool call 迭代前的上下文
func (a *Architect) prepareNextRound(ctx context.Context, responseCh chan<- string) {
	// 上下文压缩
	decision := compact.ShouldCompact(a.mainSession, a.config.ContextLimit)

	compressed := false
	if decision.Needed {
		responseCh <- fmt.Sprintf("\n📦 自动压缩中... (tokens=%d, level=%s)", decision.TokenCnt, decision.Level)

		if decision.Level == "full" {
			result := compact.BuildCompactResult(a.mainSession.Messages,
				a.mainSession.Summary, a.config.ContextLimit, decision.TokenCnt)
			compact.ApplyCompactResult(a.mainSession, result)
			responseCh <- fmt.Sprintf("\n🧠 上下文索引已重建 (保留最近 %d+ 条消息)", result.TailStartIndex)
		}

		if a.wdb != nil {
			messagesJSON := compact.SessionMessagesToJSON(a.mainSession.Messages)
			a.wdb.CreateSnapshot(a.mainSession.ID, "auto-compact", messagesJSON,
				a.mainSession.Summary, decision.TokenCnt)
		}

		if decision.Level == "micro" || decision.Level == "full" {
			compact.TrimOldToolOutputs(a.mainSession)
		}
		responseCh <- " ✓ 压缩完成"

		compressed = true

		// 压缩发生 → 上下文结构变化 → 强制刷新 MemoryIndex
		a.injectMemoryIndex()
		a.memoryIndexRounds = 0
		a.memoryIndexUpdatedAt = time.Now()
	}

	// 清理孤儿 tool_calls
	a.mainSession.CleanOrphanedToolCalls()

	// 边界条件触发 MemoryIndex 刷新（仅在压缩未触发时检查）
	if !compressed {
		a.memoryIndexRounds++
		shouldRefresh := a.memoryIndexRounds >= memoryIndexRefreshRounds ||
			time.Since(a.memoryIndexUpdatedAt) > memoryIndexRefreshInterval

		if shouldRefresh {
			a.injectMemoryIndex()
			a.memoryIndexRounds = 0
			a.memoryIndexUpdatedAt = time.Now()
		}
	}
}

// checkPermission 检查主智能体对工具的权限（从角色定义中获取）
func (a *Architect) checkPermission(toolName string) tool.PermissionLevel {
	primary := a.roleReg.GetPrimary()
	if primary == nil {
		return tool.Allow
	}
	permMap := primary.Def.Permission
	if len(permMap) == 0 {
		return tool.Allow
	}
	rules := tool.PermissionFromMap(permMap)
	checker := tool.NewPermissionChecker(rules)
	return checker.Check(toolName, "")
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 统一错误处理 — LLM 驱动自我纠正
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// NOTE: 此函数在 architect_tools.go 和 base_tools.go 中重复实现。
// 待 D-03 (runToolLoop 去重) 完成后统一提取到共享模块。
//
// collectToolError 收集工具执行错误（延迟注入版本）
// 与 handleError 相同逻辑，但不向 session 注入 system 消息，
// 而是返回错误描述字符串供调用方统一注入，避免破坏 tool 消息配对
func (a *Architect) collectToolError(responseCh chan<- string, category string, err error, context string) string {
	// thinking 模式特殊处理（在计数之前）
	if strings.Contains(err.Error(), "reasoning_content") {
		responseCh <- "\n⚠️ thinking 模式推理内容丢失，已清除思维链继续（不影响功能）。"
		a.clearThinkingState()
		return ""
	}

	errMsg, ok := a.toolErrors.Add(category, err, context)
	if !ok {
		responseCh <- fmt.Sprintf("\n⚠️ 连续 %d 次错误，停止自动纠正。请手动排查。", a.toolErrors.MaxErrors())
		return ""
	}
	responseCh <- fmt.Sprintf("\n🔄 自我纠正 #%d (%s)...", a.toolErrors.Count(), category)
	a.logError(category, "error", err.Error(), errStack(err), "architect")
	return errMsg
}

// handleError 统一错误处理入口
// 将错误上下文注入会话，由 LLM 自行分析并规划修复方案
// 返回 true 表示已处理（可继续对话），false 表示超过自纠正上限
func (a *Architect) handleError(responseCh chan<- string, category string, err error, context string) bool {
	// 不可恢复的错误：thinking 模式 reasoning_content 丢失（在计数之前）
	if strings.Contains(err.Error(), "reasoning_content") {
		responseCh <- "\n⚠️ thinking 模式推理内容丢失，已清除思维链继续（不影响功能）。"
		a.clearThinkingState()
		return false
	}

	errMsg, ok := a.toolErrors.Add(category, err, context)
	if !ok {
		responseCh <- fmt.Sprintf("\n⚠️ 连续 %d 次错误，停止自动纠正。请手动排查。", a.toolErrors.MaxErrors())
		return false
	}

	responseCh <- fmt.Sprintf("\n🔄 自我纠正 #%d (%s)...", a.toolErrors.Count(), category)
	a.mainSession.AddMessage("system",
		fmt.Sprintf("【错误反馈 #%d】%s。请分析错误原因，调整策略并尝试其他方法完成任务。", a.toolErrors.Count(), errMsg))
	a.logError(category, "error", err.Error(), errStack(err), "architect")
	return true
}

// logError 将错误持久化到数据库日志表
// 优先使用 CatError 自带的堆栈跟踪，其次捕获当前 goroutine 堆栈
func (a *Architect) logError(category, severity, message, stackTrace, source string) {
	if a.wdb == nil {
		return
	}
	if stackTrace == "" {
		stackTrace = utils.GetStack()
	}
	_ = a.wdb.LogError(category, severity, message, stackTrace, source, a.mainSession.ID) // 日志持久化失败不影响主流程
}

// resetErrors 每轮用户请求开始前重置计数
func (a *Architect) resetErrors() {
	a.toolErrors.Reset()
	a.toolCallRounds = 0
}

// planModeAllowedTools 定义计划模式下允许的工具白名单。
// 新增工具时需在此处评估是否允许在计划模式下使用。
// true=允许, false=明确禁止, 不在map中=默认禁止。
var planModeAllowedTools = map[string]bool{
	// 信息获取类
	"read": true, "glob": true, "grep": true, "webfetch": true,
	// 计划管理类
	"skill": true, "plan_enter": true, "plan_exit": true, "todo": true,
	// 交互类
	"question": true, "send_message": true, "companion_talk": true,
	// 架构类
	"ask_architect": true, "log_issue": true,
	// 明确禁止
	"task": false,
}

// isRestrictedInPlanMode 判断工具在计划模式下是否被禁止（true=禁止）
func isRestrictedInPlanMode(toolName string) bool {
	return !planModeAllowedTools[toolName]
}

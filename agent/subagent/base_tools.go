package subagent

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
	"catcode/tool"
)

// executeToolCalls 执行 LLM 请求的工具调用（子智能体简化版）
func (sa *BaseAgent) executeToolCalls(ctx context.Context, toolCalls []*llm.ToolCall, responseCh chan<- string) {
	var toolErrorsMsgs []string

	for _, tc := range toolCalls {
		var args map[string]any
		if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
			responseCh <- fmt.Sprintf("\n❌ 工具参数解析失败: %v", err)
			sa.session.AddToolResult(tc.ID, tc.Function.Name, fmt.Sprintf("参数解析失败: %v", err))
			continue
		}

		toolName := tc.Function.Name

		if toolName == "ask_architect" {
			question, _ := args["question"].(string)
			if sa.askArchitectFn != nil {
				responseCh <- fmt.Sprintf("\n💬 正在询问主智能体...")
				answer := sa.askArchitectFn(question)
				sa.session.AddToolResult(tc.ID, "ask_architect", answer)
				responseCh <- fmt.Sprintf("\n✅ 主智能体已回复")
			} else {
				sa.session.AddToolResult(tc.ID, "ask_architect", "⚠️ 通信通道未建立")
			}
			continue
		}

		toolPath := extractToolPath(toolName, args)

		perm := tool.Allow
		if sa.perms != nil {
			perm = sa.perms.Check(toolName, toolPath)
		}
		if perm == tool.Deny {
			sa.session.AddToolResult(tc.ID, toolName, fmt.Sprintf("权限拒绝: %s", toolName))
			errMsg := sa.collectToolError(responseCh, cerr.CategoryPermission, cerr.Newf("工具 %s 被权限规则限制", toolName), "")
			if errMsg != "" {
				toolErrorsMsgs = append(toolErrorsMsgs, errMsg)
			}
			continue
		}

		if perm == tool.Ask {
			if sa.askArchitectFn != nil {
				question := fmt.Sprintf("子智能体 [%s] 请求执行工具 %s，参数: %v。是否允许？(回答: 允许/拒绝)",
					sa.agentType, toolName, args)
				answer := sa.askArchitectFn(question)
				lower := strings.ToLower(answer)
				// 先检查明确的拒绝词
				denied := strings.Contains(lower, "拒绝") || strings.Contains(lower, "deny") ||
					strings.Contains(lower, "不允许") || strings.Contains(lower, "不要") ||
					strings.Contains(lower, "不准") || strings.Contains(lower, "禁止") ||
					strings.Contains(lower, "不行") || strings.Contains(lower, "不能") ||
					strings.Contains(lower, "不可以") || strings.HasPrefix(lower, "no")
				// 再检查明确的允许词（仅当不在否定上下文中）
				approved := !denied && (strings.Contains(lower, "允许") || strings.Contains(lower, "approve") ||
					strings.Contains(lower, "同意") || strings.HasPrefix(lower, "yes") ||
					strings.Contains(lower, "可以") || strings.HasPrefix(lower, "是") ||
					strings.HasPrefix(lower, "好") || strings.HasPrefix(lower, "行") ||
					strings.HasPrefix(lower, "ok"))
				if !approved {
					sa.session.AddToolResult(tc.ID, toolName,
						fmt.Sprintf("用户拒绝执行: %s (回复: %s)", toolName, answer))
					responseCh <- fmt.Sprintf("\n⛔ 用户拒绝: %s", toolName)
					errMsg := sa.collectToolError(responseCh, cerr.CategoryPermission,
						cerr.Newf("用户拒绝执行工具 %s: %s", toolName, answer), "")
					if errMsg != "" {
						toolErrorsMsgs = append(toolErrorsMsgs, errMsg)
					}
					continue
				}
			} else {
				// 没有通信通道，安全起见拒绝
				sa.session.AddToolResult(tc.ID, toolName,
					fmt.Sprintf("Ask权限需要用户确认，但通信通道未建立，已拒绝执行: %s", toolName))
				responseCh <- fmt.Sprintf("\n⛔ 无法确认: %s (无通信通道)", toolName)
				errMsg := sa.collectToolError(responseCh, cerr.CategoryPermission,
					cerr.Newf("工具 %s 需要用户确认(Ask)但无通信通道", toolName), "")
				if errMsg != "" {
					toolErrorsMsgs = append(toolErrorsMsgs, errMsg)
				}
				continue
			}
		}

		t, ok := sa.session.GetTool(toolName)
		if !ok {
			responseCh <- fmt.Sprintf("\n⚠️ 未知工具: %s", toolName)
			sa.session.AddToolResult(tc.ID, toolName, fmt.Sprintf("未知工具: %s", toolName))
			continue
		}

		toolCtx := &tool.Context{
			Ctx:        ctx,
			SessionID:  sa.session.ID,
			WorkDir:    sa.workDir,
			ToolCallID: tc.ID,
			Permission: perm,
			Extra:      map[string]any{"session": sa.session},
		}

		if toolName == "bash" && sa.guardReviewer != nil {
			toolCtx.GuardReviewer = func(command string) (bool, string) {
				guardResult := sa.reviewWithGuard(ctx, command)
				return guardResult.approved, guardResult.reason
			}
		}

		result, err := t.Call(toolCtx, args)
		if err != nil {
			sa.session.AddToolResult(tc.ID, toolName, fmt.Sprintf("执行失败: %v", err))
			errMsg := sa.collectToolError(responseCh, cerr.CategoryTool, err, fmt.Sprintf("工具: %s", toolName))
			if errMsg != "" {
				toolErrorsMsgs = append(toolErrorsMsgs, errMsg)
			}
			continue
		}

		responseCh <- fmt.Sprintf("\n✅ [%s 完成]", toolName)
		sa.session.AddToolResult(tc.ID, toolName, result)
	}

	if feedback := sa.toolErrors.FormatFeedback(); feedback != "" {
		sa.session.AddMessage("system", feedback)
	}

	sa.toolCallRounds++
	if sa.toolCallRounds > sa.maxToolCallRounds {
		responseCh <- fmt.Sprintf("\n⚠️ tool call 轮次已达上限 (%d)，强制终止工具调用。", sa.maxToolCallRounds)
		sa.session.AddMessage("system",
			fmt.Sprintf("【工具调用轮次已达上限 (%d)】请基于已获取的工具结果，直接给出最终回答，不要再调用任何工具。", sa.maxToolCallRounds))
		sa.mu.Lock()
		sa.currentTool = ""
		sa.status = "completed"
		sa.duration = time.Since(sa.startTime)
		sa.fullOutput = "[达到工具调用轮次上限，强制终止]"
		sa.mu.Unlock()
		sa.publishAgentStatus()
		if sa.bus != nil {
			sa.bus.PublishAsync(event.EventTaskCompleted, map[string]any{
				"agent":    sa.agentType,
				"id":       sa.id,
				"task":     sa.task,
				"result":   sa.fullOutput,
				"duration": sa.duration,
				"tools":    sa.toolCount,
			})
		}
	}
}

// prepareNextRound 准备下一轮 tool call 迭代前的上下文
func (sa *BaseAgent) prepareNextRound(responseCh chan<- string) {
	if sa.contextLimit > 0 {
		decision := compact.ShouldCompact(sa.session, sa.contextLimit)
		if decision.Needed {
			if decision.Level == "full" {
				result := compact.BuildCompactResult(sa.session.Messages,
					sa.session.Summary, sa.contextLimit, decision.TokenCnt)
				compact.ApplyCompactResult(sa.session, result)
			}
			if sa.wdb != nil {
				messagesJSON := compact.SessionMessagesToJSON(sa.session.Messages)
				sa.wdb.CreateSnapshot(sa.conversationID, "auto-compact", messagesJSON,
					sa.session.Summary, decision.TokenCnt)
			}
			compact.TrimOldToolOutputs(sa.session)
		}
	}
}

// runToolLoop 驱动 SubAgent 的 tool call 迭代循环（替代原递归模式）
func (sa *BaseAgent) runToolLoop(ctx context.Context, firstStreamCh <-chan *llm.StreamEvent, responseCh chan<- string, task string) {
	streamCh := firstStreamCh
	const maxRetries = 1

	for {
		if sa.toolCallRounds > 0 {
			sa.prepareNextRound(responseCh)
		}

		var result streamResult
		var streamErr error

		for attempt := 0; attempt <= maxRetries; attempt++ {
			result, streamErr = sa.processStream(ctx, streamCh, responseCh, task)
			if streamErr == nil {
				break
			}

			if attempt < maxRetries {
				req, err := sa.session.BuildRequest()
				if err != nil {
					responseCh <- fmt.Sprintf("\n❌ 构建后续请求失败: %v", err)
					sa.mu.Lock()
					sa.currentTool = ""
					sa.status = "error"
					sa.errorMsg = fmt.Sprintf("构建请求失败: %v", err)
					sa.duration = time.Since(sa.startTime)
					sa.mu.Unlock()
					sa.publishAgentStatus()
					if sa.bus != nil {
						sa.bus.PublishAsync(event.EventTaskFailed, map[string]any{
							"agent": sa.agentType,
							"id":    sa.id,
							"task":  task,
							"error": sa.errorMsg,
						})
					}
					return
				}
				newStreamCh, err := sa.provider.Chat(ctx, req)
				if err != nil {
					if cerr.IsRetryable(err) {
						continue
					}
					streamErr = err
					break
				}
				streamCh = newStreamCh
				continue
			}
		}

		if streamErr != nil {
			if strings.Contains(streamErr.Error(), "400") {
				sa.session.Clear()
			}
			sa.mu.Lock()
			sa.currentTool = ""
			if streamErr != nil {
				sa.errorMsg = streamErr.Error()
			}
			sa.status = "error"
			sa.duration = time.Since(sa.startTime)
			sa.mu.Unlock()
			sa.publishAgentStatus()
			if sa.bus != nil {
				sa.bus.PublishAsync(event.EventTaskFailed, map[string]any{
					"agent": sa.agentType,
					"id":    sa.id,
					"task":  task,
					"error": sa.errorMsg,
				})
			}
			responseCh <- fmt.Sprintf("\n❌ LLM 请求失败: %v", streamErr)
			return
		}

		if len(result.toolCalls) == 0 {
			sa.mu.Lock()
			sa.currentTool = ""
			sa.fullOutput = result.content
			sa.status = "completed"
			sa.duration = time.Since(sa.startTime)
			sa.mu.Unlock()
			sa.publishAgentStatus()
			if sa.bus != nil {
				sa.bus.PublishAsync(event.EventTaskCompleted, map[string]any{
					"agent":    sa.agentType,
					"id":       sa.id,
					"task":     task,
					"result":   result.content,
					"duration": sa.duration,
					"tools":    sa.toolCount,
				})
			}
			return
		}

		sa.executeToolCalls(ctx, result.toolCalls, responseCh)

		if sa.toolCallRounds > sa.maxToolCallRounds {
			return
		}

		req, err := sa.session.BuildRequest()
		if err != nil {
			responseCh <- fmt.Sprintf("\n❌ 构建后续请求失败: %v", err)
			sa.mu.Lock()
			sa.currentTool = ""
			sa.status = "error"
			sa.errorMsg = fmt.Sprintf("构建请求失败: %v", err)
			sa.duration = time.Since(sa.startTime)
			sa.mu.Unlock()
			sa.publishAgentStatus()
			if sa.bus != nil {
				sa.bus.PublishAsync(event.EventTaskFailed, map[string]any{
					"agent": sa.agentType,
					"id":    sa.id,
					"task":  task,
					"error": sa.errorMsg,
				})
			}
			return
		}

		streamCh, err = sa.provider.Chat(ctx, req)
		if err != nil {
			sa.logError("LLM", "error", err.Error())
			responseCh <- fmt.Sprintf("\n❌ LLM 请求失败: %v", err)
			sa.mu.Lock()
			sa.currentTool = ""
			sa.status = "error"
			sa.errorMsg = fmt.Sprintf("LLM 请求失败: %v", err)
			sa.duration = time.Since(sa.startTime)
			sa.mu.Unlock()
			sa.publishAgentStatus()
			if sa.bus != nil {
				sa.bus.PublishAsync(event.EventTaskFailed, map[string]any{
					"agent": sa.agentType,
					"id":    sa.id,
					"task":  task,
					"error": sa.errorMsg,
				})
			}
			return
		}
	}
}

// collectToolError 收集工具执行错误（延迟注入版本）
func (sa *BaseAgent) collectToolError(responseCh chan<- string, category string, err error, context string) string {
	errMsg, ok := sa.toolErrors.Add(category, err, context)
	if !ok {
		responseCh <- fmt.Sprintf("\n⚠️ 连续 %d 次错误，停止自动纠正。", sa.toolErrors.MaxErrors())
		return ""
	}
	responseCh <- fmt.Sprintf("\n🔄 自我纠正 #%d (%s)...", sa.toolErrors.Count(), category)
	sa.logError(category, "error", err.Error())
	return errMsg
}

// extractToolPath 从工具参数中提取文件路径或命令（供权限检查使用）
func extractToolPath(toolName string, args map[string]any) string {
	switch toolName {
	case "write", "edit", "read":
		if path, ok := args["filePath"].(string); ok {
			return path
		}
	case "bash":
		if cmd, ok := args["command"].(string); ok {
			parts := strings.Fields(cmd)
			if len(parts) > 0 {
				return parts[0]
			}
		}
	case "glob", "grep":
		if pattern, ok := args["pattern"].(string); ok {
			return pattern
		}
	}
	return ""
}

// truncateStr 截断字符串到指定长度
func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// truncateTask 截断任务描述到指定长度
func truncateTask(task string) string {
	if task == "" {
		return ""
	}
	runes := []rune(task)
	if len(runes) > 60 {
		return string(runes[:60]) + "..."
	}
	return task
}

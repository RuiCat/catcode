package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"catcode/core/event"
	"catcode/core/utils"
)

// dispatchSubAgent 分发子智能体任务（同步等待结果）
// toolCallID 是 LLM 原始返回的 tool_call.id，必须原样用于 AddToolResult，否则 API 会报 400 错误
func (a *Architect) dispatchSubAgent(ctx context.Context, toolCallID string, args map[string]any, responseCh chan<- string) {
	subagentType, _ := args["subagent_type"].(string)
	taskDesc, _ := args["description"].(string)
	startTime := time.Now()

	a.planStatus = fmt.Sprintf("等待子智能体: %s", subagentType)
	responseCh <- fmt.Sprintf("\n🤖 委派子智能体 %s: %s", subagentType, taskDesc)

	if a.agentPool == nil {
		responseCh <- "\n⚠️ 子智能体池未初始化"
		a.mainSession.AddToolResult(toolCallID, "task", "子智能体池未初始化，无法执行 task 调用")
		return
	}

	// 设置子智能体→主智能体的双向通信回调
	if inst, err := a.agentPool.GetOrCreate(subagentType); err == nil {
		inst.SetAskArchitectCallback(func(question string) string {
			return a.answerSubAgentQuestion(ctx, subagentType, question)
		})
		inst.SetWorkDir(a.workDir)
	}

	// 同步执行子智能体
	contextSummary := a.buildSubAgentContext(taskDesc, subagentType)
	ch, err := a.agentPool.Execute(ctx, subagentType, taskDesc, contextSummary)
	if err != nil {
		errMsg := fmt.Sprintf("子智能体 %s 启动失败: %v", subagentType, err)
		responseCh <- fmt.Sprintf("\n❌ %s", errMsg)
		a.mainSession.AddToolResult(toolCallID, "task", errMsg)
		return
	}

	// 收集结果 — 空闲超时：每次收到输出重置计时器
	var result strings.Builder
	const subAgentIdleTimeout = 10 * time.Minute // 10分钟无输出才超时
	idleTimer := time.NewTimer(subAgentIdleTimeout)
	defer idleTimer.Stop()

loop:
	for {
		select {
		case <-ctx.Done():
			a.mainSession.AddToolResult(toolCallID, "task",
				fmt.Sprintf("子智能体 %s 执行被取消", subagentType))
			return
		case <-idleTimer.C:
			responseCh <- fmt.Sprintf("\n⚠️ 子智能体 %s 执行超时 (%v 无输出)", subagentType, subAgentIdleTimeout)
			a.mainSession.AddToolResult(toolCallID, "task",
				fmt.Sprintf("子智能体 %s 执行超时 (%v 无输出)", subagentType, subAgentIdleTimeout))
			return
		case text, ok := <-ch:
			if !ok {
				break loop
			}
			// 收到输出，重置空闲计时器
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(subAgentIdleTimeout)
			result.WriteString(text)
			responseCh <- text
		}
	}
	finalResult := result.String()

	// 注入主会话（供 LLM 消化处理）— 使用 LLM 原始 tool_call_id 避免 API 400 错误
	duration := time.Since(startTime)
	a.mainSession.AddToolResult(toolCallID, "task",
		fmt.Sprintf("子智能体 %s 执行 [%s] 完成 (%.1fs):\n%s",
			subagentType, taskDesc, duration.Seconds(), finalResult))

	a.planStatus = ""
	responseCh <- fmt.Sprintf("\n✅ 子智能体 %s 完成 (%d 字符, %.1fs)", subagentType, len(finalResult), duration.Seconds())
}

// buildSubAgentContext 为子智能体构建完整的上下文摘要
func (a *Architect) buildSubAgentContext(taskDesc, subagentType string) string {
	var sb strings.Builder

	// 1. 主会话最近上下文
	mainCtx := a.extractMainContext(2000)
	if mainCtx != "" {
		sb.WriteString("[主会话上下文]\n")
		sb.WriteString(mainCtx)
		sb.WriteString("\n")
	}

	// 2. 记忆索引摘要
	if a.mainSession.MemoryIndex != "" {
		index := a.mainSession.MemoryIndex
		if len(index) > 1500 {
			index = index[:1500] + "\n...(截断)"
		}
		sb.WriteString("[记忆索引]\n")
		sb.WriteString(index)
		sb.WriteString("\n")
	}

	// 3. 指令文件
	if a.instructions != nil && !a.instructions.IsEmpty() {
		sb.WriteString(a.instructions.FormatContext(4000))
		sb.WriteString("\n")
	}

	// 4. 环境信息
	env := a.buildEnvironmentContext(subagentType)
	if env != "" {
		sb.WriteString("[环境信息]\n")
		sb.WriteString(env)
		sb.WriteString("\n")
	}

	return sb.String()
}

// BuildSubAgentContext 公开的子智能体上下文构建方法（供 main 中 @ 命令使用）
func (a *Architect) BuildSubAgentContext(taskDesc, subagentType string) string {
	return a.buildSubAgentContext(taskDesc, subagentType)
}

// extractMainContext 从主会话提取最近上下文
func (a *Architect) extractMainContext(maxChars int) string {
	if maxChars <= 0 {
		maxChars = 2000
	}
	msgs := a.mainSession.Messages
	if len(msgs) == 0 {
		return ""
	}

	var parts []string
	remaining := maxChars
	// 从后往前取最近的用户和助手消息
	for i := len(msgs) - 1; i >= 0 && remaining > 0; i-- {
		msg := msgs[i]
		if !msg.Enable {
			continue
		}
		if msg.Role != "user" && msg.Role != "assistant" {
			continue
		}
		text := msg.Content
		if len(text) > 500 {
			text = text[:500] + "..."
		}
		if len(text) > remaining {
			text = text[:remaining]
		}
		prefix := "用户: "
		if msg.Role == "assistant" {
			prefix = "助手: "
		}
		parts = append([]string{prefix + text}, parts...)
		remaining -= len(text)
	}

	return strings.Join(parts, "\n\n")
}

// buildEnvironmentContext 构建项目环境上下文
func (a *Architect) buildEnvironmentContext(subagentType string) string {
	var sb strings.Builder
	if a.workDir != "" {
		sb.WriteString("工作目录: " + a.workDir + "\n")
	}
	sb.WriteString("子智能体类型: " + subagentType + "\n")
	sb.WriteString("主会话模型: " + a.config.Model + "\n")
	return sb.String()
}

// answerSubAgentQuestion 处理子智能体提问并返回答案
// 通过 Session.BuildCleanRequest 构建独立请求，不依赖主会话状态
func (a *Architect) answerSubAgentQuestion(ctx context.Context, subagentType, question string) string {
	req := a.mainSession.BuildCleanRequest(
		fmt.Sprintf("[子智能体 %s 提问]\n%s\n\n请简要、直接地回答。", subagentType, question),
		2048,
	)

	resp, err := a.provider.ChatSync(ctx, req)
	if err != nil {
		return fmt.Sprintf("LLM 请求失败: %v", err)
	}

	if resp != nil && len(resp.Choices) > 0 {
		return resp.Choices[0].Message.Content
	}
	return "无法获取回答"
}

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// 事件处理
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func (a *Architect) onRoleResult(evt event.Event) {
	roleName, _ := evt.Data["role"].(string)
	result, _ := evt.Data["result"].(string)
	fmt.Printf("[Architect] 角色 %s 返回结果: %s\n", roleName, utils.TruncateStr(result, 100))
}

func (a *Architect) onSubAgentResult(evt event.Event) {
	agentType, _ := evt.Data["type"].(string)
	result, _ := evt.Data["result"].(string)
	task, _ := evt.Data["task"].(string)

	// 使用 system 消息注入结果，避免伪造 tool_call_id 导致 API 400 错误
	a.mainSession.AddMessage("system",
		fmt.Sprintf("[子智能体 %s 异步完成] 任务: %s\n结果:\n%s", agentType, task, result))

	fmt.Printf("[Architect] 子智能体 %s 完成: %s\n", agentType, utils.TruncateStr(result, 100))
}

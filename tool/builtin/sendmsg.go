package builtin

import (
	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/tool"
	"fmt"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// SendMessage — 向对话框直接发送消息
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// SendMessageTool 创建对话框消息工具
// 猫猫或子智能体可用此工具直接在对话框中显示回复
func SendMessageTool(bus event.EventBus) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "send_message",
			Description: "向用户对话框发送一条消息。猫猫回复用户、子智能体报告进度或任何需要直接与用户沟通时使用。sender 参数用于标识消息来源（如 猫猫/explore/general 等）。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"message": {Type: "string", Description: "要发送的消息内容"},
					"sender":  {Type: "string", Description: "发送者名称（如 猫猫、explore、general）"},
				},
				Required: []string{"message"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			msg, _ := args["message"].(string)
			sender, _ := args["sender"].(string)
			if sender == "" {
				sender = "系统"
			}
			if msg == "" {
				return "", cerr.Newf("send_message: message 不能为空")
			}
			if bus != nil {
				bus.PublishAsync(event.EventDialogSend, map[string]any{
					"message": msg,
					"sender":  sender,
				})
			}
			return fmt.Sprintf("✓ 已发送"), nil
		},
	}
}

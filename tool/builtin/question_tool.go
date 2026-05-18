package builtin

import (
	"encoding/json"
	"time"

	cerr "catcode/core/errors"
	"catcode/core/event"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Question — 选项框提问工具
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// QuestionTool 创建选项框提问工具
func QuestionTool(bus event.EventBus) *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "question",
			Description: "向用户展示选项框让其选择。适用场景：需要用户在多个方案中做决策、确认操作、选择文件等。参数 questions 是一个数组，每项包含: question(完整问题)/header(短标签)/options(选项label+description数组)/multiple(是否多选)。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"questions": {Type: "array", Description: "问题列表"},
				},
				Required: []string{"questions"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			// 处理 questions 参数：可能是 JSON 字符串，也可能是原生数组
			var questionsJSON string
			switch v := args["questions"].(type) {
			case string:
				questionsJSON = v
			default:
				// 原生类型（array/object），marshal 为 JSON 字符串
				data, err := json.Marshal(v)
				if err != nil {
					return "", cerr.Wrap(err, "question: 无法序列化 questions 参数")
				}
				questionsJSON = string(data)
			}
			replyCh := make(chan tool.QuestionAnswer, 1)
			if bus != nil {
				bus.Publish(event.EventQuestionAsked, map[string]any{
					"questions": questionsJSON,
					"reply":     replyCh,
				})
			}
			// 带超时的阻塞等待
			select {
			case answer := <-replyCh:
				data, _ := json.Marshal(answer)
				return string(data), nil
			case <-time.After(120 * time.Second):
				return "", cerr.Newf("question: 等待用户回答超时")
			}
		},
	}
}

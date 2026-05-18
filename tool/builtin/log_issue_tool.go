package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	cerr "catcode/core/errors"
	"catcode/tool"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// LogIssue — 问题日志记录工具
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

// LogIssueTool 创建问题日志记录工具
// 智能体遇到无法解决的问题时，将问题写入工作目录下的 catcode_issues.log
func LogIssueTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "log_issue",
			Description: "将当前无法解决的问题记录到日志文件中。适用场景：遇到技术限制、缺少必要依赖、需要用户手动介入、不确定如何继续等。日志文件位于工作目录下的 catcode_issues.log，采用追加模式写入，不会覆盖已有内容。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"title":       {Type: "string", Description: "问题的简短标题"},
					"description": {Type: "string", Description: "问题的详细描述，包括：遇到什么情况、尝试过什么方法、为什么无法解决、建议用户如何介入"},
					"category":    {Type: "string", Description: "问题分类: bug(代码缺陷), limitation(工具/环境限制), unknown(不确定/需用户决策)", Enum: []string{"bug", "limitation", "unknown"}},
				},
				Required: []string{"title", "description"},
			}),
		},
		Call: func(ctx *tool.Context, args map[string]any) (string, error) {
			title, _ := args["title"].(string)
			description, _ := args["description"].(string)
			category, _ := args["category"].(string)
			if category == "" {
				category = "unknown"
			}

			if title == "" {
				return "", cerr.Newf("log_issue: title 参数不能为空")
			}

			// 确定日志文件路径：工作目录下的 catcode_issues.log
			logPath := filepath.Join(ctx.WorkDir, "catcode_issues.log")

			// 构建日志条目
			timestamp := time.Now().Format("2006-01-02 15:04:05")
			entry := fmt.Sprintf("[%s] [%s] %s\n%s\n---\n", timestamp, category, title, description)

			// 追加写入
			f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
			if err != nil {
				return "", cerr.Wrapf(err, "log_issue: 无法打开日志文件 %s", logPath)
			}
			defer f.Close()

			if _, err := f.WriteString(entry); err != nil {
				return "", cerr.Wrap(err, "log_issue: 写入日志失败")
			}

			return fmt.Sprintf("问题已记录到 %s（分类: %s）", logPath, category), nil
		},
	}
}

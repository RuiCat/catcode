package builtin

import (
	cerr "catcode/core/errors"
	"catcode/tool"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Diff — 文件差异比较
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func DiffTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "diff",
			Description: "比较两个文件或生成文件变更差异。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"path":        {Type: "string", Description: "文件路径"},
					"old_content": {Type: "string", Description: "旧内容 (可选，不提供则与磁盘文件比较)"},
					"new_content": {Type: "string", Description: "新内容 (可选，不提供则与磁盘文件比较)"},
				},
				Required: []string{"path"},
			}),
		},
		Call: diffCall,
	}
}

func diffCall(ctx *tool.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	oldContent, hasOld := args["old_content"].(string)
	newContent, hasNew := args["new_content"].(string)

	if hasOld && hasNew {
		return generateDiff(path, oldContent, newContent), nil
	}

	// 与磁盘文件比较
	resolvedPath, err := ResolveAndCheckPath(path, ctx.WorkDir)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", cerr.Wrap(err, "diff: 读取文件失败")
	}

	if hasOld {
		return generateDiff(path, oldContent, string(data)), nil
	}
	if hasNew {
		return generateDiff(path, string(data), newContent), nil
	}

	// 检查 git diff
	cmd := exec.Command("git", "diff", "--", path)
	cmd.Dir = ctx.WorkDir
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return fmt.Sprintf("[diff] %s: 无变更", path), nil
	}
	return string(output), nil
}

func generateDiff(path, oldContent, newContent string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var result strings.Builder
	result.WriteString(fmt.Sprintf("--- a/%s\n+++ b/%s\n", path, path))

	// 简单的行级 diff (LCS-based 过于复杂，使用逐行比较)
	maxLen := len(oldLines)
	if len(newLines) > maxLen {
		maxLen = len(newLines)
	}

	added, removed := 0, 0
	for i := 0; i < maxLen; i++ {
		if i < len(oldLines) && i < len(newLines) {
			if oldLines[i] != newLines[i] {
				if i < len(oldLines) {
					result.WriteString(fmt.Sprintf("-%s\n", oldLines[i]))
					removed++
				}
				if i < len(newLines) {
					result.WriteString(fmt.Sprintf("+%s\n", newLines[i]))
					added++
				}
			}
		} else if i < len(oldLines) {
			result.WriteString(fmt.Sprintf("-%s\n", oldLines[i]))
			removed++
		} else {
			result.WriteString(fmt.Sprintf("+%s\n", newLines[i]))
			added++
		}
	}
	result.WriteString(fmt.Sprintf("\n[%d 行删除, %d 行新增]", removed, added))
	return result.String()
}

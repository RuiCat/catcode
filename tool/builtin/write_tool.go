package builtin

import (
	cerr "catcode/core/errors"
	"catcode/tool"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Write — 文件写入
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func WriteTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "write",
			Description: "写入文件内容。创建新文件或覆盖已有文件。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"path":    {Type: "string", Description: "文件路径"},
					"content": {Type: "string", Description: "要写入的文件内容"},
				},
				Required: []string{"path", "content"},
			}),
		},
		Call: writeCall,
	}
}

func writeCall(ctx *tool.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)

	// 相对路径基于工作目录解析
	if !filepath.IsAbs(path) && ctx.WorkDir != "" {
		path = filepath.Join(ctx.WorkDir, path)
	}

	// 安全检查：解析父目录符号链接并验证路径在工作区内
	cleanPath := filepath.Clean(path)
	parentDir := filepath.Dir(cleanPath)
	resolvedParent, err := filepath.EvalSymlinks(parentDir)
	if err != nil {
		return "", cerr.Wrap(err, "write: 无法解析父目录路径")
	}
	resolvedPath := filepath.Join(resolvedParent, filepath.Base(cleanPath))
	if !strings.HasPrefix(resolvedPath, ctx.WorkDir) {
		return "", cerr.Newf("write: 路径超出工作区范围")
	}

	// 确保目录存在
	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", cerr.Wrap(err, "write: 创建目录失败")
	}

	// 检查是否为新文件
	_, err = os.Stat(resolvedPath)
	isNew := os.IsNotExist(err)

	if err := os.WriteFile(resolvedPath, []byte(content), 0644); err != nil {
		return "", cerr.Wrap(err, "write: 写入失败")
	}

	lines := strings.Count(content, "\n") + 1
	if isNew {
		return fmt.Sprintf("✓ 已创建文件 %s (%d 行)", resolvedPath, lines), nil
	}
	return fmt.Sprintf("✓ 已覆盖文件 %s (%d 行)", resolvedPath, lines), nil
}

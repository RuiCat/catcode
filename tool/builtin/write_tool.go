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
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", cerr.Newf("write: path 参数必填")
	}
	content, _ := args["content"].(string)

	// 安全检查：使用 ResolveAndCheckPath 统一验证路径
	resolvedPath, err := ResolveAndCheckPath(path, ctx.WorkDir)
	if err != nil {
		return "", err
	}

	// 确保目录存在
	dir := filepath.Dir(resolvedPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", cerr.Wrap(err, "write: 创建目录失败")
	}

	// 检查是否为新文件
	_, err = os.Stat(resolvedPath)
	isNew := os.IsNotExist(err)

	// TOCTOU 防护：写入前对已有文件重新验证路径
	if info, err := os.Stat(resolvedPath); err == nil && !info.IsDir() {
		rechecked, err := filepath.EvalSymlinks(resolvedPath)
		if err == nil && !strings.HasPrefix(rechecked, ctx.WorkDir) {
			return "", cerr.Newf("write: 路径在安全检查后发生变化，拒绝写入")
		}
	}

	if err := os.WriteFile(resolvedPath, []byte(content), 0644); err != nil {
		return "", cerr.Wrap(err, "write: 写入失败")
	}

	lines := strings.Count(content, "\n") + 1
	if isNew {
		return fmt.Sprintf("✓ 已创建文件 %s (%d 行)", resolvedPath, lines), nil
	}
	return fmt.Sprintf("✓ 已覆盖文件 %s (%d 行)", resolvedPath, lines), nil
}

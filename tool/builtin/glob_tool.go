package builtin

import (
	cerr "catcode/core/errors"
	"catcode/tool"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Glob — 文件模式搜索
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func GlobTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "glob",
			Description: "按 glob 模式查找文件。返回匹配文件路径列表，按修改时间排序。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"pattern": {Type: "string", Description: "glob 模式，如 **/*.go, src/**/*.ts"},
					"path":    {Type: "string", Description: "搜索起始目录 (默认当前目录)"},
				},
				Required: []string{"pattern"},
			}),
		},
		Call: globCall,
	}
}

func globCall(ctx *tool.Context, args map[string]any) (string, error) {
	pattern, _ := args["pattern"].(string)
	root, _ := args["path"].(string)
	if root == "" {
		root = ctx.WorkDir
	}
	if root == "" {
		root = "."
	}

	// 安全检查：验证搜索路径在工作区范围内
	resolvedPath, err := ResolveAndCheckPath(root, ctx.WorkDir)
	if err != nil {
		return "", err
	}
	root = resolvedPath

	var matches []string
	baseDir := filepath.Dir(pattern)
	if baseDir == "." {
		baseDir = ""
	}

	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 跳过无法访问的目录
		}
		// 跳过隐藏目录
		if d.IsDir() && strings.HasPrefix(d.Name(), ".") && d.Name() != "." {
			return filepath.SkipDir
		}
		// 跳过常见忽略目录
		if d.IsDir() {
			skipDirs := map[string]bool{
				"node_modules": true, ".git": true, "__pycache__": true,
				"vendor": true, "dist": true, "build": true,
				".next": true, ".turbo": true, "target": true,
			}
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
		}
		if !d.IsDir() {
			relPath, _ := filepath.Rel(root, path)
			matched, _ := filepath.Match(filepath.Base(pattern), d.Name())
			if matched || matchGlobPattern(pattern, relPath) {
				matches = append(matches, relPath)
			}
		}
		return nil
	})
	if err != nil {
		return "", cerr.Wrap(err, "glob: 遍历失败")
	}

	if len(matches) == 0 {
		return fmt.Sprintf("[glob] 模式 %s: 无匹配文件", pattern), nil
	}

	// 限制返回数量
	maxResults := 100
	if len(matches) > maxResults {
		result := strings.Join(matches[:maxResults], "\n")
		result += fmt.Sprintf("\n\n... 还有 %d 个文件未显示", len(matches)-maxResults)
		return result, nil
	}

	return strings.Join(matches, "\n"), nil
}

// matchGlobPattern 简单 glob 匹配（** 递归匹配，* 单层匹配）
func matchGlobPattern(pattern, name string) bool {
	// ** 递归匹配
	if strings.Contains(pattern, "**") {
		parts := strings.Split(pattern, "**")
		// 前缀匹配
		if len(parts) > 0 && parts[0] != "" {
			if !strings.HasPrefix(name, strings.TrimSuffix(parts[0], "/")) {
				return false
			}
		}
		// 后缀匹配
		if len(parts) > 1 && parts[1] != "" {
			suffix := strings.TrimPrefix(parts[1], "/")
			if suffix != "" && !strings.HasSuffix(name, suffix) {
				return false
			}
		}
		return true
	}
	// 简单匹配
	matched, _ := filepath.Match(pattern, name)
	return matched
}

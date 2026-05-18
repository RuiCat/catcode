package builtin

import (
	cerr "catcode/core/errors"
	"catcode/tool"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
// Grep — 内容正则搜索
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func GrepTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "grep",
			Description: "在文件内容中搜索正则表达式。返回匹配行及其上下文。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"pattern":       {Type: "string", Description: "正则表达式模式"},
					"path":          {Type: "string", Description: "文件或目录路径"},
					"include":       {Type: "string", Description: "文件过滤 glob (如 *.go)"},
					"context_lines": {Type: "integer", Description: "匹配行上下文行数 (默认2)"},
				},
				Required: []string{"pattern"},
			}),
		},
		Call: grepCall,
	}
}

func grepCall(ctx *tool.Context, args map[string]any) (string, error) {
	pattern, _ := args["pattern"].(string)
	searchPath, _ := args["path"].(string)
	include, _ := args["include"].(string)
	contextLines := 2
	if v, ok := args["context_lines"].(float64); ok {
		contextLines = int(v)
	}

	if searchPath == "" {
		searchPath = ctx.WorkDir
	}
	if searchPath == "" {
		searchPath = "."
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", cerr.Wrap(err, "grep: 正则表达式无效")
	}

	var result strings.Builder
	matchCount := 0
	maxMatches := 50

	// 确定是搜索单个文件还是目录
	info, err := os.Stat(searchPath)
	if err != nil {
		return "", cerr.Wrap(err, "grep: 路径访问失败")
	}

	if info.IsDir() {
		// 目录搜索
		filepath.WalkDir(searchPath, func(path string, d fs.DirEntry, err error) error {
			if matchCount >= maxMatches {
				return filepath.SkipAll
			}
			if err != nil || d.IsDir() {
				return nil
			}
			if include != "" {
				matched, _ := filepath.Match(include, d.Name())
				if !matched {
					return nil
				}
			}
			// 跳过二进制文件
			if isBinaryFile(path) {
				return nil
			}
			grepFile(re, path, &result, &matchCount, maxMatches, contextLines)
			return nil
		})
	} else {
		grepFile(re, searchPath, &result, &matchCount, maxMatches, contextLines)
	}

	if matchCount == 0 {
		return fmt.Sprintf("[grep] 模式 %q: 无匹配", pattern), nil
	}

	result.WriteString(fmt.Sprintf("\n[共 %d 处匹配]", matchCount))
	return result.String(), nil
}

func grepFile(re *regexp.Regexp, path string, result *strings.Builder, matchCount *int, maxMatches, contextLines int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	fileHeaderWritten := false
	for i, line := range lines {
		if *matchCount >= maxMatches {
			return
		}
		if re.MatchString(line) {
			if !fileHeaderWritten {
				result.WriteString(fmt.Sprintf("\n%s:\n", path))
				fileHeaderWritten = true
			}
			// 上文
			start := i - contextLines
			if start < 0 {
				start = 0
			}
			for j := start; j < i; j++ {
				result.WriteString(fmt.Sprintf("  %d: %s\n", j+1, lines[j]))
			}
			// 匹配行
			result.WriteString(fmt.Sprintf("→ %d: %s\n", i+1, line))
			// 下文
			end := i + contextLines + 1
			if end > len(lines) {
				end = len(lines)
			}
			for j := i + 1; j < end; j++ {
				result.WriteString(fmt.Sprintf("  %d: %s\n", j+1, lines[j]))
			}
			*matchCount++
		}
	}
}

// isBinaryFile 检查是否为二进制文件
func isBinaryFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return true
	}
	defer f.Close()
	buf := make([]byte, 8000)
	n, _ := f.Read(buf)
	for _, b := range buf[:n] {
		if b == 0 {
			return true
		}
	}
	return false
}

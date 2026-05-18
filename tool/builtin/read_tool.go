package builtin

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"catcode/ai/session"
	cerr "catcode/core/errors"
	"catcode/tool"
)

func ReadTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "read",
			Description: "读取文件内容。支持分页、范围读取、info模式和search模式。大文件自动要求范围读取。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"path":    {Type: "string", Description: "文件路径"},
					"mode":    {Type: "string", Description: "模式: read(默认)/info(元数据)/search(搜索)", Enum: []string{"read", "info", "search"}},
					"offset":  {Type: "integer", Description: "起始行号 (1-indexed, 默认1)"},
					"limit":   {Type: "integer", Description: "读取行数 (默认500, info模式忽略)"},
					"pattern": {Type: "string", Description: "search模式的关键词/正则"},
				},
				Required: []string{"path"},
			}),
		},
		Call: readCall,
	}
}

func readCall(ctx *tool.Context, args map[string]any) (string, error) {
	path, ok := args["path"].(string)
	if !ok || path == "" {
		return "", cerr.Newf("read: path 参数必填")
	}
	if !filepath.IsAbs(path) && ctx.WorkDir != "" {
		path = filepath.Join(ctx.WorkDir, path)
	}
	cleanPath := filepath.Clean(path)
	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		return "", cerr.Wrap(err, "read: 无法解析路径")
	}
	if !strings.HasPrefix(resolvedPath, ctx.WorkDir) {
		return "", cerr.Newf("read: 路径超出工作区范围")
	}
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", cerr.Wrap(err, "read: 文件不存在或无权限")
	}
	if info.IsDir() {
		return "", cerr.Newf("read: 路径是目录而非文件: %s", path)
	}
	fileSize := int(info.Size())
	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", cerr.Wrap(err, "read: 读取文件失败")
	}
	lines := strings.Split(string(data), "\n")
	totalLines := len(lines)
	mode := "read"
	if m, ok := args["mode"].(string); ok && m != "" {
		mode = m
	}
	if mode == "info" {
		modTime := info.ModTime().Format("2006-01-02 15:04:05")
		return fmt.Sprintf("[文件: %s, 大小: %d字节, 行数: %d, 修改时间: %s]", path, fileSize, totalLines, modTime), nil
	}
	if mode == "search" {
		pattern, _ := args["pattern"].(string)
		if pattern == "" {
			return "", cerr.Newf("read: search模式需要pattern参数")
		}
		return searchLines(lines, pattern, path, totalLines), nil
	}
	offset := 1
	limit := 500
	if v, ok := args["offset"].(float64); ok && v > 0 {
		offset = int(v)
	}
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	if offset > totalLines {
		offset = totalLines
	}
	if (totalLines > 5000 || fileSize > 500000) && offset == 1 && limit >= 500 {
		return fmt.Sprintf("文件过大 (%d行, %d字节)。请指定 offset/limit 进行范围读取。使用 mode=info 查看文件元数据。", totalLines, fileSize), nil
	}
	end := offset + limit
	if end > totalLines {
		end = totalLines
	}
	var sb strings.Builder
	for i := offset - 1; i < end; i++ {
		sb.WriteString(fmt.Sprintf("%6d│%s\n", i+1, lines[i]))
	}
	content := sb.String()
	sess, _ := ctx.Extra["session"].(*session.Session)
	replaced := false
	if sess != nil {
		replaced = sess.UpsertFileBlock(ctx.ToolCallID, path, content, offset, end, totalLines, fileSize)
	}
	var result strings.Builder
	if replaced {
		result.WriteString(fmt.Sprintf("[文件: %s, 行%d-%d/%d, %d字节] (已替换旧块)\n", path, offset, end, totalLines, fileSize))
	} else {
		result.WriteString(fmt.Sprintf("[文件: %s, 行%d-%d/%d, %d字节]\n", path, offset, end, totalLines, fileSize))
	}
	result.WriteString(content)
	if end < totalLines {
		result.WriteString(fmt.Sprintf("... (剩余 %d 行, 使用 offset=%d 续读)", totalLines-end, end+1))
	}
	return result.String(), nil
}

func searchLines(lines []string, pattern, path string, totalLines int) string {
	var matches []string
	patternLower := strings.ToLower(pattern)
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), patternLower) {
			ctxStart := i - 2
			if ctxStart < 0 {
				ctxStart = 0
			}
			ctxEnd := i + 3
			if ctxEnd > len(lines) {
				ctxEnd = len(lines)
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("--- 匹配 行%d ---\n", i+1))
			for j := ctxStart; j < ctxEnd; j++ {
				marker := "  "
				if j == i {
					marker = ">>"
				}
				sb.WriteString(fmt.Sprintf("%s%6d│%s\n", marker, j+1, lines[j]))
			}
			matches = append(matches, sb.String())
		}
	}
	if len(matches) == 0 {
		return fmt.Sprintf("[文件: %s, %d行] 未找到匹配 \"%s\" 的行。", path, totalLines, pattern)
	}
	if len(matches) > 20 {
		matches = matches[:20]
		return fmt.Sprintf("[文件: %s, %d行] 搜索 \"%s\": 找到 %d+ 处匹配（仅显示前20处）\n\n%s", path, totalLines, pattern, len(matches), strings.Join(matches, ""))
	}
	return fmt.Sprintf("[文件: %s, %d行] 搜索 \"%s\": 找到 %d 处匹配\n\n%s", path, totalLines, pattern, len(matches), strings.Join(matches, ""))
}

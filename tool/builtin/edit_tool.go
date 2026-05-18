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
// Edit — 精确文本编辑（搜索替换）
// ━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

func EditTool() *tool.Tool {
	return &tool.Tool{
		Function: tool.FuncDef{
			Name:        "edit",
			Description: "在文件中执行精确的文本替换。old_string 必须精确匹配（含缩进），替换为 new_string。仅替换首次出现。",
			Parameters: tool.MustMarshalSchema(tool.Schema{
				Type: "object",
				Properties: map[string]tool.Property{
					"path":       {Type: "string", Description: "要编辑的文件路径"},
					"old_string": {Type: "string", Description: "要被替换的文本（必须精确匹配）"},
					"new_string": {Type: "string", Description: "替换后的文本"},
				},
				Required: []string{"path", "old_string", "new_string"},
			}),
		},
		Call: editCall,
	}
}

func editCall(ctx *tool.Context, args map[string]any) (string, error) {
	path, _ := args["path"].(string)
	oldStr, _ := args["old_string"].(string)
	newStr, _ := args["new_string"].(string)

	// 相对路径基于工作目录解析
	if !filepath.IsAbs(path) && ctx.WorkDir != "" {
		path = filepath.Join(ctx.WorkDir, path)
	}

	// 安全检查：解析符号链接并验证路径在工作区内
	cleanPath := filepath.Clean(path)
	resolvedPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		return "", cerr.Wrap(err, "edit: 无法解析路径")
	}
	if !strings.HasPrefix(resolvedPath, ctx.WorkDir) {
		return "", cerr.Newf("edit: 路径超出工作区范围")
	}

	data, err := os.ReadFile(resolvedPath)
	if err != nil {
		return "", cerr.Wrap(err, "edit: 读取文件失败")
	}

	content := string(data)

	// 尝试精确匹配
	idx := strings.Index(content, oldStr)
	if idx == -1 {
		// 精确匹配失败，尝试容错匹配
		matchedIdx, matchedStr := fuzzyMatchEdit(content, oldStr)
		if matchedIdx == -1 {
			// 给出更友好的错误提示
			return "", cerr.Newf("edit: old_string 在文件中未找到。\n"+
				"期望匹配的文本:\n%s\n\n"+
				"提示: 请确保 old_string 的缩进、空格和换行与文件中完全一致。\n"+
				"建议先用 read 工具查看文件实际内容，再复制需要替换的文本。", oldStr)
		}
		// 使用容错匹配到的字符串
		oldStr = matchedStr
		idx = matchedIdx
	}

	// 检查是否有多重匹配（仅在精确匹配时检查）
	if oldStr == args["old_string"].(string) {
		count := strings.Count(content, oldStr)
		if count > 1 {
			// 尝试用行号定位：检查 oldStr 是否跨行，如果是则用行号辅助
			oldLines := strings.Split(oldStr, "\n")
			if len(oldLines) > 1 {
				// 多行匹配：找第一个唯一匹配
				firstLine := oldLines[0]
				var bestIdx, bestScore int
				searchFrom := 0
				for {
					pos := strings.Index(content[searchFrom:], firstLine)
					if pos == -1 {
						break
					}
					pos += searchFrom
					// 检查是否完整匹配多行
					endPos := pos + len(oldStr)
					if endPos <= len(content) && content[pos:endPos] == oldStr {
						// 计算匹配得分：优先选择行号更靠后的（最近修改的位置）
						lineNum := strings.Count(content[:pos], "\n") + 1
						score := lineNum
						if score > bestScore {
							bestScore = score
							bestIdx = pos
						}
					}
					searchFrom = pos + 1
				}
				if bestIdx > 0 {
					idx = bestIdx
				}
				// 如果仍有歧义，使用最后一次出现（通常是最近需要修改的位置）
			}
		}
	}

	newContent := content[:idx] + newStr + content[idx+len(oldStr):]
	if err := os.WriteFile(resolvedPath, []byte(newContent), 0644); err != nil {
		return "", cerr.Wrap(err, "edit: 写入失败")
	}

	// 生成简短 diff
	oldLines := strings.Split(oldStr, "\n")
	newLines := strings.Split(newStr, "\n")
	added := len(newLines) - len(oldLines)

	return fmt.Sprintf("✓ 已编辑 %s: 替换了 %d 行 (%+d 行变化)", resolvedPath, len(oldLines), added), nil
}

// fuzzyMatchEdit 容错匹配：尝试去除首尾空白、统一缩进后匹配
func fuzzyMatchEdit(content, oldStr string) (int, string) {
	// 策略1: 去除 oldStr 首尾空白后匹配
	trimmed := strings.TrimSpace(oldStr)
	if trimmed != oldStr {
		if idx := strings.Index(content, trimmed); idx != -1 {
			return idx, trimmed
		}
	}

	// 策略2: 统一换行符（\r\n → \n）
	normalized := strings.ReplaceAll(oldStr, "\r\n", "\n")
	if normalized != oldStr {
		if idx := strings.Index(content, normalized); idx != -1 {
			return idx, normalized
		}
	}

	// 策略3: 逐行匹配（忽略每行首尾空白差异）
	oldLines := strings.Split(oldStr, "\n")
	contentLines := strings.Split(content, "\n")

	if len(oldLines) <= len(contentLines) && len(oldLines) > 0 {
		// 尝试找到匹配的行序列
		for start := 0; start <= len(contentLines)-len(oldLines); start++ {
			match := true
			for i, oldLine := range oldLines {
				if strings.TrimSpace(oldLine) != strings.TrimSpace(contentLines[start+i]) {
					match = false
					break
				}
			}
			if match {
				// 用文件中实际的行内容构建匹配字符串
				matchedLines := contentLines[start : start+len(oldLines)]
				matchedStr := strings.Join(matchedLines, "\n")
				// 计算在 content 中的位置
				prefix := strings.Join(contentLines[:start], "\n")
				if start > 0 {
					prefix += "\n"
				}
				idx := len(prefix)
				return idx, matchedStr
			}
		}
	}

	return -1, ""
}
